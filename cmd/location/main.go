package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ride-dispatch/engine/internal/location"
	"github.com/ride-dispatch/engine/internal/platform/config"
	"github.com/ride-dispatch/engine/internal/platform/logger"
	"github.com/ride-dispatch/engine/internal/platform/server"
	redisstore "github.com/ride-dispatch/engine/internal/store/redis"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("Failed to load config", slog.String("error", err.Error()))
		os.Exit(1)
	}

	log := logger.New("location", cfg.LogLevel, os.Stdout)
	log.Info("Starting Location Service", slog.String("metricsAddr", cfg.MetricsAddr))

	// 1. Internal metrics listener
	metricsSrv, metricsErr := server.StartMetricsServer(cfg.MetricsAddr)
	go func() {
		if err := <-metricsErr; err != nil {
			log.Error("Metrics server error", slog.String("error", err.Error()))
		}
	}()

	// 2. Redis Store
	redisClient, err := redisstore.NewClient(redisstore.Config{
		Addrs:    cfg.RedisAddrs,
		Username: cfg.RedisUsername,
		Password: cfg.RedisPassword,
		UseTLS:   cfg.RedisTLS,
	})
	if err != nil {
		log.Warn("Redis connection unavailable", slog.String("error", err.Error()))
	} else {
		defer redisClient.Close()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 3. Location Service & Staleness Cleaner
	validator := location.NewValidator(location.DefaultBoundingBox, 160.0, 2*time.Second)
	_ = location.NewService(redisClient, validator, cfg.LocationStaleAfter)

	if redisClient != nil {
		stalenessCleaner := location.NewStalenessCleaner(redisClient, cfg.LocationStaleAfter, 10*time.Second, log)
		go stalenessCleaner.Start(ctx, "downtown", nil)
		log.Info("Staleness cleaner started")
	}

	// 4. Graceful Shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	log.Info("Shutting down location service...")
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	_ = server.StopMetricsServer(shutdownCtx, metricsSrv)
	log.Info("Location service stopped cleanly")
}
