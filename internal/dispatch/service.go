package dispatch

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/ride-dispatch/engine/internal/domain"
	"github.com/ride-dispatch/engine/internal/platform/metrics"
)

type Config struct {
	CandidatesCount int
	LockTTL         time.Duration
	OfferTimeout    time.Duration
	MaxReassigns    int
}

type Service struct {
	cfg       Config
	matcher   *Matcher
	rideRepo  RideRepo
	idemStore IdempotencyStore
	locker    Locker
	notifier  Notifier
}

func NewService(cfg Config, matcher *Matcher, rideRepo RideRepo, idemStore IdempotencyStore, locker Locker, notifier Notifier) *Service {
	if cfg.CandidatesCount == 0 {
		cfg.CandidatesCount = 10
	}
	if cfg.LockTTL == 0 {
		cfg.LockTTL = 15 * time.Second
	}
	if cfg.OfferTimeout == 0 {
		cfg.OfferTimeout = 15 * time.Second
	}
	if cfg.MaxReassigns == 0 {
		cfg.MaxReassigns = 3
	}

	return &Service{
		cfg:       cfg,
		matcher:   matcher,
		rideRepo:  rideRepo,
		idemStore: idemStore,
		locker:    locker,
		notifier:  notifier,
	}
}

// RequestRideRequest contains input parameters for requesting a ride.
type RequestRideRequest struct {
	RiderID        string             `json:"riderId"`
	Pickup         domain.Location    `json:"pickup"`
	Dropoff        domain.Location    `json:"dropoff"`
	ServiceType    domain.ServiceType `json:"serviceType"`
	Region         string             `json:"region"`
	IdempotencyKey string             `json:"idempotencyKey"`
}

// RequestRide handles ride creation, idempotency check, driver reservation, persistence, and notification.
func (s *Service) RequestRide(ctx context.Context, req RequestRideRequest) (*domain.Ride, error) {
	start := time.Now()
	defer func() {
		metrics.DispatchLatencySeconds.Observe(time.Since(start).Seconds())
	}()

	// 1. Validate coordinates and service type
	if err := req.Pickup.Validate(); err != nil {
		metrics.DispatchRequestsTotal.WithLabelValues(metrics.ResultError).Inc()
		return nil, err
	}
	if err := req.Dropoff.Validate(); err != nil {
		metrics.DispatchRequestsTotal.WithLabelValues(metrics.ResultError).Inc()
		return nil, err
	}
	if !req.ServiceType.IsValid() {
		metrics.DispatchRequestsTotal.WithLabelValues(metrics.ResultError).Inc()
		return nil, domain.ErrInvalidServiceType
	}
	if req.IdempotencyKey == "" {
		metrics.DispatchRequestsTotal.WithLabelValues(metrics.ResultError).Inc()
		return nil, domain.ErrInvalidIdempotencyKey
	}

	rideID := uuid.New().String()

	// 2. Idempotency check: SET idem:{riderID}:{uuid} <rideID> NX EX 24h
	existingRideID, isNew, err := s.idemStore.CheckOrSetIdempotency(ctx, req.RiderID, req.IdempotencyKey, rideID, 24*time.Hour)
	if err != nil {
		metrics.DispatchRequestsTotal.WithLabelValues(metrics.ResultError).Inc()
		return nil, err
	}
	if !isNew {
		metrics.DispatchRequestsTotal.WithLabelValues(metrics.ResultDuplicate).Inc()
		return s.rideRepo.GetRideByID(ctx, existingRideID)
	}

	now := time.Now().UTC()
	ride := &domain.Ride{
		RideID:          rideID,
		RiderID:         req.RiderID,
		Pickup:          req.Pickup,
		Dropoff:         req.Dropoff,
		ServiceType:     req.ServiceType,
		IdempotencyKey:  req.IdempotencyKey,
		Version:         1,
		CreatedAt:       now,
		UpdatedAt:       now,
		ExcludedDrivers: make([]string, 0),
		History: []domain.RideHistoryEntry{
			{
				Status:    domain.StatusPendingAccept,
				Timestamp: now,
				Actor:     "rider:" + req.RiderID,
				Reason:    "ride requested",
			},
		},
	}

	// 3. Search and reserve nearest driver
	reservedDriver, err := s.matcher.FindAndReserveBestDriver(ctx, req.Region, req.Pickup, rideID, nil, s.cfg.CandidatesCount, s.cfg.LockTTL)
	if err != nil {
		if errors.Is(err, domain.ErrNoDriverFound) {
			ride.Status = domain.StatusNoDriverFound
			event, _ := domain.NewEventEnvelope(uuid.New().String(), domain.EventRideRequested, rideID, now, ride)
			_ = s.rideRepo.CreateRideWithOutbox(ctx, ride, event)
			metrics.DispatchRequestsTotal.WithLabelValues(metrics.ResultNoDriver).Inc()
			metrics.DispatchNoDriverTotal.Inc()
			return ride, domain.ErrNoDriverFound
		}
		metrics.DispatchRequestsTotal.WithLabelValues(metrics.ResultError).Inc()
		return nil, err
	}

	ride.DriverID = reservedDriver.DriverID
	ride.Status = domain.StatusPendingAccept

	// 4. Persist ride + outbox record BEFORE notifying driver
	event, err := domain.NewEventEnvelope(uuid.New().String(), domain.EventRideDriverAssigned, rideID, now, ride)
	if err != nil {
		_, _ = s.locker.ReleaseDriverLock(ctx, reservedDriver.DriverID, rideID)
		metrics.DispatchRequestsTotal.WithLabelValues(metrics.ResultError).Inc()
		return nil, err
	}

	if err := s.rideRepo.CreateRideWithOutbox(ctx, ride, event); err != nil {
		_, _ = s.locker.ReleaseDriverLock(ctx, reservedDriver.DriverID, rideID)
		metrics.DispatchRequestsTotal.WithLabelValues(metrics.ResultError).Inc()
		return nil, err
	}

	// 5. Notify driver asynchronously with timeout
	if s.notifier != nil {
		go func() {
			notifyCtx, cancel := context.WithTimeout(context.Background(), s.cfg.OfferTimeout)
			defer cancel()
			if notifyErr := s.notifier.NotifyDriverOffer(notifyCtx, reservedDriver.DriverID, ride, s.cfg.OfferTimeout); notifyErr != nil {
				metrics.PushFailuresTotal.Inc()
			}
		}()
	}

	metrics.DispatchRequestsTotal.WithLabelValues(metrics.ResultSuccess).Inc()
	return ride, nil
}

// RespondToOffer processes driver acceptance or rejection of a ride offer.
func (s *Service) RespondToOffer(ctx context.Context, driverID, rideID string, accept bool) error {
	ride, err := s.rideRepo.GetRideByID(ctx, rideID)
	if err != nil {
		return err
	}

	if ride.DriverID != driverID {
		return domain.ErrForbidden
	}
	if ride.Status != domain.StatusPendingAccept {
		return domain.ErrInvalidTransition
	}

	now := time.Now().UTC()

	if accept {
		event, err := domain.NewEventEnvelope(uuid.New().String(), domain.EventRideAccepted, rideID, now, map[string]string{
			"rideId":   rideID,
			"driverId": driverID,
		})
		if err != nil {
			return err
		}

		return s.rideRepo.UpdateRideStatus(ctx, rideID, domain.StatusPendingAccept, ride.Version, domain.StatusAccepted, "driver:"+driverID, "driver accepted offer", now, event)
	}

	// Driver rejected: release lock via compare-and-delete and mark for reassigning
	_, _ = s.locker.ReleaseDriverLock(ctx, driverID, rideID)

	event, err := domain.NewEventEnvelope(uuid.New().String(), domain.EventRideRejected, rideID, now, map[string]string{
		"rideId":   rideID,
		"driverId": driverID,
	})
	if err != nil {
		return err
	}

	metrics.DispatchReassignmentsTotal.Inc()
	return s.rideRepo.UpdateRideForReassign(ctx, rideID, ride.Version, driverID, event)
}

// CancelRide processes cancellation by rider or driver before trip starts.
func (s *Service) CancelRide(ctx context.Context, rideID, actor string) error {
	ride, err := s.rideRepo.GetRideByID(ctx, rideID)
	if err != nil {
		return err
	}

	if !domain.CanCancel(ride.Status) {
		return domain.ErrInvalidTransition
	}

	// Release driver lock if one was assigned
	if ride.DriverID != "" {
		_, _ = s.locker.ReleaseDriverLock(ctx, ride.DriverID, rideID)
	}

	now := time.Now().UTC()
	event, err := domain.NewEventEnvelope(uuid.New().String(), domain.EventRideCancelled, rideID, now, map[string]string{
		"rideId": rideID,
		"actor":  actor,
	})
	if err != nil {
		return err
	}

	return s.rideRepo.UpdateRideStatus(ctx, rideID, ride.Status, ride.Version, domain.StatusCancelled, actor, "ride cancelled", now, event)
}

// GetRide retrieves the current ride details.
func (s *Service) GetRide(ctx context.Context, rideID string) (*domain.Ride, error) {
	return s.rideRepo.GetRideByID(ctx, rideID)
}
