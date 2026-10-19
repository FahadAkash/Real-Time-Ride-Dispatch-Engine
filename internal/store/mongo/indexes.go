package mongo

import (
	"context"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// EnsureIndexes creates all required indexes for rides, drivers, and outbox collections per AGENTS.md Section 9.
func (s *Store) EnsureIndexes(ctx context.Context) error {
	// 1. Rides indexes
	rideIndexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "rideId", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{
				{Key: "riderId", Value: 1},
				{Key: "idempotencyKey", Value: 1},
			},
			Options: options.Index().SetUnique(true).SetSparse(true),
		},
		{
			Keys: bson.D{
				{Key: "riderId", Value: 1},
				{Key: "createdAt", Value: -1},
			},
		},
		{
			Keys: bson.D{
				{Key: "status", Value: 1},
				{Key: "updatedAt", Value: 1},
			},
		},
	}
	if _, err := s.rides.Indexes().CreateMany(ctx, rideIndexes); err != nil {
		return err
	}

	// 2. Drivers indexes
	driverIndexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "driverId", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
	}
	if _, err := s.drivers.Indexes().CreateMany(ctx, driverIndexes); err != nil {
		return err
	}

	// 3. Outbox indexes
	outboxIndexes := []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "published", Value: 1},
				{Key: "createdAt", Value: 1},
			},
		},
	}
	if _, err := s.outbox.Indexes().CreateMany(ctx, outboxIndexes); err != nil {
		return err
	}

	return nil
}
