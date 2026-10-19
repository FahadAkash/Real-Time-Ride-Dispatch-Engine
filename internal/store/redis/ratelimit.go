package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

var rateLimitScript = redis.NewScript(`
local current = redis.call("incr", KEYS[1])
if current == 1 then
    redis.call("expire", KEYS[1], ARGV[1])
end
return current
`)

// AllowRateLimit checks if a principal exceeds the allowed request limit within the duration window.
func (c *Client) AllowRateLimit(ctx context.Context, principal, route string, limit int, window time.Duration) (bool, error) {
	key := fmt.Sprintf("rl:%s:%s", principal, route)
	windowSeconds := int(window.Seconds())
	if windowSeconds < 1 {
		windowSeconds = 1
	}

	res, err := rateLimitScript.Run(ctx, c.rdb, []string{key}, windowSeconds).Result()
	if err != nil {
		return false, err
	}

	count, ok := res.(int64)
	if !ok {
		return false, fmt.Errorf("unexpected rate limit return type: %T", res)
	}

	return count <= int64(limit), nil
}

