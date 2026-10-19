package domain

import (
	"errors"
	"fmt"
)

var (
	// State machine errors
	ErrInvalidTransition      = errors.New("invalid ride status transition")
	ErrOptimisticLockConflict = errors.New("optimistic lock conflict: version or status mismatch")

	// Resource errors
	ErrRideNotFound      = errors.New("ride not found")
	ErrDriverNotFound    = errors.New("driver not found")
	ErrDriverUnavailable = errors.New("driver is not available")
	ErrNoDriverFound     = errors.New("no suitable driver found in radius")

	// Concurrency & reservation errors
	ErrLockAcquisitionFailed = errors.New("failed to acquire distributed lock")
	ErrDoubleBookingDetected = errors.New("driver already reserved for another ride")
	ErrDuplicateRequest      = errors.New("duplicate request with same idempotency key")
	ErrInvalidIdempotencyKey = errors.New("invalid or missing idempotency key")

	// Authentication & Authorization errors
	ErrUnauthorized    = errors.New("unauthorized: missing or invalid credentials")
	ErrForbidden       = errors.New("forbidden: caller does not own the requested resource")
	ErrInvalidToken    = errors.New("invalid token: failed signature or claims verification")
	ErrExpiredToken    = errors.New("token has expired")
	ErrInvalidUserRole = errors.New("user role not permitted for this action")

	// Validation & Plausibility errors
	ErrInvalidCoordinates  = errors.New("coordinates out of bounds or invalid float")
	ErrSpeedAnomaly        = errors.New("location update exceeds plausible maximum speed threshold")
	ErrRateLimitExceeded   = errors.New("rate limit exceeded")
	ErrInvalidServiceType  = errors.New("unsupported service type")
	ErrDistanceOutOfBounds = errors.New("pickup and dropoff distance is out of bounds")
	ErrStaleLocationUpdate = errors.New("location update is older than last recorded update")
)

// TransitionError records details of an illegal state transition attempt.
type TransitionError struct {
	From RideStatus
	To   RideStatus
}

func (e *TransitionError) Error() string {
	return fmt.Sprintf("%s: cannot transition from %s to %s", ErrInvalidTransition, e.From, e.To)
}

func (e *TransitionError) Unwrap() error {
	return ErrInvalidTransition
}
