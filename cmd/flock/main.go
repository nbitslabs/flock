package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nbitslabs/flock/internal/config"
	"github.com/nbitslabs/flock/internal/db"
	"github.com/nbitslabs/flock/internal/db/sqlc"
	"github.com/nbitslabs/flock/internal/opencode"
	"github.com/nbitslabs/flock/internal/server"
)

func main() {
	configPath := flag.String("config", "flock.toml", "config file path")
	flagOpenCodeURL := flag.String("opencode-url", "", "OpenCode server URL")
	flagAddr := flag.String("addr", "", "listen address")
	flagDB := flag.String("db", "", "database path")
	flag.Parse()

	cfg := config.Load(*configPath)

	// CLI flags override config/env (highest priority)
	if *flagOpenCodeURL != "" {
		cfg.OpenCodeURL = *flagOpenCodeURL
	}
	if *flagAddr != "" {
		cfg.Addr = *flagAddr
	}
	if *flagDB != "" {
		cfg.DBPath = *flagDB
	}

	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Printf("starting flock (opencode: %s)...", cfg.OpenCodeURL)

	// Open database
	database, err := db.Open(cfg.DBPath)
	if err != nil {
		log.Fatal("failed to open database:", err)
	}
	defer database.Close()

	queries := sqlc.New(database)

	// Create shared OpenCode client
	client := opencode.NewClient(cfg.OpenCodeURL)

	// Create SSE broker
	broker := server.NewSSEBroker()

	// Create instance manager with shared client
	manager := opencode.NewManager(queries, func(instanceID, rawJSON string) {
		broker.HandleEvent(instanceID, rawJSON)
	}, client)

	// Load existing instances from DB (survives flock restarts)
	if err := manager.LoadExisting(context.Background()); err != nil {
		log.Printf("warning: failed to load existing instances: %v", err)
	}

	// Start global event subscription to the OpenCode server
	manager.StartEventSubscription()

	// Create HTTP server
	srv := server.New(queries, manager, broker)
	httpServer := &http.Server{
		Addr:    cfg.Addr,
		Handler: srv,
	}

	// Graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("flock listening on %s", cfg.Addr)
		if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatal("server error:", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	manager.StopEventSubscription()
	httpServer.Shutdown(shutdownCtx)

	log.Println("flock stopped")
}
