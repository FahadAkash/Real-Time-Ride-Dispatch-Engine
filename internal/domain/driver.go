package domain

import (
	"time"
)

// DriverState represents the availability of a driver.
type DriverState string

const (
	DriverStateAvailable DriverState = "AVAILABLE"
	DriverStateBusy      DriverState = "BUSY"
	DriverStateOffline   DriverState = "OFFLINE"
)

// Vehicle describes the driver's vehicle.
type Vehicle struct {
	Make  string `json:"make" bson:"make"`
	Model string `json:"model" bson:"model"`
	Plate string `json:"plate" bson:"plate"`
	Color string `json:"color" bson:"color"`
}

// Driver represents the durable driver document in MongoDB and Redis status cache.
type Driver struct {
	DriverID   string      `json:"driverId" bson:"driverId"`
	Rating     float64     `json:"rating" bson:"rating"`
	Vehicle    Vehicle     `json:"vehicle" bson:"vehicle"`
	Region     string      `json:"region" bson:"region"`
	State      DriverState `json:"state" bson:"state"`
	LastSeenAt time.Time   `json:"lastSeenAt" bson:"lastSeenAt"`
}

// DriverCandidate represents a driver returned by geo search with computed distance and ranking score.
type DriverCandidate struct {
	DriverID       string   `json:"driverId"`
	DistanceMeters float64  `json:"distanceMeters"`
	Rating         float64  `json:"rating"`
	Location       Location `json:"location"`
	Score          float64  `json:"score"`
}
