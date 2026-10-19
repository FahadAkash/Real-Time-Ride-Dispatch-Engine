package gateway

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

type RouterConfig struct {
	JWTValidator    *JWTValidator
	RateLimiter     RateLimiter
	RateLimitPerMin int
	Logger          *slog.Logger
}

// NewRouter sets up the Chi HTTP router with security middlewares and route groups per AGENTS.md.
func NewRouter(cfg RouterConfig, handlers *Handlers) http.Handler {
	r := chi.NewRouter()

	// Base middleware stack
	r.Use(chimiddleware.RealIP)
	r.Use(RequestIDMiddleware)
	r.Use(BodyLimitMiddleware) // 16 KB max body
	r.Use(RecoveryMiddleware(cfg.Logger))

	// Health probes (internal or edge)
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"healthy"}`))
	})
	r.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ready"}`))
	})

	// Protected API Routes
	r.Route("/api", func(api chi.Router) {
		if cfg.JWTValidator != nil {
			api.Use(AuthMiddleware(cfg.JWTValidator))
		}

		// Rate limiting middleware
		rateLimit := cfg.RateLimitPerMin
		if rateLimit == 0 {
			rateLimit = 60
		}
		api.Use(RateLimitMiddleware(cfg.RateLimiter, rateLimit, 1*time.Minute))

		// Rider routes
		api.Group(func(rider chi.Router) {
			rider.Use(RequireRole("rider"))
			rider.Post("/ride/request", handlers.HandleRequestRide)
		})

		// Driver routes
		api.Group(func(driver chi.Router) {
			driver.Use(RequireRole("driver"))
			driver.Post("/ride/response", handlers.HandleDriverResponse)
			driver.Post("/driver/update-location", handlers.HandleDriverUpdateLocation)
		})

		// Shared routes (rider, driver, admin)
		api.Group(func(shared chi.Router) {
			shared.Use(RequireRole("rider", "driver", "admin"))
			shared.Get("/ride/status", handlers.HandleGetRideStatus)
			shared.Post("/ride/cancel", handlers.HandleCancelRide)
		})
	})

	return r
}
