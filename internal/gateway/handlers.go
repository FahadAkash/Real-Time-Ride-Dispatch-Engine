package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/ride-dispatch/engine/internal/dispatch"
	"github.com/ride-dispatch/engine/internal/domain"
)

type DispatchService interface {
	RequestRide(ctx context.Context, req dispatch.RequestRideRequest) (*domain.Ride, error)
	RespondToOffer(ctx context.Context, driverID, rideID string, accept bool) error
	CancelRide(ctx context.Context, rideID, actor string) error
	GetRide(ctx context.Context, rideID string) (*domain.Ride, error)
}

type LocationService interface {
	UpdateLocation(ctx context.Context, driverID string, loc domain.Location, region string, state domain.DriverState) error
}

type Handlers struct {
	dispatchSvc DispatchService
	locationSvc LocationService
}

func NewHandlers(dispatchSvc DispatchService, locationSvc LocationService) *Handlers {
	return &Handlers{
		dispatchSvc: dispatchSvc,
		locationSvc: locationSvc,
	}
}

// RequestRideRequestDTO represents the HTTP request payload for requesting a ride.
type RequestRideRequestDTO struct {
	RiderID     string             `json:"riderId"`
	Pickup      domain.Location    `json:"pickup"`
	Dropoff     domain.Location    `json:"dropoff"`
	ServiceType domain.ServiceType `json:"serviceType"`
	Region      string             `json:"region"`
}

// HandleRequestRide handles POST /api/ride/request
func (h *Handlers) HandleRequestRide(w http.ResponseWriter, r *http.Request) {
	claims, ok := GetUserClaims(r.Context())
	if !ok || claims.Subject == "" {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	idemKey := r.Header.Get("Idempotency-Key")
	if idemKey == "" {
		http.Error(w, `{"error":"missing Idempotency-Key header"}`, http.StatusBadRequest)
		return
	}

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields() // Strict JSON validation per AGENTS.md Section 10

	var dto RequestRideRequestDTO
	if err := decoder.Decode(&dto); err != nil {
		http.Error(w, `{"error":"malformed json request body"}`, http.StatusBadRequest)
		return
	}

	// Principal rule: identity comes strictly from verified token sub
	if dto.RiderID != "" && dto.RiderID != claims.Subject {
		http.Error(w, `{"error":"forbidden: riderId mismatch"}`, http.StatusForbidden)
		return
	}
	dto.RiderID = claims.Subject

	if dto.Region == "" {
		dto.Region = "downtown"
	}

	req := dispatch.RequestRideRequest{
		RiderID:        dto.RiderID,
		Pickup:         dto.Pickup,
		Dropoff:        dto.Dropoff,
		ServiceType:    dto.ServiceType,
		Region:         dto.Region,
		IdempotencyKey: idemKey,
	}

	ride, err := h.dispatchSvc.RequestRide(r.Context(), req)
	if err != nil {
		if errors.Is(err, domain.ErrNoDriverFound) {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(ride)
			return
		}
		if errors.Is(err, domain.ErrInvalidCoordinates) || errors.Is(err, domain.ErrInvalidServiceType) {
			http.Error(w, `{"error":"invalid input parameters"}`, http.StatusBadRequest)
			return
		}
		http.Error(w, `{"error":"failed to dispatch ride"}`, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(ride)
}

// HandleGetRideStatus handles GET /api/ride/status?rideId=xxx
func (h *Handlers) HandleGetRideStatus(w http.ResponseWriter, r *http.Request) {
	claims, ok := GetUserClaims(r.Context())
	if !ok {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	rideID := r.URL.Query().Get("rideId")
	if rideID == "" {
		http.Error(w, `{"error":"missing rideId parameter"}`, http.StatusBadRequest)
		return
	}

	ride, err := h.dispatchSvc.GetRide(r.Context(), rideID)
	if err != nil {
		if errors.Is(err, domain.ErrRideNotFound) {
			http.Error(w, `{"error":"ride not found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	// Object-level authorization check per AGENTS.md Section 8
	if err := VerifyObjectOwnership(claims, ride); err != nil {
		http.Error(w, `{"error":"forbidden: cannot access this ride"}`, http.StatusForbidden)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(ride)
}

type DriverResponseDTO struct {
	DriverID string `json:"driverId"`
	RideID   string `json:"rideId"`
	Accept   bool   `json:"accept"`
}

// HandleDriverResponse handles POST /api/ride/response
func (h *Handlers) HandleDriverResponse(w http.ResponseWriter, r *http.Request) {
	claims, ok := GetUserClaims(r.Context())
	if !ok || claims.Subject == "" {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	var dto DriverResponseDTO
	if err := decoder.Decode(&dto); err != nil {
		http.Error(w, `{"error":"malformed request body"}`, http.StatusBadRequest)
		return
	}

	// Identity comes from verified token sub
	if dto.DriverID != "" && dto.DriverID != claims.Subject {
		http.Error(w, `{"error":"forbidden: driverId mismatch"}`, http.StatusForbidden)
		return
	}
	dto.DriverID = claims.Subject

	err := h.dispatchSvc.RespondToOffer(r.Context(), dto.DriverID, dto.RideID, dto.Accept)
	if err != nil {
		if errors.Is(err, domain.ErrForbidden) {
			http.Error(w, `{"error":"forbidden: ride is not offered to you"}`, http.StatusForbidden)
			return
		}
		if errors.Is(err, domain.ErrInvalidTransition) || errors.Is(err, domain.ErrOptimisticLockConflict) {
			http.Error(w, `{"error":"ride is no longer pending acceptance"}`, http.StatusConflict)
			return
		}
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"success"}`))
}

type CancelRideDTO struct {
	RideID string `json:"rideId"`
	Reason string `json:"reason,omitempty"`
}

// HandleCancelRide handles POST /api/ride/cancel
func (h *Handlers) HandleCancelRide(w http.ResponseWriter, r *http.Request) {
	claims, ok := GetUserClaims(r.Context())
	if !ok {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	var dto CancelRideDTO
	if err := decoder.Decode(&dto); err != nil {
		http.Error(w, `{"error":"malformed request body"}`, http.StatusBadRequest)
		return
	}

	ride, err := h.dispatchSvc.GetRide(r.Context(), dto.RideID)
	if err != nil {
		if errors.Is(err, domain.ErrRideNotFound) {
			http.Error(w, `{"error":"ride not found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	// Confirm caller is authorized to cancel
	if err := VerifyObjectOwnership(claims, ride); err != nil {
		http.Error(w, `{"error":"forbidden: cannot cancel this ride"}`, http.StatusForbidden)
		return
	}

	actor := claims.Role + ":" + claims.Subject
	if err := h.dispatchSvc.CancelRide(r.Context(), dto.RideID, actor); err != nil {
		if errors.Is(err, domain.ErrInvalidTransition) {
			http.Error(w, `{"error":"ride cannot be cancelled in its current state"}`, http.StatusConflict)
			return
		}
		http.Error(w, `{"error":"failed to cancel ride"}`, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"cancelled"}`))
}

type DriverUpdateLocationDTO struct {
	DriverID string             `json:"driverId"`
	Location domain.Location    `json:"location"`
	Region   string             `json:"region"`
	State    domain.DriverState `json:"state"`
}

// HandleDriverUpdateLocation handles POST /api/driver/update-location
func (h *Handlers) HandleDriverUpdateLocation(w http.ResponseWriter, r *http.Request) {
	claims, ok := GetUserClaims(r.Context())
	if !ok || claims.Subject == "" {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	var dto DriverUpdateLocationDTO
	if err := decoder.Decode(&dto); err != nil {
		http.Error(w, `{"error":"malformed request body"}`, http.StatusBadRequest)
		return
	}

	if dto.DriverID != "" && dto.DriverID != claims.Subject {
		http.Error(w, `{"error":"forbidden: driverId mismatch"}`, http.StatusForbidden)
		return
	}
	dto.DriverID = claims.Subject

	if dto.Region == "" {
		dto.Region = "downtown"
	}
	if dto.State == "" {
		dto.State = domain.DriverStateAvailable
	}

	if err := h.locationSvc.UpdateLocation(r.Context(), dto.DriverID, dto.Location, dto.Region, dto.State); err != nil {
		if errors.Is(err, domain.ErrInvalidCoordinates) || errors.Is(err, domain.ErrDistanceOutOfBounds) {
			http.Error(w, `{"error":"invalid coordinates"}`, http.StatusBadRequest)
			return
		}
		if errors.Is(err, domain.ErrSpeedAnomaly) {
			http.Error(w, `{"error":"impossible speed detected (gps spoofing)"}`, http.StatusUnprocessableEntity)
			return
		}
		if errors.Is(err, domain.ErrRateLimitExceeded) {
			http.Error(w, `{"error":"location update rate limit exceeded"}`, http.StatusTooManyRequests)
			return
		}
		http.Error(w, `{"error":"failed to update location"}`, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}
