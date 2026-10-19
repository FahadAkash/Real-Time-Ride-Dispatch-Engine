package domain

import (
	"encoding/json"
	"time"
)

// Standard event types for ride lifecycle.
const (
	EventRideRequested      = "ride.requested"
	EventRideDriverAssigned = "ride.driver-assigned"
	EventRideAccepted       = "ride.accepted"
	EventRideRejected       = "ride.rejected"
	EventRideStarted        = "ride.started"
	EventRideCompleted      = "ride.completed"
	EventRideCancelled      = "ride.cancelled"
)

// EventEnvelope defines the standard message format for all Kafka events per AGENTS.md Section 9.
type EventEnvelope struct {
	EventID       string          `json:"eventId" bson:"eventId"`
	Type          string          `json:"type" bson:"type"`
	RideID        string          `json:"rideId" bson:"rideId"`
	OccurredAt    time.Time       `json:"occurredAt" bson:"occurredAt"`
	SchemaVersion int             `json:"schemaVersion" bson:"schemaVersion"`
	Payload       json.RawMessage `json:"payload" bson:"payload"`
}

// OutboxRecord represents an event stored in MongoDB within the same transaction as the ride state mutation.
type OutboxRecord struct {
	ID            string          `json:"id" bson:"_id"`
	EventID       string          `json:"eventId" bson:"eventId"`
	Topic         string          `json:"topic" bson:"topic"`
	Key           string          `json:"key" bson:"key"` // rideID for strict partition ordering
	Payload       json.RawMessage `json:"payload" bson:"payload"`
	CreatedAt     time.Time       `json:"createdAt" bson:"createdAt"`
	Published     bool            `json:"published" bson:"published"`
	PublishedAt   *time.Time      `json:"publishedAt,omitempty" bson:"publishedAt,omitempty"`
	RetryCount    int             `json:"retryCount" bson:"retryCount"`
	LastError     string          `json:"lastError,omitempty" bson:"lastError,omitempty"`
}

// NewEventEnvelope constructs a standardized event envelope.
func NewEventEnvelope(eventID, eventType, rideID string, occurredAt time.Time, payload any) (*EventEnvelope, error) {
	bytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return &EventEnvelope{
		EventID:       eventID,
		Type:          eventType,
		RideID:        rideID,
		OccurredAt:    occurredAt.UTC(),
		SchemaVersion: 1,
		Payload:       bytes,
	}, nil
}

