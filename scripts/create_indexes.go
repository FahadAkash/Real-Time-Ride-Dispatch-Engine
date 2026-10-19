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

	"github.com/ride-dispatch/engine/internal/platform/config"
	mongostore "github.com/ride-dispatch/engine/internal/store/mongo"
)

func main() {
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

	fmt.Printf("Connecting to MongoDB (%s, DB: %s)...\n", cfg.MongoURI, cfg.MongoDB)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	store, err := mongostore.NewStore(ctx, mongostore.Config{
		URI:      cfg.MongoURI,
		Database: cfg.MongoDB,
	})
	if err != nil {
		log.Fatalf("Failed to connect to MongoDB: %v", err)
	}
	defer func() { _ = store.Close(context.Background()) }()

	fmt.Println("Creating MongoDB indexes for rides, drivers, and outbox...")
	if err := store.EnsureIndexes(ctx); err != nil {
		log.Fatalf("Failed to create indexes: %v", err)
	}

	fmt.Println("All MongoDB indexes verified and created successfully.")
}

