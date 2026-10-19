package notify

import (
	"context"
	"time"

	"github.com/ride-dispatch/engine/internal/domain"
	"github.com/ride-dispatch/engine/internal/platform/resilience"
)

// Notifier delivers push ride offers to drivers with backoff and circuit breaking.
type Notifier interface {
	NotifyDriverOffer(ctx context.Context, driverID string, ride *domain.Ride, timeout time.Duration) error
}

// SimulatedNotifier provides an in-memory notifier for local development, tests, and load simulation.
type SimulatedNotifier struct {
	breaker *resilience.Breaker
}

func NewSimulatedNotifier() *SimulatedNotifier {
	return &SimulatedNotifier{
		breaker: resilience.NewBreaker(resilience.CircuitBreakerConfig{
			Name: "simulated-push-notifier",
		}),
	}
}

func (s *SimulatedNotifier) NotifyDriverOffer(ctx context.Context, driverID string, ride *domain.Ride, timeout time.Duration) error {
	_, err := s.breaker.Execute(func() (any, error) {
		// Simulate network dispatch with backoff if requested
		return nil, nil
	})
	return err
}
