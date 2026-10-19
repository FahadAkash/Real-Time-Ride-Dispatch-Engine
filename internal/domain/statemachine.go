package domain

import (
	"time"
)

// allowedTransitions maps every valid origin status to its set of permitted destination statuses.
var allowedTransitions = map[RideStatus]map[RideStatus]bool{
	StatusPendingAccept: {
		StatusAccepted:      true, // Driver accepted
		StatusReassigning:   true, // Driver rejected or offer timed out
		StatusNoDriverFound: true, // No candidates remaining
		StatusCancelled:     true, // Rider or admin cancelled
	},
	StatusReassigning: {
		StatusPendingAccept: true, // Next candidate locked
		StatusNoDriverFound: true, // Max reassign attempts exhausted
		StatusCancelled:     true, // Rider cancelled while reassigning
	},
	StatusAccepted: {
		StatusStarted:   true, // Driver arrived and trip started
		StatusCancelled: true, // Allowed before trip starts
	},
	StatusStarted: {
		StatusCompleted: true, // Trip successfully completed
	},
	// Terminal states: StatusCompleted, StatusNoDriverFound, StatusCancelled
	StatusCompleted:     {},
	StatusNoDriverFound: {},
	StatusCancelled:     {},
}

// IsValidTransition checks whether transitioning from origin to target status is valid.
func IsValidTransition(from, to RideStatus) bool {
	targets, ok := allowedTransitions[from]
	if !ok {
		return false
	}
	return targets[to]
}

// CanCancel checks if a ride in the given status can still be cancelled.
func CanCancel(status RideStatus) bool {
	return status == StatusPendingAccept || status == StatusReassigning || status == StatusAccepted
}

// Transition applies a validated state transition to a Ride entity, updating its version and history.
func Transition(ride *Ride, targetStatus RideStatus, actor, reason string, timestamp time.Time) error {
	if ride == nil {
		return ErrRideNotFound
	}

	if !IsValidTransition(ride.Status, targetStatus) {
		return &TransitionError{
			From: ride.Status,
			To:   targetStatus,
		}
	}

	ride.Status = targetStatus
	ride.Version++
	ride.UpdatedAt = timestamp.UTC()
	ride.History = append(ride.History, RideHistoryEntry{
		Status:    targetStatus,
		Timestamp: timestamp.UTC(),
		Actor:     actor,
		Reason:    reason,
	})

	return nil
}
