package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/ride-dispatch/engine/internal/domain"
	"github.com/ride-dispatch/engine/internal/gateway"
	"github.com/ride-dispatch/engine/internal/platform/logger"
)

func main() {
	gatewayURL := flag.String("gateway", "http://localhost:8080", "Gateway HTTP URL")
	driversCount := flag.Int("drivers", 1000, "Number of concurrent simulated drivers")
	targetQPS := flag.Int("qps", 500, "Target ride requests per second")
	duration := flag.Duration("duration", 30*time.Second, "Load generation duration")
	flag.Parse()

	log := logger.New("loadgen", "info", os.Stdout)
	log.Info("Starting Load Generator",
		slog.String("gateway", *gatewayURL),
		slog.Int("drivers", *driversCount),
		slog.Int("targetQPS", *targetQPS),
		slog.Duration("duration", *duration),
	)

	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		log.Error("Failed to generate rsa key", slog.String("error", err.Error()))
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *duration)
	defer cancel()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-quit
		cancel()
	}()

	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        1000,
			MaxIdleConnsPerHost: 1000,
			IdleConnTimeout:     90 * time.Second,
		},
	}

	var wg sync.WaitGroup

	// 1. Simulate Driver Heartbeat Goroutines (every ~5s)
	for i := 0; i < *driversCount; i++ {
		wg.Add(1)
		driverID := fmt.Sprintf("driver-%06d", i)
		go func(id string, index int) {
			defer wg.Done()
			ticker := time.NewTicker(5 * time.Second)
			defer ticker.Stop()

			// Initial offset
			baseLat := 40.7128 + float64(index%100)*0.001
			baseLon := -74.0060 + float64(index/100)*0.001

			token := generateToken(privKey, id, "driver")

			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					dto := gateway.DriverUpdateLocationDTO{
						DriverID: id,
						Location: domain.Location{
							Latitude:  baseLat + (float64(time.Now().Unix()%10)-5)*0.0001,
							Longitude: baseLon + (float64(time.Now().Unix()%10)-5)*0.0001,
						},
						Region: "downtown",
						State:  domain.DriverStateAvailable,
					}
					payload, _ := json.Marshal(dto)
					req, _ := http.NewRequestWithContext(ctx, "POST", *gatewayURL+"/api/driver/update-location", bytes.NewReader(payload))
					req.Header.Set("Authorization", "Bearer "+token)
					req.Header.Set("Content-Type", "application/json")
					resp, reqErr := client.Do(req)
					if reqErr == nil {
						_ = resp.Body.Close()
					}
				}
			}
		}(driverID, i)
	}

	// 2. Simulate Rider Dispatch Requests
	var (
		reqCounter  int64
		succCounter int64
		errCounter  int64
	)

	reqInterval := time.Duration(float64(time.Second) / float64(*targetQPS))
	reqTicker := time.NewTicker(reqInterval)
	defer reqTicker.Stop()

	log.Info("Load generator running...")
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-reqTicker.C:
				atomic.AddInt64(&reqCounter, 1)
				go func() {
					riderID := fmt.Sprintf("rider-%06d", randInt(1, 100000))
					token := generateToken(privKey, riderID, "rider")
					idemKey := uuid.New().String()

					dto := gateway.RequestRideRequestDTO{
						RiderID: riderID,
						Pickup: domain.Location{
							Latitude:  40.7128 + (float64(randInt(0, 50))-25)*0.001,
							Longitude: -74.0060 + (float64(randInt(0, 50))-25)*0.001,
						},
						Dropoff: domain.Location{
							Latitude:  40.7580,
							Longitude: -73.9855,
						},
						ServiceType: domain.ServiceStandard,
						Region:      "downtown",
					}
					payload, _ := json.Marshal(dto)
					req, _ := http.NewRequestWithContext(ctx, "POST", *gatewayURL+"/api/ride/request", bytes.NewReader(payload))
					req.Header.Set("Authorization", "Bearer "+token)
					req.Header.Set("Content-Type", "application/json")
					req.Header.Set("Idempotency-Key", idemKey)

					resp, err := client.Do(req)
					if err != nil {
						atomic.AddInt64(&errCounter, 1)
						return
					}
					_ = resp.Body.Close()
					if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
						atomic.AddInt64(&succCounter, 1)
					} else {
						atomic.AddInt64(&errCounter, 1)
					}
				}()
			}
		}
	}()

	<-ctx.Done()
	log.Info("Load generation finished",
		slog.Int64("totalRequests", atomic.LoadInt64(&reqCounter)),
		slog.Int64("successfulRequests", atomic.LoadInt64(&succCounter)),
		slog.Int64("failedRequests", atomic.LoadInt64(&errCounter)),
	)
}

func generateToken(privKey *rsa.PrivateKey, sub, role string) string {
	claims := &gateway.UserClaims{
		Role: role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   sub,
			Issuer:    "dispatch-auth",
			Audience:  jwt.ClaimStrings{"dispatch-engine"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(15 * time.Minute)),
		},
	}
	t, _ := jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(privKey)
	return t
}

func randInt(min, max int) int {
	return int(math.Abs(float64(time.Now().UnixNano()%int64(max-min+1)))) + min
}

