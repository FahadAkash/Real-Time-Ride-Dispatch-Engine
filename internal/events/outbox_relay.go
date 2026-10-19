package events

import (
	"context"
	"log/slog"
	"time"

	"github.com/ride-dispatch/engine/internal/domain"
	"github.com/ride-dispatch/engine/internal/platform/metrics"
)

type OutboxStore interface {
	GetPendingOutboxRecords(ctx context.Context, batchSize int) ([]*domain.OutboxRecord, error)
	MarkOutboxRecordPublished(ctx context.Context, id string) error
	IncrementOutboxRetry(ctx context.Context, id string, lastErr string) error
	CountPendingOutbox(ctx context.Context) (int64, error)
}

type EventPublisher interface {
	Publish(ctx context.Context, topic, key string, payload []byte) error
}

type OutboxRelayConfig struct {
	PollInterval time.Duration
	BatchSize    int
}

type OutboxRelay struct {
	cfg       OutboxRelayConfig
	store     OutboxStore
	publisher EventPublisher
	logger    *slog.Logger
}

func NewOutboxRelay(cfg OutboxRelayConfig, store OutboxStore, publisher EventPublisher, logger *slog.Logger) *OutboxRelay {
	if cfg.PollInterval == 0 {
		cfg.PollInterval = 200 * time.Millisecond
	}
	if cfg.BatchSize == 0 {
		cfg.BatchSize = 100
	}
	if logger == nil {
		logger = slog.Default()
	}

	return &OutboxRelay{
		cfg:       cfg,
		store:     store,
		publisher: publisher,
		logger:    logger,
	}
}

// Start runs the outbox relay loop until the context is cancelled.
func (r *OutboxRelay) Start(ctx context.Context) {
	ticker := time.NewTicker(r.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.ProcessBatch(ctx)
		}
	}
}

// ProcessBatch reads a batch of pending events and publishes them to Kafka.
func (r *OutboxRelay) ProcessBatch(ctx context.Context) {
	records, err := r.store.GetPendingOutboxRecords(ctx, r.cfg.BatchSize)
	if err != nil {
		r.logger.Error("Failed to fetch pending outbox records", slog.String("error", err.Error()))
		return
	}

	if len(records) == 0 {
		return
	}

	for _, rec := range records {
		pubErr := r.publisher.Publish(ctx, rec.Topic, rec.Key, rec.Payload)
		if pubErr != nil {
			r.logger.Warn("Failed to publish outbox event to Kafka", slog.String("eventId", rec.EventID), slog.String("topic", rec.Topic), slog.String("error", pubErr.Error()))
			_ = r.store.IncrementOutboxRetry(ctx, rec.ID, pubErr.Error())
			continue
		}

		if err := r.store.MarkOutboxRecordPublished(ctx, rec.ID); err != nil {
			r.logger.Error("Failed to mark outbox record as published", slog.String("id", rec.ID), slog.String("error", err.Error()))
		}
	}

	// Update outbox pending gauge metric
	if pendingCount, err := r.store.CountPendingOutbox(ctx); err == nil {
		metrics.OutboxPending.Set(float64(pendingCount))
	}
}
