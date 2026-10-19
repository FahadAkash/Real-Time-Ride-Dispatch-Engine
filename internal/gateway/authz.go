package gateway

import (
	"context"
	"net/http"
	"strings"

	"github.com/ride-dispatch/engine/internal/domain"
)

type contextKey string

const (
	ClaimsContextKey contextKey = "userClaims"
)

// SetUserClaims injects verified claims into the request context.
func SetUserClaims(ctx context.Context, claims *UserClaims) context.Context {
	return context.WithValue(ctx, ClaimsContextKey, claims)
}

// GetUserClaims extracts verified claims from the request context.
func GetUserClaims(ctx context.Context) (*UserClaims, bool) {
	claims, ok := ctx.Value(ClaimsContextKey).(*UserClaims)
	return claims, ok
}

// AuthMiddleware extracts Bearer tokens and validates them via JWTValidator.
func AuthMiddleware(validator *JWTValidator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				http.Error(w, `{"error":"missing authorization header"}`, http.StatusUnauthorized)
				return
			}

			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
				http.Error(w, `{"error":"invalid authorization format, expected Bearer <token>"}`, http.StatusUnauthorized)
				return
			}

			claims, err := validator.ValidateToken(parts[1])
			if err != nil {
				http.Error(w, `{"error":"invalid or expired token"}`, http.StatusUnauthorized)
				return
			}

			ctx := SetUserClaims(r.Context(), claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireRole ensures the authenticated principal holds at least one of the specified roles.
func RequireRole(allowedRoles ...string) func(http.Handler) http.Handler {
	allowed := make(map[string]bool, len(allowedRoles))
	for _, r := range allowedRoles {
		allowed[strings.ToLower(r)] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := GetUserClaims(r.Context())
			if !ok || !allowed[strings.ToLower(claims.Role)] {
				http.Error(w, `{"error":"forbidden: insufficient role"}`, http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// VerifyObjectOwnership confirms that caller is authorized to view or mutate the ride.
func VerifyObjectOwnership(claims *UserClaims, ride *domain.Ride) error {
	if claims == nil || ride == nil {
		return domain.ErrForbidden
	}

	// Admin can access any ride
	if strings.EqualFold(claims.Role, "admin") {
		return nil
	}

	// Rider must own the ride
	if strings.EqualFold(claims.Role, "rider") {
		if ride.RiderID != claims.Subject {
			return domain.ErrForbidden
		}
		return nil
	}

	// Driver must be assigned to the ride
	if strings.EqualFold(claims.Role, "driver") {
		if ride.DriverID != claims.Subject {
			return domain.ErrForbidden
		}
		return nil
	}

	return domain.ErrForbidden
}
