package location_test

import (
	"math"
	"testing"
	"time"

	"github.com/ride-dispatch/engine/internal/domain"
	"github.com/ride-dispatch/engine/internal/location"
)

func TestValidator_ValidateBounds(t *testing.T) {
	bounds := location.BoundingBox{
		MinLat: 40.0,
		MaxLat: 41.0,
		MinLon: -74.5,
		MaxLon: -73.5,
	}
	v := location.NewValidator(bounds, 160.0, 2*time.Second)

	tests := []struct {
		name    string
		loc     domain.Location
		wantErr error
	}{
		{
			name:    "valid coordinate inside bounds",
			loc:     domain.Location{Latitude: 40.7128, Longitude: -74.0060},
			wantErr: nil,
		},
		{
			name:    "invalid latitude over 90",
			loc:     domain.Location{Latitude: 91.0, Longitude: -74.0060},
			wantErr: domain.ErrInvalidCoordinates,
		},
		{
			name:    "invalid longitude over 180",
			loc:     domain.Location{Latitude: 40.7128, Longitude: 181.0},
			wantErr: domain.ErrInvalidCoordinates,
		},
		{
			name:    "coordinate with NaN",
			loc:     domain.Location{Latitude: math.NaN(), Longitude: -74.0060},
			wantErr: domain.ErrInvalidCoordinates,
		},
		{
			name:    "valid coordinate outside regional bounds",
			loc:     domain.Location{Latitude: 37.7749, Longitude: -122.4194}, // San Francisco outside NY bounds
			wantErr: domain.ErrDistanceOutOfBounds,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.ValidateBounds(tt.loc)
			if tt.wantErr == nil && err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}
			if tt.wantErr != nil && err != tt.wantErr {
				t.Fatalf("expected %v, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestValidator_CheckPlausibility(t *testing.T) {
	v := location.NewValidator(location.DefaultBoundingBox, 160.0, 2*time.Second)

	now := time.Now()
	nyStart := domain.Location{Latitude: 40.7128, Longitude: -74.0060}

	// 1. Initial position with zero prevTime
	err := v.CheckPlausibility(domain.Location{}, nyStart, time.Time{}, now)
	if err != nil {
		t.Fatalf("expected nil error for initial point, got %v", err)
	}

	// 2. Timestamp in the past
	past := now.Add(-5 * time.Second)
	err = v.CheckPlausibility(nyStart, nyStart, now, past)
	if err != domain.ErrStaleLocationUpdate {
		t.Fatalf("expected ErrStaleLocationUpdate, got %v", err)
	}

	// 3. Update too fast (< minUpdateInterval of 2s)
	rapid := now.Add(1 * time.Second)
	err = v.CheckPlausibility(nyStart, nyStart, now, rapid)
	if err != domain.ErrRateLimitExceeded {
		t.Fatalf("expected ErrRateLimitExceeded, got %v", err)
	}

	// 4. Normal realistic movement: ~100m in 10s (36 km/h)
	// 0.0009 deg lat is approx 100 meters
	realisticLoc := domain.Location{Latitude: 40.7137, Longitude: -74.0060}
	realisticTime := now.Add(10 * time.Second)
	err = v.CheckPlausibility(nyStart, realisticLoc, now, realisticTime)
	if err != nil {
		t.Fatalf("expected nil error for realistic drive, got %v", err)
	}

	// 5. GPS Spoofing / Teleportation: 50km in 3s (60,000 km/h)
	teleportLoc := domain.Location{Latitude: 41.1628, Longitude: -74.0060}
	teleportTime := now.Add(3 * time.Second)
	err = v.CheckPlausibility(nyStart, teleportLoc, now, teleportTime)
	if err != domain.ErrSpeedAnomaly {
		t.Fatalf("expected ErrSpeedAnomaly for teleportation, got %v", err)
	}
}
