package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"offline-sync-agent/internal/config"
	"offline-sync-agent/internal/logging"
	"offline-sync-agent/internal/server"
)

func main() {
	cfg, err := config.LoadServerConfig()
	if err != nil {
		log.Fatal(err)
	}

	if err := cfg.Validate(); err != nil {
		log.Fatal(err)
	}

	logger := logging.New(cfg.LogLevel)

	store, err := server.NewStore(cfg.StoreBackend)
	if err != nil {
		logger.Error("failed to create backend store", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	httpServer := server.New(cfg, store, logger)
	if err := httpServer.Run(ctx); err != nil {
		logger.Error("backend server exited with error", "error", err)
		os.Exit(1)
	}
}
