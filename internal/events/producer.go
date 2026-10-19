package events

import (
	"context"
	"crypto/tls"

	"github.com/twmb/franz-go/pkg/kgo"
)

type Config struct {
	Brokers      []string
	SASLUsername string
	SASLPassword string
	UseTLS       bool
}

type Producer struct {
	client *kgo.Client
}

// NewProducer initializes a high-performance Franz-Go Kafka producer.
func NewProducer(cfg Config) (*Producer, error) {
	opts := []kgo.Opt{
		kgo.SeedBrokers(cfg.Brokers...),
		kgo.ProducerBatchMaxBytes(1024 * 1024), // 1 MB batch
	}

	if cfg.UseTLS {
		opts = append(opts, kgo.DialTLSConfig(&tls.Config{
			MinVersion: tls.VersionTLS12,
		}))
	}

	client, err := kgo.NewClient(opts...)
	if err != nil {
		return nil, err
	}

	return &Producer{client: client}, nil
}

// Publish delivers an event to Kafka with the given topic and key (rideID for partition ordering).
func (p *Producer) Publish(ctx context.Context, topic, key string, payload []byte) error {
	record := &kgo.Record{
		Topic: topic,
		Key:   []byte(key),
		Value: payload,
	}

	results := p.client.ProduceSync(ctx, record)
	return results.FirstErr()
}

// Close gracefully flushes pending records and closes the producer.
func (p *Producer) Close() {
	if p.client != nil {
		p.client.Close()
	}
}

