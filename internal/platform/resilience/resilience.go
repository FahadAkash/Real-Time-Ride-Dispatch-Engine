package resilience

import (
	"context"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/sony/gobreaker"
)

// CircuitBreakerConfig defines threshold settings for the breaker.
type CircuitBreakerConfig struct {
	Name        string
	MaxRequests uint32
	Interval    time.Duration
	Timeout     time.Duration
}

// Breaker wraps gobreaker.CircuitBreaker for safe execution.
type Breaker struct {
	cb *gobreaker.CircuitBreaker
}

// NewBreaker initializes a circuit breaker with sensible defaults.
func NewBreaker(cfg CircuitBreakerConfig) *Breaker {
	if cfg.MaxRequests == 0 {
		cfg.MaxRequests = 5
	}
	if cfg.Interval == 0 {
		cfg.Interval = 10 * time.Second
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}

	st := gobreaker.Settings{
		Name:        cfg.Name,
		MaxRequests: cfg.MaxRequests,
		Interval:    cfg.Interval,
		Timeout:     cfg.Timeout,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			failureRatio := float64(counts.TotalFailures) / float64(counts.Requests)
			return counts.Requests >= 10 && failureRatio >= 0.5
		},
	}

	return &Breaker{
		cb: gobreaker.NewCircuitBreaker(st),
	}
}

// Execute runs the given operation through the circuit breaker.
func (b *Breaker) Execute(op func() (any, error)) (any, error) {
	return b.cb.Execute(op)
}

// RetryWithBackoff executes an idempotent operation with exponential backoff and jitter.
func RetryWithBackoff(ctx context.Context, maxElapsedTime time.Duration, op func() error) error {
	expBackoff := backoff.NewExponentialBackOff()
	expBackoff.InitialInterval = 50 * time.Millisecond
	expBackoff.MaxInterval = 1 * time.Second
	expBackoff.MaxElapsedTime = maxElapsedTime
	expBackoff.RandomizationFactor = 0.5 // full jitter

	backoffWithContext := backoff.WithContext(expBackoff, ctx)
	return backoff.Retry(op, backoffWithContext)
}
