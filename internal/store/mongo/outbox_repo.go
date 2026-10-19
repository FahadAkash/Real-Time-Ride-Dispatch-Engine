package mongo

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/ride-dispatch/engine/internal/domain"
)

// GetPendingOutboxRecords fetches unpublished outbox records sorted by creation timestamp.
func (s *Store) GetPendingOutboxRecords(ctx context.Context, batchSize int) ([]*domain.OutboxRecord, error) {
	filter := bson.M{
		"published": false,
		"retryCount": bson.M{
			"$lt": 10,
		},
	}
	opts := options.Find().SetLimit(int64(batchSize)).SetSort(bson.D{{Key: "createdAt", Value: 1}})

	cursor, err := s.outbox.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var records []*domain.OutboxRecord
	if err := cursor.All(ctx, &records); err != nil {
		return nil, err
	}
	return records, nil
}

// MarkOutboxRecordPublished marks an outbox event as successfully delivered to Kafka.
func (s *Store) MarkOutboxRecordPublished(ctx context.Context, id string) error {
	filter := bson.M{"_id": id}
	now := time.Now().UTC()
	update := bson.M{
		"$set": bson.M{
			"published":   true,
			"publishedAt": &now,
		},
	}
	_, err := s.outbox.UpdateOne(ctx, filter, update)
	return err
}

// IncrementOutboxRetry records a publishing error and increments retry count.
func (s *Store) IncrementOutboxRetry(ctx context.Context, id string, lastErr string) error {
	filter := bson.M{"_id": id}
	update := bson.M{
		"$inc": bson.M{
			"retryCount": 1,
		},
		"$set": bson.M{
			"lastError": lastErr,
		},
	}
	_, err := s.outbox.UpdateOne(ctx, filter, update)
	return err
}

// CountPendingOutbox returns the count of unpublished outbox records for metrics monitoring.
func (s *Store) CountPendingOutbox(ctx context.Context) (int64, error) {
	filter := bson.M{"published": false}
	return s.outbox.CountDocuments(ctx, filter)
}
