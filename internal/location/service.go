package location

import (
	"context"
	"sync"
	"time"

	"github.com/ride-dispatch/engine/internal/domain"
	"github.com/ride-dispatch/engine/internal/platform/metrics"
)

// GeoStore abstracts Redis GEO and status hash operations for the location service.
type GeoStore interface {
	AddDriverLocation(ctx context.Context, region, driverID string, loc domain.Location) error
	RemoveDriverLocation(ctx context.Context, region, driverID string) error
	UpdateDriverStatus(ctx context.Context, driverID string, state domain.DriverState, region string, lastSeen time.Time, ttl time.Duration) error
	GetDriverStatus(ctx context.Context, driverID string) (domain.DriverState, string, time.Time, error)
}

type driverPositionSnapshot struct {
	loc       domain.Location
	timestamp time.Time
}

type Service struct {
	geo        GeoStore
	validator  *Validator
	statusTTL  time.Duration
	positions  sync.Map // driverID -> driverPositionSnapshot (fast in-memory plausibility lookup)
}

func NewService(geo GeoStore, validator *Validator, statusTTL time.Duration) *Service {
	if validator == nil {
		validator = NewValidator(DefaultBoundingBox, 160.0, 2*time.Second)
	}
	if statusTTL == 0 {
		statusTTL = 30 * time.Second
	}

	return &Service{
		geo:       geo,
		validator: validator,
		statusTTL: statusTTL,
	}
}

// UpdateLocation validates coordinates, verifies movement plausibility, and updates Redis GEO and status cache.
func (s *Service) UpdateLocation(ctx context.Context, driverID string, loc domain.Location, region string, state domain.DriverState) error {
	now := time.Now().UTC()

	// 1. Validate coordinate bounds
	if err := s.validator.ValidateBounds(loc); err != nil {
		return err
	}

	// 2. Check plausibility against previous update (speed anomaly / rate limit)
	if val, ok := s.positions.Load(driverID); ok {
		prev := val.(driverPositionSnapshot)
		if err := s.validator.CheckPlausibility(prev.loc, loc, prev.timestamp, now); err != nil {
			return err
		}
	}

	// Update local memory snapshot
	s.positions.Store(driverID, driverPositionSnapshot{loc: loc, timestamp: now})

	// 3. Write GEOADD to regional GEO set
	if state == domain.DriverStateAvailable {
		if err := s.geo.AddDriverLocation(ctx, region, driverID, loc); err != nil {
			return err
		}
	} else {
		// Non-available drivers (Busy/Offline) are removed from GEO availability set
		_ = s.geo.RemoveDriverLocation(ctx, region, driverID)
	}

	// 4. Refresh driver status hash with TTL (~30s)
	if err := s.geo.UpdateDriverStatus(ctx, driverID, state, region, now, s.statusTTL); err != nil {
		return err
	}

	metrics.DriverLocationUpdatesTotal.Inc()
	return nil
}

