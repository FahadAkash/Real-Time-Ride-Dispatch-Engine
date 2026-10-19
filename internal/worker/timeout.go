package worker

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/ride-dispatch/engine/internal/domain"
	"github.com/ride-dispatch/engine/internal/platform/metrics"
)

type Matcher interface {
	FindAndReserveBestDriver(ctx context.Context, region string, pickup domain.Location, rideID string, excludedDrivers []string, maxCandidates int, lockTTL time.Duration) (*domain.DriverCandidate, error)
}

type Locker interface {
	ReleaseDriverLock(ctx context.Context, driverID, rideID string) (bool, error)
}

type RideRepo interface {
	GetActiveRidesForTimeout(ctx context.Context, olderThan time.Time, limit int) ([]*domain.Ride, error)
	UpdateRideForReassign(ctx context.Context, rideID string, expectedVersion int64, excludedDriver string, event *domain.EventEnvelope) error
	UpdateRideStatus(ctx context.Context, rideID string, expectedStatus domain.RideStatus, expectedVersion int64, newStatus domain.RideStatus, actor, reason string, at time.Time, event *domain.EventEnvelope) error
}

type Notifier interface {
	NotifyDriverOffer(ctx context.Context, driverID string, ride *domain.Ride, timeout time.Duration) error
}

type TimeoutWorkerConfig struct {
	PollInterval    time.Duration
	OfferTimeout    time.Duration
	LockTTL         time.Duration
	MaxReassigns    int
	CandidatesCount int
	BatchSize       int
}

type TimeoutWorker struct {
	cfg      TimeoutWorkerConfig
	matcher  Matcher
	locker   Locker
	rideRepo RideRepo
	notifier Notifier
	logger   *slog.Logger
}

func NewTimeoutWorker(cfg TimeoutWorkerConfig, matcher Matcher, locker Locker, rideRepo RideRepo, notifier Notifier, logger *slog.Logger) *TimeoutWorker {
	if cfg.PollInterval == 0 {
		cfg.PollInterval = 1 * time.Second
	}
	if cfg.OfferTimeout == 0 {
		cfg.OfferTimeout = 15 * time.Second
	}
	if cfg.LockTTL == 0 {
		cfg.LockTTL = 15 * time.Second
	}
	if cfg.MaxReassigns == 0 {
		cfg.MaxReassigns = 3
	}
	if cfg.CandidatesCount == 0 {
		cfg.CandidatesCount = 10
	}
	if cfg.BatchSize == 0 {
		cfg.BatchSize = 100
	}
	if logger == nil {
		logger = slog.Default()
	}

	return &TimeoutWorker{
		cfg:      cfg,
		matcher:  matcher,
		locker:   locker,
		rideRepo: rideRepo,
		notifier: notifier,
		logger:   logger,
	}
}

// Start runs the periodic timeout processing loop until ctx is cancelled.
func (w *TimeoutWorker) Start(ctx context.Context) {
	ticker := time.NewTicker(w.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.ProcessExpiredOffers(ctx)
		}
	}
}

// ProcessExpiredOffers scans for unanswered offers, releases their locks, and triggers re-matching.
func (w *TimeoutWorker) ProcessExpiredOffers(ctx context.Context) {
	cutoff := time.Now().UTC().Add(-w.cfg.OfferTimeout)
	expiredRides, err := w.rideRepo.GetActiveRidesForTimeout(ctx, cutoff, w.cfg.BatchSize)
	if err != nil {
		w.logger.Error("Failed to query expired rides for timeout", slog.String("error", err.Error()))
		return
	}

	for _, ride := range expiredRides {
		w.handleSingleExpiredRide(ctx, ride)
	}
}

func (w *TimeoutWorker) handleSingleExpiredRide(ctx context.Context, ride *domain.Ride) {
	now := time.Now().UTC()
	timedOutDriver := ride.DriverID

	// 1. Safe Compare-and-Delete: Release previous driver's lock
	if timedOutDriver != "" {
		released, err := w.locker.ReleaseDriverLock(ctx, timedOutDriver, ride.RideID)
		if err != nil {
			w.logger.Warn("Failed to release expired driver lock", slog.String("driverId", timedOutDriver), slog.String("rideId", ride.RideID), slog.String("error", err.Error()))
		} else if !released {
			w.logger.Debug("Driver lock was already released or owned by another ride", slog.String("driverId", timedOutDriver), slog.String("rideId", ride.RideID))
		}
	}

	// 2. Check if max reassign attempts exceeded
	if ride.ReassignCount >= w.cfg.MaxReassigns {
		event, _ := domain.NewEventEnvelope(uuid.New().String(), domain.EventRideRejected, ride.RideID, now, map[string]string{
			"rideId": ride.RideID,
			"reason": "max reassignment attempts exceeded",
		})
		_ = w.rideRepo.UpdateRideStatus(ctx, ride.RideID, domain.StatusPendingAccept, ride.Version, domain.StatusNoDriverFound, "timeout-worker", "max reassignment attempts exceeded", now, event)
		metrics.DispatchNoDriverTotal.Inc()
		return
	}

	// 3. Mark ride as REASSIGNING and exclude previous driver
	rejectEvent, _ := domain.NewEventEnvelope(uuid.New().String(), domain.EventRideRejected, ride.RideID, now, map[string]string{
		"rideId":   ride.RideID,
		"driverId": timedOutDriver,
		"reason":   "offer timed out",
	})
	if err := w.rideRepo.UpdateRideForReassign(ctx, ride.RideID, ride.Version, timedOutDriver, rejectEvent); err != nil {
		if errors.Is(err, domain.ErrOptimisticLockConflict) {
			// Ride state changed concurrently (e.g. driver accepted just before timeout)
			return
		}
		w.logger.Error("Failed to update ride for reassign", slog.String("rideId", ride.RideID), slog.String("error", err.Error()))
		return
	}

	metrics.DispatchReassignmentsTotal.Inc()

	// 4. Re-match excluding all previously attempted drivers
	excluded := append(ride.ExcludedDrivers, timedOutDriver)
	candidate, err := w.matcher.FindAndReserveBestDriver(ctx, "downtown", ride.Pickup, ride.RideID, excluded, w.cfg.CandidatesCount, w.cfg.LockTTL)
	if err != nil {
		if errors.Is(err, domain.ErrNoDriverFound) {
			noDriverEvent, _ := domain.NewEventEnvelope(uuid.New().String(), domain.EventRideRejected, ride.RideID, now, map[string]string{
				"rideId": ride.RideID,
				"reason": "no candidates available for reassign",
			})
			_ = w.rideRepo.UpdateRideStatus(ctx, ride.RideID, domain.StatusReassigning, ride.Version+1, domain.StatusNoDriverFound, "timeout-worker", "no driver available", now, noDriverEvent)
			metrics.DispatchNoDriverTotal.Inc()
		}
		return
	}

	// 5. Reserve new driver and transition REASSIGNING -> PENDING_ACCEPT
	assignEvent, _ := domain.NewEventEnvelope(uuid.New().String(), domain.EventRideDriverAssigned, ride.RideID, now, map[string]string{
		"rideId":   ride.RideID,
		"driverId": candidate.DriverID,
	})
	if err := w.rideRepo.UpdateRideStatus(ctx, ride.RideID, domain.StatusReassigning, ride.Version+1, domain.StatusPendingAccept, "timeout-worker", "reassigned to new driver", now, assignEvent); err != nil {
		_, _ = w.locker.ReleaseDriverLock(ctx, candidate.DriverID, ride.RideID)
		return
	}

	// 6. Notify new driver
	if w.notifier != nil {
		go func(driverID string) {
			notifyCtx, cancel := context.WithTimeout(context.Background(), w.cfg.OfferTimeout)
			defer cancel()
			_ = w.notifier.NotifyDriverOffer(notifyCtx, driverID, ride, w.cfg.OfferTimeout)
		}(candidate.DriverID)
	}
}
