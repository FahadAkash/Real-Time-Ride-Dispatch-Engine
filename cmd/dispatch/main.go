package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ride-dispatch/engine/internal/dispatch"
	"github.com/ride-dispatch/engine/internal/events"
	"github.com/ride-dispatch/engine/internal/notify"
	"github.com/ride-dispatch/engine/internal/platform/config"
	"github.com/ride-dispatch/engine/internal/platform/logger"
	"github.com/ride-dispatch/engine/internal/platform/server"
	mongostore "github.com/ride-dispatch/engine/internal/store/mongo"
	redisstore "github.com/ride-dispatch/engine/internal/store/redis"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("Failed to load config", slog.String("error", err.Error()))
		os.Exit(1)
	}

	log := logger.New("dispatch", cfg.LogLevel, os.Stdout)
	log.Info("Starting Dispatch Service", slog.String("metricsAddr", cfg.MetricsAddr))

	// 1. Internal metrics listener
	metricsSrv, metricsErr := server.StartMetricsServer(cfg.MetricsAddr)
	go func() {
		if err := <-metricsErr; err != nil {
			log.Error("Metrics server error", slog.String("error", err.Error()))
		}
	}()

	// 2. Data Stores
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

	// 3. Kafka Producer & Outbox Relay
	kafkaProducer, err := events.NewProducer(events.Config{
		Brokers:      cfg.KafkaBrokers,
		SASLUsername: cfg.KafkaSASLUsername,
		SASLPassword: cfg.KafkaSASLPassword,
		UseTLS:       cfg.KafkaTLS,
	})
	if err != nil {
		log.Warn("Kafka producer unavailable", slog.String("error", err.Error()))
	} else {
		defer kafkaProducer.Close()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if mongoStore != nil && kafkaProducer != nil {
		outboxRelay := events.NewOutboxRelay(
			events.OutboxRelayConfig{
				PollInterval: 500 * time.Millisecond,
				BatchSize:    100,
			},
			mongoStore,
			kafkaProducer,
			log,
		)
		go outboxRelay.Start(ctx)
		log.Info("Transactional outbox relay started")
	}

	// 4. Dispatch Service Components
	var matcher *dispatch.Matcher
	if redisClient != nil {
		matcher = dispatch.NewMatcher(redisClient, redisClient)
	}

	simulatedNotifier := notify.NewSimulatedNotifier()
	_ = dispatch.NewService(
		dispatch.Config{
			CandidatesCount: cfg.DispatchCandidates,
			LockTTL:         cfg.DriverLockTTL,
			OfferTimeout:    cfg.OfferTimeout,
			MaxReassigns:    cfg.MaxReassignAttempts,
		},
		matcher,
		mongoStore,
		redisClient,
		redisClient,
		simulatedNotifier,
	)

	// 5. Graceful Shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	log.Info("Shutting down dispatch service...")
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	_ = server.StopMetricsServer(shutdownCtx, metricsSrv)
	log.Info("Dispatch service stopped cleanly")
}
