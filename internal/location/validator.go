package location

import (
	"time"

	"github.com/ride-dispatch/engine/internal/domain"
)

// BoundingBox defines the geographic limits for a valid service region.
type BoundingBox struct {
	MinLat float64
	MaxLat float64
	MinLon float64
	MaxLon float64
}

// DefaultBoundingBox covers global coordinate extremes if region is unbounded.
var DefaultBoundingBox = BoundingBox{
	MinLat: -90.0,
	MaxLat: 90.0,
	MinLon: -180.0,
	MaxLon: 180.0,
}

// Validator enforces plausibility checks on incoming location updates.
type Validator struct {
	bounds            BoundingBox
	maxSpeedMps       float64       // Max speed in meters per second (e.g. 45 m/s ≈ 162 km/h)
	minUpdateInterval time.Duration // Rate limit: ~1 update / 2s / driver
}

func NewValidator(bounds BoundingBox, maxSpeedKmh float64, minUpdateInterval time.Duration) *Validator {
	if maxSpeedKmh == 0 {
		maxSpeedKmh = 160.0 // 160 km/h default
	}
	if minUpdateInterval == 0 {
		minUpdateInterval = 2 * time.Second
	}

	return &Validator{
		bounds:            bounds,
		maxSpeedMps:       maxSpeedKmh / 3.6,
		minUpdateInterval: minUpdateInterval,
	}
}

// ValidateBounds verifies that coordinates are valid and lie within the service area bounding box.
func (v *Validator) ValidateBounds(loc domain.Location) error {
	if err := loc.Validate(); err != nil {
		return err
	}
	if loc.Latitude < v.bounds.MinLat || loc.Latitude > v.bounds.MaxLat {
		return domain.ErrDistanceOutOfBounds
	}
	if loc.Longitude < v.bounds.MinLon || loc.Longitude > v.bounds.MaxLon {
		return domain.ErrDistanceOutOfBounds
	}
	return nil
}

// CheckPlausibility inspects consecutive updates to detect GPS spoofing / teleportation anomalies.
func (v *Validator) CheckPlausibility(prevLoc, newLoc domain.Location, prevTime, newTime time.Time) error {
	if !prevTime.IsZero() && newTime.Before(prevTime) {
		return domain.ErrStaleLocationUpdate
	}

	if prevTime.IsZero() {
		return nil
	}

	elapsed := newTime.Sub(prevTime)
	if elapsed < v.minUpdateInterval {
		return domain.ErrRateLimitExceeded
	}

	// Calculate distance and speed
	distMeters := prevLoc.DistanceMeters(newLoc)
	speedMps := distMeters / elapsed.Seconds()

	if speedMps > v.maxSpeedMps {
		return domain.ErrSpeedAnomaly
	}

	return nil
}

