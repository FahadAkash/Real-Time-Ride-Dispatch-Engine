package domain

import (
	"math"
	"time"
)

// RideStatus represents the current state of a ride in the state machine.
type RideStatus string

const (
	StatusPendingAccept RideStatus = "PENDING_ACCEPT"
	StatusAccepted      RideStatus = "ACCEPTED"
	StatusStarted       RideStatus = "STARTED"
	StatusCompleted     RideStatus = "COMPLETED"
	StatusReassigning   RideStatus = "REASSIGNING"
	StatusNoDriverFound RideStatus = "NO_DRIVER_FOUND"
	StatusCancelled     RideStatus = "CANCELLED"
)

// ServiceType denotes the category of ride service requested.
type ServiceType string

const (
	ServiceStandard ServiceType = "STANDARD"
	ServiceComfort  ServiceType = "COMFORT"
	ServiceXL       ServiceType = "XL"
)

// IsValid checks if the service type is supported.
func (s ServiceType) IsValid() bool {
	switch s {
	case ServiceStandard, ServiceComfort, ServiceXL:
		return true
	default:
		return false
	}
}

// Location represents geographic coordinates (WGS84).
type Location struct {
	Latitude  float64 `json:"latitude" bson:"latitude"`
	Longitude float64 `json:"longitude" bson:"longitude"`
}

// Validate ensures latitude and longitude are valid numbers within global coordinates bounds.
func (l Location) Validate() error {
	if math.IsNaN(l.Latitude) || math.IsInf(l.Latitude, 0) || l.Latitude < -90.0 || l.Latitude > 90.0 {
		return ErrInvalidCoordinates
	}
	if math.IsNaN(l.Longitude) || math.IsInf(l.Longitude, 0) || l.Longitude < -180.0 || l.Longitude > 180.0 {
		return ErrInvalidCoordinates
	}
	return nil
}

// DistanceMeters calculates great-circle distance between two locations in meters using Haversine formula.
func (l Location) DistanceMeters(other Location) float64 {
	const earthRadiusMeters = 6371000.0

	lat1Rad := l.Latitude * math.Pi / 180.0
	lat2Rad := other.Latitude * math.Pi / 180.0
	deltaLat := (other.Latitude - l.Latitude) * math.Pi / 180.0
	deltaLon := (other.Longitude - l.Longitude) * math.Pi / 180.0

	a := math.Sin(deltaLat/2.0)*math.Sin(deltaLat/2.0) +
		math.Cos(lat1Rad)*math.Cos(lat2Rad)*
			math.Sin(deltaLon/2.0)*math.Sin(deltaLon/2.0)
	c := 2.0 * math.Atan2(math.Sqrt(a), math.Sqrt(1.0-a))

	return earthRadiusMeters * c
}

// RideHistoryEntry logs state transitions along with actor and reason.
type RideHistoryEntry struct {
	Status    RideStatus `json:"status" bson:"status"`
	Timestamp time.Time  `json:"timestamp" bson:"timestamp"`
	Actor     string     `json:"actor" bson:"actor"`
	Reason    string     `json:"reason,omitempty" bson:"reason,omitempty"`
}

// Ride represents the durable ride document in MongoDB.
type Ride struct {
	RideID          string             `json:"rideId" bson:"rideId"`
	RiderID         string             `json:"riderId" bson:"riderId"`
	DriverID        string             `json:"driverId,omitempty" bson:"driverId,omitempty"`
	Status          RideStatus         `json:"status" bson:"status"`
	Pickup          Location           `json:"pickup" bson:"pickup"`
	Dropoff         Location           `json:"dropoff" bson:"dropoff"`
	ServiceType     ServiceType        `json:"serviceType" bson:"serviceType"`
	IdempotencyKey  string             `json:"idempotencyKey,omitempty" bson:"idempotencyKey,omitempty"`
	Version         int64              `json:"version" bson:"version"`
	ReassignCount   int                `json:"reassignCount" bson:"reassignCount"`
	ExcludedDrivers []string           `json:"excludedDrivers,omitempty" bson:"excludedDrivers,omitempty"`
	CreatedAt       time.Time          `json:"createdAt" bson:"createdAt"`
	UpdatedAt       time.Time          `json:"updatedAt" bson:"updatedAt"`
	History         []RideHistoryEntry `json:"history" bson:"history"`
}

