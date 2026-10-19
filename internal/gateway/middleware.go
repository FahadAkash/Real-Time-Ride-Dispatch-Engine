package gateway

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/ride-dispatch/engine/internal/platform/logger"
	"github.com/ride-dispatch/engine/internal/platform/server"
)

type RateLimiter interface {
	AllowRateLimit(ctx context.Context, principal, route string, limit int, window time.Duration) (bool, error)
}

// RequestIDMiddleware injects X-Request-ID into request context and response headers.
func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := r.Header.Get("X-Request-ID")
		if reqID == "" {
			reqID = uuid.New().String()
		}
		w.Header().Set("X-Request-ID", reqID)
		ctx := logger.ContextWithRequestID(r.Context(), reqID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// BodyLimitMiddleware caps incoming HTTP request bodies at 16 KB per AGENTS.md Section 10.
func BodyLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, server.MaxBodyBytes)
		next.ServeHTTP(w, r)
	})
}

// RateLimitMiddleware enforces rate limiting per principal or IP.
func RateLimitMiddleware(limiter RateLimiter, limit int, window time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if limiter == nil {
				next.ServeHTTP(w, r)
				return
			}

			principal := r.RemoteAddr
			if claims, ok := GetUserClaims(r.Context()); ok && claims.Subject != "" {
				principal = claims.Subject
			}

			allowed, err := limiter.AllowRateLimit(r.Context(), principal, r.URL.Path, limit, window)
			if err != nil || !allowed {
				http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RecoveryMiddleware catches panics and returns generic 500 responses without stack traces.
func RecoveryMiddleware(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					log.Error("Unhandled panic in HTTP handler", slog.Any("panic", rec))
					http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

