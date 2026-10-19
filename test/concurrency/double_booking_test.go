package concurrency_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ride-dispatch/engine/internal/dispatch"
	"github.com/ride-dispatch/engine/internal/domain"
)

// AtomicMemLocker emulates Redis SET NX and Lua compare-and-delete in memory for concurrency verification.
type AtomicMemLocker struct {
	mu    sync.Mutex
	locks map[string]string // key -> owner rideID
}

func NewAtomicMemLocker() *AtomicMemLocker {
	return &AtomicMemLocker{
		locks: make(map[string]string),
	}
}

func (l *AtomicMemLocker) AcquireDriverLock(ctx context.Context, driverID, rideID string, ttl time.Duration) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	key := fmt.Sprintf("driver:%s:lock", driverID)
	if _, exists := l.locks[key]; exists {
		return false, nil
	}
	l.locks[key] = rideID
	return true, nil
}

func (l *AtomicMemLocker) ReleaseDriverLock(ctx context.Context, driverID, rideID string) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	key := fmt.Sprintf("driver:%s:lock", driverID)
	if owner, exists := l.locks[key]; exists && owner == rideID {
		delete(l.locks, key)
		return true, nil
	}
	return false, nil
}

// MockDriverIndex returns 1 single driver candidate.
type MockDriverIndex struct {
	driverID string
}

func (m *MockDriverIndex) SearchNearestDrivers(ctx context.Context, region string, pickup domain.Location, radiusMeters float64, count int) ([]domain.DriverCandidate, error) {
	return []domain.DriverCandidate{
		{
			DriverID:       m.driverID,
			DistanceMeters: 500.0,
			Rating:         4.9,
			Location:       pickup,
		},
	}, nil
}

func (m *MockDriverIndex) GetDriverStatus(ctx context.Context, driverID string) (domain.DriverState, string, time.Time, error) {
	return domain.DriverStateAvailable, "downtown", time.Now(), nil
}

// TestDoubleBookingRaceConditions verifies that when N concurrent dispatch requests
// race for 1 single available driver, EXACTLY ONE reservation succeeds, zero double-booking occurs,
// and compare-and-delete prevents unauthorized lock releases.
func TestDoubleBookingRaceConditions(t *testing.T) {
	const concurrentRequests = 100
	const targetDriverID = "driver-alpha-001"

	locker := NewAtomicMemLocker()
	geo := &MockDriverIndex{driverID: targetDriverID}
	matcher := dispatch.NewMatcher(geo, locker)

	var (
		wg             sync.WaitGroup
		successCounter int64
		failCounter    int64
		winningRideID  string
		winningMu      sync.Mutex
	)

	startBarrier := make(chan struct{})
	ctx := context.Background()

	for i := 0; i < concurrentRequests; i++ {
		wg.Add(1)
		rideID := fmt.Sprintf("ride-uuid-%04d", i)

		go func(rID string) {
			defer wg.Done()

			// Block on barrier until all goroutines are spawned
			<-startBarrier

			pickup := domain.Location{Latitude: 40.7128, Longitude: -74.0060}
			res, err := matcher.FindAndReserveBestDriver(ctx, "downtown", pickup, rID, nil, 5, 15*time.Second)
			if err == nil && res != nil {
				atomic.AddInt64(&successCounter, 1)
				winningMu.Lock()
				winningRideID = rID
				winningMu.Unlock()
			} else {
				atomic.AddInt64(&failCounter, 1)
			}
		}(rideID)
	}

	// Release all goroutines simultaneously
	close(startBarrier)
	wg.Wait()

	// 1. Assert exactly one winner
	if successCounter != 1 {
		t.Fatalf("CRITICAL: Double booking detected! Expected exactly 1 reservation winner, got %d", successCounter)
	}

	// 2. Assert N - 1 rejections
	if failCounter != concurrentRequests-1 {
		t.Fatalf("Expected %d failed reservations, got %d", concurrentRequests-1, failCounter)
	}

	// 3. Test Compare-and-Delete: An unauthorized ride cannot release the winning driver's lock
	fraudulentRideID := "ride-uuid-imposter"
	released, err := locker.ReleaseDriverLock(ctx, targetDriverID, fraudulentRideID)
	if err != nil {
		t.Fatalf("unexpected error on release attempt: %v", err)
	}
	if released {
		t.Fatalf("CRITICAL: Imposter ride %s was able to release lock owned by %s!", fraudulentRideID, winningRideID)
	}

	// 4. Test Compare-and-Delete: Legitimate winning ride successfully releases the lock
	released, err = locker.ReleaseDriverLock(ctx, targetDriverID, winningRideID)
	if err != nil {
		t.Fatalf("unexpected error on legitimate release: %v", err)
	}
	if !released {
		t.Fatalf("Legitimate owner %s failed to release lock", winningRideID)
	}

	// 5. Verify the driver is now free for reservation again
	newRideID := "ride-uuid-subsequent"
	pickup := domain.Location{Latitude: 40.7128, Longitude: -74.0060}
	res, err := matcher.FindAndReserveBestDriver(ctx, "downtown", pickup, newRideID, nil, 5, 15*time.Second)
	if err != nil || res == nil {
		t.Fatalf("Subsequent reservation failed after driver lock was released: %v", err)
	}
}
