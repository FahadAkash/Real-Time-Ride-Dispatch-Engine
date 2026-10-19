package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ride-dispatch/engine/internal/dispatch"
	"github.com/ride-dispatch/engine/internal/notify"
	"github.com/ride-dispatch/engine/internal/platform/config"
	"github.com/ride-dispatch/engine/internal/platform/logger"
	"github.com/ride-dispatch/engine/internal/platform/server"
	"github.com/ride-dispatch/engine/internal/worker"
	mongostore "github.com/ride-dispatch/engine/internal/store/mongo"
	redisstore "github.com/ride-dispatch/engine/internal/store/redis"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("Failed to load config", slog.String("error", err.Error()))
		os.Exit(1)
	}

	log := logger.New("timeout-worker", cfg.LogLevel, os.Stdout)
	log.Info("Starting Timeout Worker", slog.String("metricsAddr", cfg.MetricsAddr))

	// 1. Internal metrics listener
	metricsSrv, metricsErr := server.StartMetricsServer(cfg.MetricsAddr)
	go func() {
		if err := <-metricsErr; err != nil {
			log.Error("Metrics server error", slog.String("error", err.Error()))
		}
	}()

	// 2. Stores
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

	mongoCtx, mongoCancel := context.WithTimeout(context.Background(), 5*time.Second)
	mongoStore, err := mongostore.NewStore(mongoCtx, mongostore.Config{
		URI:      cfg.MongoURI,
		Database: cfg.MongoDB,
	})
	mongoCancel()
	if err != nil {
		log.Warn("MongoDB connection unavailable", slog.String("error", err.Error()))
	} else {
		defer func() { _ = mongoStore.Close(context.Background()) }()
	}

	// 3. Matcher & Worker
	var matcher *dispatch.Matcher
	if redisClient != nil {
		matcher = dispatch.NewMatcher(redisClient, redisClient)
	}

	simulatedNotifier := notify.NewSimulatedNotifier()
	timeoutWorker := worker.NewTimeoutWorker(
		worker.TimeoutWorkerConfig{
			PollInterval:    1 * time.Second,
			OfferTimeout:    cfg.OfferTimeout,
			LockTTL:         cfg.DriverLockTTL,
			MaxReassigns:    cfg.MaxReassignAttempts,
			CandidatesCount: cfg.DispatchCandidates,
			BatchSize:       100,
		},
		matcher,
		redisClient,
		mongoStore,
		simulatedNotifier,
		log,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go timeoutWorker.Start(ctx)
	log.Info("Timeout worker polling loop active")

	// 4. Graceful Shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	log.Info("Shutting down timeout worker...")
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	_ = server.StopMetricsServer(shutdownCtx, metricsSrv)
	log.Info("Timeout worker stopped cleanly")
}
