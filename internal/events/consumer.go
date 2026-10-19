package events

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/ride-dispatch/engine/internal/domain"
)

type EventHandler func(ctx context.Context, envelope *domain.EventEnvelope) error

type ConsumerGroup struct {
	client     *kgo.Client
	handler    EventHandler
	seenMu     sync.RWMutex
	seenEvents map[string]time.Time // eventId -> firstSeenAt
}

// NewConsumerGroup initializes an idempotent Kafka consumer group.
func NewConsumerGroup(brokers []string, group string, topics []string, handler EventHandler) (*ConsumerGroup, error) {
	opts := []kgo.Opt{
		kgo.SeedBrokers(brokers...),
		kgo.ConsumerGroup(group),
		kgo.ConsumeTopics(topics...),
	}

	client, err := kgo.NewClient(opts...)
	if err != nil {
		return nil, err
	}

	return &ConsumerGroup{
		client:     client,
		handler:    handler,
		seenEvents: make(map[string]time.Time),
	}, nil
}

// PollAndProcess pulls and executes records while deduplicating events by eventId.
func (c *ConsumerGroup) PollAndProcess(ctx context.Context) error {
	fetches := c.client.PollFetches(ctx)
	if fetches.IsClientClosed() {
		return context.Canceled
	}

	iter := fetches.RecordIter()
	for !iter.Done() {
		record := iter.Next()

		var envelope domain.EventEnvelope
		if err := json.Unmarshal(record.Value, &envelope); err != nil {
			continue // Skip malformed events
		}

		// Idempotency deduplication check
		if c.isDuplicate(envelope.EventID) {
			continue
		}

		if err := c.handler(ctx, &envelope); err != nil {
			return err
		}

		c.markSeen(envelope.EventID)
	}

	return nil
}

func (c *ConsumerGroup) isDuplicate(eventID string) bool {
	c.seenMu.RLock()
	defer c.seenMu.RUnlock()
	_, exists := c.seenEvents[eventID]
	return exists
}

func (c *ConsumerGroup) markSeen(eventID string) {
	c.seenMu.Lock()
	defer c.seenMu.Unlock()
	c.seenEvents[eventID] = time.Now()
}

// Close closes the Kafka consumer.
func (c *ConsumerGroup) Close() {
	if c.client != nil {
		c.client.Close()
	}
}
