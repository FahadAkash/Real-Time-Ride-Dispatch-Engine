package integration_test

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

	"github.com/ride-dispatch/engine/internal/domain"
	mongostore "github.com/ride-dispatch/engine/internal/store/mongo"
	redisstore "github.com/ride-dispatch/engine/internal/store/redis"
)

func isServiceAvailable(addr string, timeout time.Duration) bool {
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func TestRedisIntegration_LocksAndGeo(t *testing.T) {
	redisAddr := os.Getenv("REDIS_ADDRS")
	if redisAddr == "" {
		redisAddr = "192.168.0.113:6379"
	}
	if !isServiceAvailable(redisAddr, 500*time.Millisecond) {
		t.Skipf("Redis server not available at %s; skipping integration test", redisAddr)
	}

	ctx := context.Background()
	rClient, err := redisstore.NewClient(redisstore.Config{
		Addrs: []string{redisAddr},
	})
	if err != nil {
		t.Skipf("Failed to initialize Redis client: %v; skipping", err)
	}
	defer rClient.Close()

	if err := rClient.Raw().Ping(ctx).Err(); err != nil {
		t.Skipf("Cannot ping Redis: %v; skipping", err)
	}

	driverID := "driver-itest-1"
	region := "downtown"
	loc := domain.Location{Latitude: 40.7128, Longitude: -74.0060}

	// 1. Update location & status
	if err := rClient.AddDriverLocation(ctx, region, driverID, loc); err != nil {
		t.Fatalf("failed to add driver location: %v", err)
	}
	if err := rClient.UpdateDriverStatus(ctx, driverID, domain.DriverStateAvailable, region, time.Now(), 30*time.Second); err != nil {
		t.Fatalf("failed to set driver status: %v", err)
	}

	// 2. Search
	candidates, err := rClient.SearchNearestDrivers(ctx, region, loc, 5000, 10)
	if err != nil {
		t.Fatalf("failed to search drivers: %v", err)
	}
	found := false
	for _, c := range candidates {
		if c.DriverID == driverID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected to find seeded driver %s in geo search", driverID)
	}

	// 3. Lock acquisition
	acquired, err := rClient.AcquireDriverLock(ctx, driverID, "ride-itest-101", 10*time.Second)
	if err != nil || !acquired {
		t.Fatalf("expected lock acquisition to succeed, got %v, %v", acquired, err)
	}

	// Double-locking same driver should fail
	acquiredAgain, err := rClient.AcquireDriverLock(ctx, driverID, "ride-itest-102", 10*time.Second)
	if err != nil || acquiredAgain {
		t.Fatalf("expected duplicate lock acquisition to fail, got %v, %v", acquiredAgain, err)
	}

	// Release with wrong rideID should return false (compare-and-delete)
	releasedWrong, err := rClient.ReleaseDriverLock(ctx, driverID, "ride-itest-102")
	if err != nil || releasedWrong {
		t.Fatalf("expected wrong release to return false, got %v", releasedWrong)
	}

	// Release with correct rideID should succeed
	releasedCorrect, err := rClient.ReleaseDriverLock(ctx, driverID, "ride-itest-101")
	if err != nil || !releasedCorrect {
		t.Fatalf("expected correct release to succeed, got %v", releasedCorrect)
	}
}

func TestMongoIntegration_RideLifecycle(t *testing.T) {
	mongoURI := os.Getenv("MONGO_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://192.168.0.113:27017"
	}
	mongoHost := "192.168.0.113:27017"
	if !isServiceAvailable(mongoHost, 500*time.Millisecond) {
		t.Skipf("MongoDB server not available at %s; skipping integration test", mongoHost)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	mStore, err := mongostore.NewStore(ctx, mongostore.Config{
		URI:      mongoURI,
		Database: "dispatch_itest",
	})
	if err != nil {
		t.Skipf("Cannot connect to MongoDB: %v; skipping", err)
	}
	defer func() { _ = mStore.Close(ctx) }()

	ride := &domain.Ride{
		RideID:      "ride-itest-201",
		RiderID:     "rider-itest-1",
		DriverID:    "driver-itest-2",
		Status:      domain.StatusPendingAccept,
		Pickup:      domain.Location{Latitude: 40.7128, Longitude: -74.0060},
		Dropoff:     domain.Location{Latitude: 40.7306, Longitude: -73.9352},
		ServiceType: domain.ServiceStandard,
		Version:     1,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}

	event, _ := domain.NewEventEnvelope("event-itest-1", domain.EventRideRequested, ride.RideID, time.Now().UTC(), map[string]string{"rideId": ride.RideID})

	// Create with outbox
	if err := mStore.CreateRideWithOutbox(ctx, ride, event); err != nil {
		t.Fatalf("failed to create ride with outbox: %v", err)
	}

	// Read back
	retrieved, err := mStore.GetRideByID(ctx, ride.RideID)
	if err != nil {
		t.Fatalf("failed to retrieve ride: %v", err)
	}
	if retrieved.RideID != ride.RideID || retrieved.Status != domain.StatusPendingAccept {
		t.Fatalf("unexpected retrieved ride: %+v", retrieved)
	}

	// Optimistic locking update
	acceptEvent, _ := domain.NewEventEnvelope("event-itest-2", domain.EventRideAccepted, ride.RideID, time.Now().UTC(), map[string]string{"rideId": ride.RideID})
	err = mStore.UpdateRideStatus(ctx, ride.RideID, domain.StatusPendingAccept, 1, domain.StatusAccepted, "driver-itest-2", "accepted", time.Now().UTC(), acceptEvent)
	if err != nil {
		t.Fatalf("expected successful status transition, got: %v", err)
	}

	// Conflicting update with stale version must fail with ErrOptimisticLockConflict
	staleEvent, _ := domain.NewEventEnvelope("event-itest-3", domain.EventRideStarted, ride.RideID, time.Now().UTC(), map[string]string{"rideId": ride.RideID})
	err = mStore.UpdateRideStatus(ctx, ride.RideID, domain.StatusAccepted, 1, domain.StatusStarted, "driver-itest-2", "started", time.Now().UTC(), staleEvent)
	if err != domain.ErrOptimisticLockConflict {
		t.Fatalf("expected ErrOptimisticLockConflict, got: %v", err)
	}
}
