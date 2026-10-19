package location

import (
	"context"
	"log/slog"
	"time"
)

// StalenessCleaner periodically prunes drivers who have stopped sending heartbeats.
type StalenessCleaner struct {
	geo           GeoStore
	staleDuration time.Duration
	interval      time.Duration
	logger        *slog.Logger
}

func NewStalenessCleaner(geo GeoStore, staleDuration, interval time.Duration, logger *slog.Logger) *StalenessCleaner {
	if staleDuration == 0 {
		staleDuration = 30 * time.Second
	}
	if interval == 0 {
		interval = 10 * time.Second
	}
	if logger == nil {
		logger = slog.Default()
	}

	return &StalenessCleaner{
		geo:           geo,
		staleDuration: staleDuration,
		interval:      interval,
		logger:        logger,
	}
}

// Start begins the background staleness cleaning loop.
func (c *StalenessCleaner) Start(ctx context.Context, region string, knownDrivers []string) {
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.CleanStaleDrivers(ctx, region, knownDrivers)
		}
	}
}

// CleanStaleDrivers checks the status of known drivers and removes any with expired status from the GEO set.
func (c *StalenessCleaner) CleanStaleDrivers(ctx context.Context, region string, driverIDs []string) {
	now := time.Now().UTC()
	for _, id := range driverIDs {
		state, _, lastSeen, err := c.geo.GetDriverStatus(ctx, id)
		if err != nil || state != "AVAILABLE" || now.Sub(lastSeen) > c.staleDuration {
			_ = c.geo.RemoveDriverLocation(ctx, region, id)
		}
	}
}

