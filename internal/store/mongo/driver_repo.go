package mongo

import (
	"context"
	"errors"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/ride-dispatch/engine/internal/domain"
)

// SaveDriver creates or updates a driver record in MongoDB.
func (s *Store) SaveDriver(ctx context.Context, driver *domain.Driver) error {
	filter := bson.M{"driverId": driver.DriverID}
	opts := options.Update().SetUpsert(true)
	update := bson.M{"$set": driver}

	_, err := s.drivers.UpdateOne(ctx, filter, update, opts)
	return err
}

// GetDriver retrieves a driver record by their ID.
func (s *Store) GetDriver(ctx context.Context, driverID string) (*domain.Driver, error) {
	filter := bson.M{"driverId": driverID}
	var driver domain.Driver
	err := s.drivers.FindOne(ctx, filter).Decode(&driver)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, domain.ErrDriverNotFound
		}
		return nil, err
	}
	return &driver, nil
}
