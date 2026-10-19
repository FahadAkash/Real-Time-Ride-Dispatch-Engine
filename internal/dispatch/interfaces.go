package dispatch

import (
	"context"
	"time"

	"github.com/ride-dispatch/engine/internal/domain"
)

// DriverIndex abstracts regional geospatial candidate searching and status lookups.
type DriverIndex interface {
	SearchNearestDrivers(ctx context.Context, region string, pickup domain.Location, radiusMeters float64, count int) ([]domain.DriverCandidate, error)
	GetDriverStatus(ctx context.Context, driverID string) (domain.DriverState, string, time.Time, error)
}

// Locker abstracts distributed locking with compare-and-delete semantics.
type Locker interface {
	AcquireDriverLock(ctx context.Context, driverID, rideID string, ttl time.Duration) (bool, error)
	ReleaseDriverLock(ctx context.Context, driverID, rideID string) (bool, error)
}

// IdempotencyStore manages 24h ride request idempotency keys.
type IdempotencyStore interface {
	CheckOrSetIdempotency(ctx context.Context, riderID, idemKey, rideID string, ttl time.Duration) (existingRideID string, isNew bool, err error)
}

// RideRepo defines durable persistence for rides and outbox events.
type RideRepo interface {
	CreateRideWithOutbox(ctx context.Context, ride *domain.Ride, event *domain.EventEnvelope) error
	GetRideByID(ctx context.Context, rideID string) (*domain.Ride, error)
	UpdateRideStatus(ctx context.Context, rideID string, expectedStatus domain.RideStatus, expectedVersion int64, newStatus domain.RideStatus, actor, reason string, at time.Time, event *domain.EventEnvelope) error
	UpdateRideForReassign(ctx context.Context, rideID string, expectedVersion int64, excludedDriver string, event *domain.EventEnvelope) error
}

// Notifier delivers push notifications to drivers.
type Notifier interface {
	NotifyDriverOffer(ctx context.Context, driverID string, ride *domain.Ride, timeout time.Duration) error
}

