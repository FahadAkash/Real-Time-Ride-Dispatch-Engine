//go:build ignore

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/ride-dispatch/engine/internal/domain"
	"github.com/ride-dispatch/engine/internal/platform/config"
	redisstore "github.com/ride-dispatch/engine/internal/store/redis"
)

func main() {
	count := flag.Int("count", 5000, "Number of simulated drivers to seed")
	region := flag.String("region", "downtown", "Target region identifier")
	isProdConfirmed := flag.Bool("i-know-this-is-prod", false, "Confirm execution against production database")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	if strings.EqualFold(cfg.AppEnv, "prod") && !*isProdConfirmed {
		fmt.Println("REFUSED: Target database is tagged as PRODUCTION. To proceed, pass --i-know-this-is-prod flag.")
		os.Exit(1)
	}

	fmt.Printf("Connecting to Redis (%v)...\n", cfg.RedisAddrs)
	client, err := redisstore.NewClient(redisstore.Config{
		Addrs:    cfg.RedisAddrs,
		Username: cfg.RedisUsername,
		Password: cfg.RedisPassword,
		UseTLS:   cfg.RedisTLS,
	})
	if err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}
	defer client.Close()

	ctx := context.Background()
	baseLat := 40.7128 // NYC center
	baseLon := -74.0060

	batchSize := 500
	fmt.Printf("Seeding %d drivers into region '%s' (batch size: %d)...\n", *count, *region, batchSize)

	for i := 0; i < *count; i += batchSize {
		end := i + batchSize
		if end > *count {
			end = *count
		}

		geoLocations := make([]*redis.GeoLocation, 0, end-i)
		for j := i; j < end; j++ {
			driverID := fmt.Sprintf("driver-%06d", j)
			// Disperse drivers across a ~10km grid
			lat := baseLat + float64(j%200-100)*0.001
			lon := baseLon + float64(j/200-100)*0.001

			geoLocations = append(geoLocations, &redis.GeoLocation{
				Name:      driverID,
				Latitude:  lat,
				Longitude: lon,
			})
			_ = client.UpdateDriverStatus(ctx, driverID, domain.DriverStateAvailable, *region, time.Now().UTC(), 24*time.Hour)
		}

		geoKey := fmt.Sprintf("drivers:geo:%s", *region)
		if err := client.Raw().GeoAdd(ctx, geoKey, geoLocations...).Err(); err != nil {
			log.Fatalf("Batch GeoAdd failed at index %d: %v", i, err)
		}
		fmt.Printf("  Seeded %d / %d drivers...\n", end, *count)
	}

	fmt.Printf("Successfully seeded %d drivers into region '%s'.\n", *count, *region)
}

