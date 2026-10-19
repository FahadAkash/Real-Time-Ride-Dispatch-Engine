package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// compareAndDeleteScript releases the lock only if the value matches the expected owner.
var compareAndDeleteScript = redis.NewScript(`
if redis.call("get", KEYS[1]) == ARGV[1] then
    return redis.call("del", KEYS[1])
else
    return 0
end
`)

// AcquireDriverLock attempts to atomically reserve a driver for a specific ride with a TTL.
// Returns true if the lock was acquired, false if the driver is already locked.
func (c *Client) AcquireDriverLock(ctx context.Context, driverID, rideID string, ttl time.Duration) (bool, error) {
	key := fmt.Sprintf("driver:%s:lock", driverID)
	ok, err := c.rdb.SetNX(ctx, key, rideID, ttl).Result()
	if err != nil {
		return false, err
	}
	return ok, nil
}

// ReleaseDriverLock atomically releases a driver lock using compare-and-delete.
// It succeeds ONLY if the current lock value equals the given rideID.
func (c *Client) ReleaseDriverLock(ctx context.Context, driverID, rideID string) (bool, error) {
	key := fmt.Sprintf("driver:%s:lock", driverID)
	res, err := compareAndDeleteScript.Run(ctx, c.rdb, []string{key}, rideID).Result()
	if err != nil {
		return false, err
	}
	// Lua returns 1 if deleted, 0 if not matched
	val, ok := res.(int64)
	return ok && val == 1, nil
}

// AcquireRideMutex attempts to lock a ride for state transition.
func (c *Client) AcquireRideMutex(ctx context.Context, rideID, token string, ttl time.Duration) (bool, error) {
	key := fmt.Sprintf("ride:%s:lock", rideID)
	return c.rdb.SetNX(ctx, key, token, ttl).Result()
}

// ReleaseRideMutex releases the short state transition mutex.
func (c *Client) ReleaseRideMutex(ctx context.Context, rideID, token string) (bool, error) {
	key := fmt.Sprintf("ride:%s:lock", rideID)
	res, err := compareAndDeleteScript.Run(ctx, c.rdb, []string{key}, token).Result()
	if err != nil {
		return false, err
	}
	val, ok := res.(int64)
	return ok && val == 1, nil
}

