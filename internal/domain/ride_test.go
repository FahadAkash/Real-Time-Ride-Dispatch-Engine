package domain

import (
	"math"
	"testing"
)

func TestLocationValidation(t *testing.T) {
	tests := []struct {
		name      string
		loc       Location
		expectErr bool
	}{
		{"Valid location (New York)", Location{Latitude: 40.7128, Longitude: -74.0060}, false},
		{"Valid extremes (North Pole)", Location{Latitude: 90.0, Longitude: 0.0}, false},
		{"Valid extremes (Date line)", Location{Latitude: 0.0, Longitude: 180.0}, false},
		{"Invalid latitude too high", Location{Latitude: 90.001, Longitude: 0.0}, true},
		{"Invalid latitude too low", Location{Latitude: -90.001, Longitude: 0.0}, true},
		{"Invalid longitude too high", Location{Latitude: 0.0, Longitude: 180.001}, true},
		{"Invalid longitude too low", Location{Latitude: 0.0, Longitude: -180.001}, true},
		{"NaN latitude", Location{Latitude: math.NaN(), Longitude: 0.0}, true},
		{"Inf longitude", Location{Latitude: 0.0, Longitude: math.Inf(1)}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.loc.Validate()
			if (err != nil) != tt.expectErr {
				t.Fatalf("Validate() error = %v, expectErr = %v", err, tt.expectErr)
			}
		})
	}
}

func TestHaversineDistance(t *testing.T) {
	// NYC (Times Square: 40.7580, -73.9855) to Empire State Building (40.7484, -73.9857)
	// Approx ~1,070 meters
	loc1 := Location{Latitude: 40.7580, Longitude: -73.9855}
	loc2 := Location{Latitude: 40.7484, Longitude: -73.9857}

	dist := loc1.DistanceMeters(loc2)
	if dist < 1000 || dist > 1150 {
		t.Fatalf("expected distance around 1070m, got %.2f", dist)
	}

	// Same location should be 0
	if zeroDist := loc1.DistanceMeters(loc1); zeroDist > 0.001 {
		t.Fatalf("expected distance to self to be 0, got %.2f", zeroDist)
	}
}

func TestServiceTypeValidation(t *testing.T) {
	if !ServiceStandard.IsValid() {
		t.Errorf("expected STANDARD to be valid")
	}
	if !ServiceComfort.IsValid() {
		t.Errorf("expected COMFORT to be valid")
	}
	if !ServiceXL.IsValid() {
		t.Errorf("expected XL to be valid")
	}
	if ServiceType("FLYING_CAR").IsValid() {
		t.Errorf("expected FLYING_CAR to be invalid")
	}
}

