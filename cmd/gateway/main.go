package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ride-dispatch/engine/internal/dispatch"
	"github.com/ride-dispatch/engine/internal/gateway"
	"github.com/ride-dispatch/engine/internal/location"
	"github.com/ride-dispatch/engine/internal/notify"
	"github.com/ride-dispatch/engine/internal/platform/config"
	"github.com/ride-dispatch/engine/internal/platform/logger"
	_ "github.com/ride-dispatch/engine/internal/platform/metrics"
	"github.com/ride-dispatch/engine/internal/platform/server"
	mongostore "github.com/ride-dispatch/engine/internal/store/mongo"
	redisstore "github.com/ride-dispatch/engine/internal/store/redis"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("Failed to load configuration", slog.String("error", err.Error()))
		os.Exit(1)
	}

	log := logger.New("gateway", cfg.LogLevel, os.Stdout)
	log.Info("Starting API Gateway", slog.String("httpAddr", cfg.HTTPAddr), slog.String("metricsAddr", cfg.MetricsAddr))

	// 1. Start internal metrics server on METRICS_ADDR
	metricsSrv, metricsErrChan := server.StartMetricsServer(cfg.MetricsAddr)
	go func() {
		if err := <-metricsErrChan; err != nil {
			log.Error("Metrics server error", slog.String("error", err.Error()))
		}
	}()

	// 2. Initialize Data Stores
	redisClient, err := redisstore.NewClient(redisstore.Config{
		Addrs:    cfg.RedisAddrs,
		Username: cfg.RedisUsername,
		Password: cfg.RedisPassword,
		UseTLS:   cfg.RedisTLS,
	})
	if err != nil {
		log.Warn("Redis connection offline (falling back to direct responses)", slog.String("error", err.Error()))
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
		log.Warn("MongoDB connection offline (falling back to direct responses)", slog.String("error", err.Error()))
	} else {
		defer func() { _ = mongoStore.Close(context.Background()) }()
	}

	// 3. Initialize Domain Services
	var matcher *dispatch.Matcher
	if redisClient != nil {
		matcher = dispatch.NewMatcher(redisClient, redisClient)
	}

	simulatedNotifier := notify.NewSimulatedNotifier()
	dispatchSvc := dispatch.NewService(
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

	locationValidator := location.NewValidator(location.DefaultBoundingBox, 160.0, 2*time.Second)
	locationSvc := location.NewService(redisClient, locationValidator, cfg.LocationStaleAfter)

	// 4. Initialize JWT Validator (Local dev generates key pair if none configured)
	privKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	jwtValidator := gateway.NewJWTValidator(cfg.JWTIssuer, cfg.JWTAudience, cfg.JWTAllowedAlgs, &privKey.PublicKey)

	// 5. Build Handlers & Router
	handlers := gateway.NewHandlers(dispatchSvc, locationSvc)
	router := gateway.NewRouter(gateway.RouterConfig{
		JWTValidator:    jwtValidator,
		RateLimiter:     redisClient,
		RateLimitPerMin: cfg.RateLimitRideRequestPerMin,
		Logger:          log,
	}, handlers)

	httpSrv := server.New(server.Config{
		Addr: cfg.HTTPAddr,
	}, router)

	srvErrChan := httpSrv.Start()

	// 6. Graceful Shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-srvErrChan:
		log.Error("Server error", slog.String("error", err.Error()))
	case sig := <-quit:
		log.Info("Shutting down gateway gracefully", slog.String("signal", sig.String()))
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := httpSrv.Shutdown(shutdownCtx); err != nil {
			log.Error("HTTP server shutdown error", slog.String("error", err.Error()))
		}
		if err := server.StopMetricsServer(shutdownCtx, metricsSrv); err != nil {
			log.Error("Metrics server shutdown error", slog.String("error", err.Error()))
		}
		log.Info("Gateway stopped cleanly")
	}
}
