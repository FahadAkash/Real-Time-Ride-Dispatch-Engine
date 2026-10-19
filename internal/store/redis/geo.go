package redis

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/ride-dispatch/engine/internal/domain"
)

// AddDriverLocation updates the driver's geographic coordinates in the regional GEO set.
func (c *Client) AddDriverLocation(ctx context.Context, region, driverID string, loc domain.Location) error {
	key := fmt.Sprintf("drivers:geo:%s", region)
	return c.rdb.GeoAdd(ctx, key, &redis.GeoLocation{
		Name:      driverID,
		Longitude: loc.Longitude,
		Latitude:  loc.Latitude,
	}).Err()
}

// RemoveDriverLocation removes a driver from the regional GEO set (e.g. when offline or stale).
func (c *Client) RemoveDriverLocation(ctx context.Context, region, driverID string) error {
	key := fmt.Sprintf("drivers:geo:%s", region)
	return c.rdb.ZRem(ctx, key, driverID).Err()
}

// SearchNearestDrivers executes a GEOSEARCH query to locate the nearest drivers within the specified radius.
func (c *Client) SearchNearestDrivers(ctx context.Context, region string, pickup domain.Location, radiusMeters float64, count int) ([]domain.DriverCandidate, error) {
	key := fmt.Sprintf("drivers:geo:%s", region)

	query := &redis.GeoSearchLocationQuery{
		GeoSearchQuery: redis.GeoSearchQuery{
			Longitude:  pickup.Longitude,
			Latitude:   pickup.Latitude,
			Radius:     radiusMeters,
			RadiusUnit: "m",
			Sort:       "ASC",
			Count:      count,
		},
		WithCoord: true,
		WithDist:  true,
	}

	locations, err := c.rdb.GeoSearchLocation(ctx, key, query).Result()
	if err != nil {
		return nil, err
	}

	candidates := make([]domain.DriverCandidate, 0, len(locations))
	for _, loc := range locations {
		candidates = append(candidates, domain.DriverCandidate{
			DriverID:       loc.Name,
			DistanceMeters: loc.Dist,
			Location: domain.Location{
				Latitude:  loc.Latitude,
				Longitude: loc.Longitude,
			},
		})
	}

	return candidates, nil
}

// UpdateDriverStatus sets the driver's status hash with a TTL.
func (c *Client) UpdateDriverStatus(ctx context.Context, driverID string, state domain.DriverState, region string, lastSeen time.Time, ttl time.Duration) error {
	key := fmt.Sprintf("driver:%s:status", driverID)
	pipe := c.rdb.Pipeline()
	pipe.HSet(ctx, key, map[string]any{
		"state":    string(state),
		"region":   region,
		"lastSeen": strconv.FormatInt(lastSeen.UnixMilli(), 10),
	})
	pipe.Expire(ctx, key, ttl)
	_, err := pipe.Exec(ctx)
	return err
}

// GetDriverStatus reads the driver's status hash.
func (c *Client) GetDriverStatus(ctx context.Context, driverID string) (domain.DriverState, string, time.Time, error) {
	key := fmt.Sprintf("driver:%s:status", driverID)
	res, err := c.rdb.HGetAll(ctx, key).Result()
	if err != nil {
		return domain.DriverStateOffline, "", time.Time{}, err
	}
	if len(res) == 0 {
		return domain.DriverStateOffline, "", time.Time{}, domain.ErrDriverNotFound
	}

	state := domain.DriverState(res["state"])
	region := res["region"]
	var lastSeen time.Time
	if millisStr, ok := res["lastSeen"]; ok {
		if millis, err := strconv.ParseInt(millisStr, 10, 64); err == nil {
			lastSeen = time.UnixMilli(millis)
		}
	}

	return state, region, lastSeen, nil
}

