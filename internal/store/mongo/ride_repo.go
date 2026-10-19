package mongo

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/ride-dispatch/engine/internal/domain"
)

// CreateRideWithOutbox persists the ride and writes the initial outbox event atomically.
func (s *Store) CreateRideWithOutbox(ctx context.Context, ride *domain.Ride, event *domain.EventEnvelope) error {
	outboxRec := &domain.OutboxRecord{
		ID:        uuid.New().String(),
		EventID:   event.EventID,
		Topic:     event.Type,
		Key:       ride.RideID,
		Payload:   event.Payload,
		CreatedAt: event.OccurredAt,
		Published: false,
	}

	session, err := s.client.StartSession()
	if err == nil {
		defer session.EndSession(ctx)
		_, txErr := session.WithTransaction(ctx, func(sessCtx mongo.SessionContext) (any, error) {
			if _, insertErr := s.rides.InsertOne(sessCtx, ride); insertErr != nil {
				return nil, insertErr
			}
			if _, outErr := s.outbox.InsertOne(sessCtx, outboxRec); outErr != nil {
				return nil, outErr
			}
			return nil, nil
		})
		if txErr == nil {
			return nil
		}
	}

	// Standalone fallback
	if _, err := s.rides.InsertOne(ctx, ride); err != nil {
		return err
	}
	if _, err := s.outbox.InsertOne(ctx, outboxRec); err != nil {
		return err
	}
	return nil
}

// GetRideByID retrieves a ride by its ID.
func (s *Store) GetRideByID(ctx context.Context, rideID string) (*domain.Ride, error) {
	filter := bson.M{"rideId": rideID}
	var ride domain.Ride
	err := s.rides.FindOne(ctx, filter).Decode(&ride)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, domain.ErrRideNotFound
		}
		return nil, err
	}
	return &ride, nil
}

// UpdateRideStatus conditionally updates status and version with optimistic concurrency and appends history.
func (s *Store) UpdateRideStatus(ctx context.Context, rideID string, expectedStatus domain.RideStatus, expectedVersion int64, newStatus domain.RideStatus, actor, reason string, at time.Time, event *domain.EventEnvelope) error {
	historyEntry := domain.RideHistoryEntry{
		Status:    newStatus,
		Timestamp: at.UTC(),
		Actor:     actor,
		Reason:    reason,
	}

	filter := bson.M{
		"rideId":  rideID,
		"status":  expectedStatus,
		"version": expectedVersion,
	}

	update := bson.M{
		"$set": bson.M{
			"status":    newStatus,
			"updatedAt": at.UTC(),
		},
		"$inc": bson.M{
			"version": 1,
		},
		"$push": bson.M{
			"history": historyEntry,
		},
	}

	outboxRec := &domain.OutboxRecord{
		ID:        uuid.New().String(),
		EventID:   event.EventID,
		Topic:     event.Type,
		Key:       rideID,
		Payload:   event.Payload,
		CreatedAt: at.UTC(),
		Published: false,
	}

	session, err := s.client.StartSession()
	if err == nil {
		defer session.EndSession(ctx)
		_, txErr := session.WithTransaction(ctx, func(sessCtx mongo.SessionContext) (any, error) {
			res, updateErr := s.rides.UpdateOne(sessCtx, filter, update)
			if updateErr != nil {
				return nil, updateErr
			}
			if res.MatchedCount == 0 {
				return nil, domain.ErrOptimisticLockConflict
			}
			if _, outErr := s.outbox.InsertOne(sessCtx, outboxRec); outErr != nil {
				return nil, outErr
			}
			return nil, nil
		})
		if txErr == nil {
			return nil
		}
		if errors.Is(txErr, domain.ErrOptimisticLockConflict) {
			return domain.ErrOptimisticLockConflict
		}
	}

	// Standalone fallback
	res, updateErr := s.rides.UpdateOne(ctx, filter, update)
	if updateErr != nil {
		return updateErr
	}
	if res.MatchedCount == 0 {
		return domain.ErrOptimisticLockConflict
	}
	_, _ = s.outbox.InsertOne(ctx, outboxRec)
	return nil
}

// GetActiveRidesForTimeout retrieves rides stuck in PENDING_ACCEPT older than the cutoff threshold.
func (s *Store) GetActiveRidesForTimeout(ctx context.Context, olderThan time.Time, limit int) ([]*domain.Ride, error) {
	filter := bson.M{
		"status": domain.StatusPendingAccept,
		"updatedAt": bson.M{
			"$lt": olderThan.UTC(),
		},
	}

	opts := options.Find().SetLimit(int64(limit)).SetSort(bson.D{{Key: "updatedAt", Value: 1}})
	cursor, err := s.rides.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var rides []*domain.Ride
	if err := cursor.All(ctx, &rides); err != nil {
		return nil, err
	}
	return rides, nil
}

// UpdateRideForReassign updates a ride to REASSIGNING, appends the excluded driver, and logs an outbox event.
func (s *Store) UpdateRideForReassign(ctx context.Context, rideID string, expectedVersion int64, excludedDriver string, event *domain.EventEnvelope) error {
	filter := bson.M{
		"rideId":  rideID,
		"status":  domain.StatusPendingAccept,
		"version": expectedVersion,
	}

	now := time.Now().UTC()
	update := bson.M{
		"$set": bson.M{
			"status":    domain.StatusReassigning,
			"driverId":  "",
			"updatedAt": now,
		},
		"$inc": bson.M{
			"version":       1,
			"reassignCount": 1,
		},
		"$addToSet": bson.M{
			"excludedDrivers": excludedDriver,
		},
		"$push": bson.M{
			"history": domain.RideHistoryEntry{
				Status:    domain.StatusReassigning,
				Timestamp: now,
				Actor:     "system/timeout-worker",
				Reason:    "offer expired or rejected",
			},
		},
	}

	res, err := s.rides.UpdateOne(ctx, filter, update)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return domain.ErrOptimisticLockConflict
	}

	if event != nil {
		outboxRec := &domain.OutboxRecord{
			ID:        uuid.New().String(),
			EventID:   event.EventID,
			Topic:     event.Type,
			Key:       rideID,
			Payload:   event.Payload,
			CreatedAt: now,
			Published: false,
		}
		_, _ = s.outbox.InsertOne(ctx, outboxRec)
	}

	return nil
}
