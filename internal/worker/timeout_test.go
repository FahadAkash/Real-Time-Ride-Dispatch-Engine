package worker_test

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/ride-dispatch/engine/internal/domain"
	"github.com/ride-dispatch/engine/internal/worker"
)

type mockMatcher struct {
	candidate *domain.DriverCandidate
	err       error
}

func (m *mockMatcher) FindAndReserveBestDriver(ctx context.Context, region string, pickup domain.Location, rideID string, excludedDrivers []string, maxCandidates int, lockTTL time.Duration) (*domain.DriverCandidate, error) {
	return m.candidate, m.err
}

type mockLocker struct {
	releasedDriver string
	releasedRide   string
}

func (m *mockLocker) ReleaseDriverLock(ctx context.Context, driverID, rideID string) (bool, error) {
	m.releasedDriver = driverID
	m.releasedRide = rideID
	return true, nil
}

type mockRideRepo struct {
	expiredRides      []*domain.Ride
	reassignedRideID  string
	excludedDriver    string
	finalStatus       domain.RideStatus
	conflictOnReasign bool
}

func (m *mockRideRepo) GetActiveRidesForTimeout(ctx context.Context, olderThan time.Time, limit int) ([]*domain.Ride, error) {
	return m.expiredRides, nil
}

func (m *mockRideRepo) UpdateRideForReassign(ctx context.Context, rideID string, expectedVersion int64, excludedDriver string, event *domain.EventEnvelope) error {
	if m.conflictOnReasign {
		return domain.ErrOptimisticLockConflict
	}
	m.reassignedRideID = rideID
	m.excludedDriver = excludedDriver
	return nil
}

func (m *mockRideRepo) UpdateRideStatus(ctx context.Context, rideID string, expectedStatus domain.RideStatus, expectedVersion int64, newStatus domain.RideStatus, actor, reason string, at time.Time, event *domain.EventEnvelope) error {
	m.finalStatus = newStatus
	return nil
}

type mockNotifier struct {
	mu             sync.Mutex
	notifiedDriver string
	ch             chan string
}

func newMockNotifier() *mockNotifier {
	return &mockNotifier{ch: make(chan string, 10)}
}

func (m *mockNotifier) NotifyDriverOffer(ctx context.Context, driverID string, ride *domain.Ride, timeout time.Duration) error {
	m.mu.Lock()
	m.notifiedDriver = driverID
	m.mu.Unlock()
	m.ch <- driverID
	return nil
}

func TestTimeoutWorker_Reassignment(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	ride := &domain.Ride{
		RideID:          "ride-test-1",
		RiderID:         "rider-1",
		DriverID:        "driver-old",
		Status:          domain.StatusPendingAccept,
		Version:         1,
		ReassignCount:   0,
		ExcludedDrivers: []string{},
		Pickup:          domain.Location{Latitude: 40.7128, Longitude: -74.0060},
	}

	repo := &mockRideRepo{expiredRides: []*domain.Ride{ride}}
	locker := &mockLocker{}
	matcher := &mockMatcher{
		candidate: &domain.DriverCandidate{DriverID: "driver-new", DistanceMeters: 400},
	}
	notifier := newMockNotifier()

	cfg := worker.TimeoutWorkerConfig{
		OfferTimeout:    10 * time.Second,
		MaxReassigns:    3,
		CandidatesCount: 5,
		BatchSize:       10,
	}

	w := worker.NewTimeoutWorker(cfg, matcher, locker, repo, notifier, logger)
	w.ProcessExpiredOffers(ctx)

	if locker.releasedDriver != "driver-old" || locker.releasedRide != "ride-test-1" {
		t.Fatalf("expected lock released for driver-old on ride-test-1, got driver: %s, ride: %s", locker.releasedDriver, locker.releasedRide)
	}

	if repo.reassignedRideID != "ride-test-1" || repo.excludedDriver != "driver-old" {
		t.Fatalf("expected ride-test-1 reassigned with driver-old excluded, got: %s, %s", repo.reassignedRideID, repo.excludedDriver)
	}

	select {
	case driver := <-notifier.ch:
		if driver != "driver-new" {
			t.Fatalf("expected new candidate driver-new to be notified, got: %s", driver)
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("timed out waiting for notification")
	}
}

func TestTimeoutWorker_MaxReassignmentsExceeded(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	ride := &domain.Ride{
		RideID:        "ride-test-2",
		DriverID:      "driver-old",
		Status:        domain.StatusPendingAccept,
		Version:       4,
		ReassignCount: 3, // Already hit MaxReassigns
	}

	repo := &mockRideRepo{expiredRides: []*domain.Ride{ride}}
	locker := &mockLocker{}
	matcher := &mockMatcher{}
	notifier := newMockNotifier()

	cfg := worker.TimeoutWorkerConfig{
		OfferTimeout: 10 * time.Second,
		MaxReassigns: 3,
		BatchSize:    10,
	}

	w := worker.NewTimeoutWorker(cfg, matcher, locker, repo, notifier, logger)
	w.ProcessExpiredOffers(ctx)

	if repo.finalStatus != domain.StatusNoDriverFound {
		t.Fatalf("expected status to transition to NO_DRIVER_FOUND, got: %s", repo.finalStatus)
	}
}

func TestTimeoutWorker_OptimisticLockConflict(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	ride := &domain.Ride{
		RideID:        "ride-test-3",
		DriverID:      "driver-old",
		Status:        domain.StatusPendingAccept,
		Version:       1,
		ReassignCount: 0,
	}

	repo := &mockRideRepo{
		expiredRides:      []*domain.Ride{ride},
		conflictOnReasign: true, // Concurrent update happened
	}
	locker := &mockLocker{}
	matcher := &mockMatcher{}
	notifier := newMockNotifier()

	cfg := worker.TimeoutWorkerConfig{
		OfferTimeout: 10 * time.Second,
		MaxReassigns: 3,
		BatchSize:    10,
	}

	w := worker.NewTimeoutWorker(cfg, matcher, locker, repo, notifier, logger)
	w.ProcessExpiredOffers(ctx)

	select {
	case driver := <-notifier.ch:
		t.Fatalf("expected no notification on optimistic lock conflict, got: %s", driver)
	case <-time.After(100 * time.Millisecond):
		// Expected: no notification sent
	}
}

