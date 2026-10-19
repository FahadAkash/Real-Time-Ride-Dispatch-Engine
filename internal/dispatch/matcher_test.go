package dispatch_test

import (
	"context"
	"testing"
	"time"

	"github.com/ride-dispatch/engine/internal/dispatch"
	"github.com/ride-dispatch/engine/internal/domain"
)

type mockDriverIndex struct {
	candidates []domain.DriverCandidate
	statuses   map[string]domain.DriverState
}

func (m *mockDriverIndex) SearchNearestDrivers(ctx context.Context, region string, pickup domain.Location, radiusMeters float64, count int) ([]domain.DriverCandidate, error) {
	return m.candidates, nil
}

func (m *mockDriverIndex) GetDriverStatus(ctx context.Context, driverID string) (domain.DriverState, string, time.Time, error) {
	if state, ok := m.statuses[driverID]; ok {
		return state, "nyc", time.Now(), nil
	}
	return domain.DriverStateOffline, "nyc", time.Now(), nil
}

type mockLocker struct {
	failDriverIDs map[string]bool
	acquired      map[string]string
}

func (m *mockLocker) AcquireDriverLock(ctx context.Context, driverID, rideID string, ttl time.Duration) (bool, error) {
	if m.failDriverIDs[driverID] {
		return false, nil
	}
	if _, exists := m.acquired[driverID]; exists {
		return false, nil
	}
	m.acquired[driverID] = rideID
	return true, nil
}

func (m *mockLocker) ReleaseDriverLock(ctx context.Context, driverID, rideID string) (bool, error) {
	if current, exists := m.acquired[driverID]; exists && current == rideID {
		delete(m.acquired, driverID)
		return true, nil
	}
	return false, nil
}

func TestMatcher_RankingAndReservation(t *testing.T) {
	ctx := context.Background()

	// Setup candidates:
	// driver-1: 500m away, 4.9 rating (should be top ranked)
	// driver-2: 2000m away, 4.0 rating
	// driver-3: 100m away, but offline (should be filtered out)
	// driver-4: 300m away, 4.8 rating, but excluded (should be filtered out)
	candidates := []domain.DriverCandidate{
		{DriverID: "driver-1", DistanceMeters: 500, Rating: 4.9},
		{DriverID: "driver-2", DistanceMeters: 2000, Rating: 4.0},
		{DriverID: "driver-3", DistanceMeters: 100, Rating: 5.0},
		{DriverID: "driver-4", DistanceMeters: 300, Rating: 4.8},
	}
	statuses := map[string]domain.DriverState{
		"driver-1": domain.DriverStateAvailable,
		"driver-2": domain.DriverStateAvailable,
		"driver-3": domain.DriverStateOffline,
		"driver-4": domain.DriverStateAvailable,
	}

	geo := &mockDriverIndex{candidates: candidates, statuses: statuses}
	locker := &mockLocker{
		failDriverIDs: make(map[string]bool),
		acquired:      make(map[string]string),
	}

	matcher := dispatch.NewMatcher(geo, locker)

	pickup := domain.Location{Latitude: 40.7128, Longitude: -74.0060}
	selected, err := matcher.FindAndReserveBestDriver(ctx, "nyc", pickup, "ride-101", []string{"driver-4"}, 10, 15*time.Second)
	if err != nil {
		t.Fatalf("expected successful match, got error: %v", err)
	}

	if selected.DriverID != "driver-1" {
		t.Fatalf("expected driver-1 to be selected, got: %s", selected.DriverID)
	}
	if locker.acquired["driver-1"] != "ride-101" {
		t.Fatalf("expected driver-1 lock to be held by ride-101")
	}
}

func TestMatcher_FailoverOnLockContention(t *testing.T) {
	ctx := context.Background()

	// driver-1 is closest, but lock acquisition fails (e.g. another ride is locking it)
	// driver-2 is next best, lock acquisition succeeds
	candidates := []domain.DriverCandidate{
		{DriverID: "driver-1", DistanceMeters: 500, Rating: 4.9},
		{DriverID: "driver-2", DistanceMeters: 1000, Rating: 4.8},
	}
	statuses := map[string]domain.DriverState{
		"driver-1": domain.DriverStateAvailable,
		"driver-2": domain.DriverStateAvailable,
	}

	geo := &mockDriverIndex{candidates: candidates, statuses: statuses}
	locker := &mockLocker{
		failDriverIDs: map[string]bool{"driver-1": true}, // contention on driver-1
		acquired:      make(map[string]string),
	}

	matcher := dispatch.NewMatcher(geo, locker)
	pickup := domain.Location{Latitude: 40.7128, Longitude: -74.0060}

	selected, err := matcher.FindAndReserveBestDriver(ctx, "nyc", pickup, "ride-102", nil, 10, 15*time.Second)
	if err != nil {
		t.Fatalf("expected successful match to candidate 2, got: %v", err)
	}

	if selected.DriverID != "driver-2" {
		t.Fatalf("expected driver-2 to be reserved due to contention on driver-1, got: %s", selected.DriverID)
	}
}

func TestMatcher_NoDriverFound(t *testing.T) {
	ctx := context.Background()

	// No candidates returned
	geo := &mockDriverIndex{candidates: nil, statuses: nil}
	locker := &mockLocker{
		failDriverIDs: make(map[string]bool),
		acquired:      make(map[string]string),
	}

	matcher := dispatch.NewMatcher(geo, locker)
	pickup := domain.Location{Latitude: 40.7128, Longitude: -74.0060}

	_, err := matcher.FindAndReserveBestDriver(ctx, "nyc", pickup, "ride-103", nil, 10, 15*time.Second)
	if err != domain.ErrNoDriverFound {
		t.Fatalf("expected ErrNoDriverFound, got %v", err)
	}
}
