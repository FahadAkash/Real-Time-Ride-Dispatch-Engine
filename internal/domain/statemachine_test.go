package domain

import (
	"errors"
	"testing"
	"time"
)

func TestStateTransitions(t *testing.T) {
	tests := []struct {
		name        string
		from        RideStatus
		to          RideStatus
		expectValid bool
	}{
		// Valid from PENDING_ACCEPT
		{"Pending to Accepted", StatusPendingAccept, StatusAccepted, true},
		{"Pending to Reassigning", StatusPendingAccept, StatusReassigning, true},
		{"Pending to NoDriverFound", StatusPendingAccept, StatusNoDriverFound, true},
		{"Pending to Cancelled", StatusPendingAccept, StatusCancelled, true},
		{"Pending to Started (invalid)", StatusPendingAccept, StatusStarted, false},
		{"Pending to Completed (invalid)", StatusPendingAccept, StatusCompleted, false},

		// Valid from REASSIGNING
		{"Reassigning to Pending", StatusReassigning, StatusPendingAccept, true},
		{"Reassigning to NoDriverFound", StatusReassigning, StatusNoDriverFound, true},
		{"Reassigning to Cancelled", StatusReassigning, StatusCancelled, true},
		{"Reassigning to Accepted (invalid directly)", StatusReassigning, StatusAccepted, false},
		{"Reassigning to Started (invalid)", StatusReassigning, StatusStarted, false},

		// Valid from ACCEPTED
		{"Accepted to Started", StatusAccepted, StatusStarted, true},
		{"Accepted to Cancelled", StatusAccepted, StatusCancelled, true},
		{"Accepted to Completed (invalid directly)", StatusAccepted, StatusCompleted, false},
		{"Accepted to Pending (invalid)", StatusAccepted, StatusPendingAccept, false},

		// Valid from STARTED
		{"Started to Completed", StatusStarted, StatusCompleted, true},
		{"Started to Cancelled (invalid)", StatusStarted, StatusCancelled, false},
		{"Started to Reassigning (invalid)", StatusStarted, StatusReassigning, false},

		// Terminal states cannot transition to anything
		{"Completed to any (invalid)", StatusCompleted, StatusStarted, false},
		{"NoDriver to any (invalid)", StatusNoDriverFound, StatusPendingAccept, false},
		{"Cancelled to any (invalid)", StatusCancelled, StatusPendingAccept, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsValidTransition(tt.from, tt.to)
			if got != tt.expectValid {
				t.Fatalf("IsValidTransition(%s, %s) = %v, expected %v", tt.from, tt.to, got, tt.expectValid)
			}
		})
	}
}

func TestTransitionFunction(t *testing.T) {
	now := time.Now().UTC()
	ride := &Ride{
		RideID:    "ride-123",
		RiderID:   "rider-456",
		Status:    StatusPendingAccept,
		Version:   1,
		CreatedAt: now,
		UpdatedAt: now,
		History:   make([]RideHistoryEntry, 0),
	}

	// Valid transition: PENDING_ACCEPT -> ACCEPTED
	err := Transition(ride, StatusAccepted, "driver-789", "driver accepted offer", now.Add(5*time.Second))
	if err != nil {
		t.Fatalf("unexpected error on valid transition: %v", err)
	}
	if ride.Status != StatusAccepted {
		t.Errorf("expected status %s, got %s", StatusAccepted, ride.Status)
	}
	if ride.Version != 2 {
		t.Errorf("expected version 2, got %d", ride.Version)
	}
	if len(ride.History) != 1 {
		t.Fatalf("expected 1 history entry, got %d", len(ride.History))
	}
	if ride.History[0].Status != StatusAccepted {
		t.Errorf("expected history status %s, got %s", StatusAccepted, ride.History[0].Status)
	}

	// Invalid transition: ACCEPTED -> COMPLETED directly
	err = Transition(ride, StatusCompleted, "system", "invalid jump", now.Add(10*time.Second))
	if err == nil {
		t.Fatalf("expected error on invalid transition from ACCEPTED to COMPLETED, got nil")
	}
	var transErr *TransitionError
	if !errors.As(err, &transErr) {
		t.Fatalf("expected TransitionError, got %T: %v", err, err)
	}
	if transErr.From != StatusAccepted || transErr.To != StatusCompleted {
		t.Errorf("unexpected transition details: from=%s, to=%s", transErr.From, transErr.To)
	}
}

func TestCanCancel(t *testing.T) {
	if !CanCancel(StatusPendingAccept) {
		t.Errorf("expected PENDING_ACCEPT to be cancellable")
	}
	if !CanCancel(StatusReassigning) {
		t.Errorf("expected REASSIGNING to be cancellable")
	}
	if !CanCancel(StatusAccepted) {
		t.Errorf("expected ACCEPTED to be cancellable")
	}
	if CanCancel(StatusStarted) {
		t.Errorf("expected STARTED not to be cancellable")
	}
	if CanCancel(StatusCompleted) {
		t.Errorf("expected COMPLETED not to be cancellable")
	}
}
