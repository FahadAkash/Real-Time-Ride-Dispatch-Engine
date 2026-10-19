package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// CheckOrSetIdempotency checks if an idempotency key already exists for the rider.
// If it exists, it returns the existing rideID and isNew = false.
// If it does not exist, it stores rideID with the given TTL and returns isNew = true.
func (c *Client) CheckOrSetIdempotency(ctx context.Context, riderID, idemKey, rideID string, ttl time.Duration) (string, bool, error) {
	key := fmt.Sprintf("idem:%s:%s", riderID, idemKey)

	// Attempt to set key only if it does not already exist
	ok, err := c.rdb.SetNX(ctx, key, rideID, ttl).Result()
	if err != nil {
		return "", false, err
	}
	if ok {
		return rideID, true, nil
	}

	// Key already exists, fetch the previously associated rideID
	existingRideID, err := c.rdb.Get(ctx, key).Result()
	if err != nil && err != redis.Nil {
		return "", false, err
	}
	return existingRideID, false, nil
}
