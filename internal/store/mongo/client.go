package mongo

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

type Config struct {
	URI      string
	Database string
}

type Store struct {
	client  *mongo.Client
	db      *mongo.Database
	rides   *mongo.Collection
	drivers *mongo.Collection
	outbox  *mongo.Collection
}

// NewStore initializes a MongoDB store with connection pooling and health check.
func NewStore(ctx context.Context, cfg Config) (*Store, error) {
	opts := options.Client().
		ApplyURI(cfg.URI).
		SetMaxPoolSize(100).
		SetMinPoolSize(10).
		SetMaxConnIdleTime(60 * time.Second)

	client, err := mongo.Connect(ctx, opts)
	if err != nil {
		return nil, err
	}

	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	if err := client.Ping(pingCtx, readpref.Primary()); err != nil {
		_ = client.Disconnect(ctx)
		return nil, err
	}

	db := client.Database(cfg.Database)

	store := &Store{
		client:  client,
		db:      db,
		rides:   db.Collection("rides"),
		drivers: db.Collection("drivers"),
		outbox:  db.Collection("outbox"),
	}

	return store, nil
}

// Close gracefully closes the Mongo client connection.
func (s *Store) Close(ctx context.Context) error {
	return s.client.Disconnect(ctx)
}

// Client returns the underlying mongo client.
func (s *Store) Client() *mongo.Client {
	return s.client
}
