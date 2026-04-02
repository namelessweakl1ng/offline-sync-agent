package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"offline-sync-agent/internal/cli"
	"offline-sync-agent/internal/config"
	"offline-sync-agent/internal/db"
	"offline-sync-agent/internal/logging"
	"offline-sync-agent/internal/network"
	"offline-sync-agent/internal/queue"
	syncer "offline-sync-agent/internal/sync"
)

func main() {
	cfg, err := config.LoadClientConfig()
	if err != nil {
		log.Fatal(err)
	}

	logger := logging.New(cfg.LogLevel)

	dbClient, err := db.Open(cfg.DBPath)
	if err != nil {
		logger.Error("failed to open local database", "error", err, "db_path", cfg.DBPath)
		os.Exit(1)
	}
	defer func() {
		if err := dbClient.Close(); err != nil {
			logger.Warn("failed to close local database", "error", err)
		}
	}()

	queueRepository := queue.NewRepository(dbClient)
	networkClient := network.NewClient(
		cfg.ServerURL,
		cfg.AuthToken,
		cfg.HTTPTimeout,
		cfg.InsecureSkipVerify,
		logger,
	)
	syncService := syncer.NewService(queueRepository, networkClient, logger, 3)

	app := cli.NewApp(cfg, queueRepository, syncService, logger, os.Stdout, os.Stderr)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := app.Run(ctx, os.Args[1:]); err != nil {
		logger.Error("command failed", "error", err)
		os.Exit(1)
	}
}
