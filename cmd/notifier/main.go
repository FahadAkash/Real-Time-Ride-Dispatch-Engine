package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ride-dispatch/engine/internal/notify"
	"github.com/ride-dispatch/engine/internal/platform/config"
	"github.com/ride-dispatch/engine/internal/platform/logger"
	"github.com/ride-dispatch/engine/internal/platform/server"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("Failed to load config", slog.String("error", err.Error()))
		os.Exit(1)
	}

	log := logger.New("notifier", cfg.LogLevel, os.Stdout)
	log.Info("Starting Notification Service", slog.String("metricsAddr", cfg.MetricsAddr))

	metricsSrv, metricsErr := server.StartMetricsServer(cfg.MetricsAddr)
	go func() {
		if err := <-metricsErr; err != nil {
			log.Error("Metrics server error", slog.String("error", err.Error()))
		}
	}()

	_ = notify.NewSimulatedNotifier()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	log.Info("Shutting down notifier service...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	_ = server.StopMetricsServer(shutdownCtx, metricsSrv)
	log.Info("Notifier service stopped cleanly")
}
