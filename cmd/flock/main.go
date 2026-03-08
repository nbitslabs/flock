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

	"github.com/nbitslabs/flock/internal/agent"
	"github.com/nbitslabs/flock/internal/auth"
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

	// Ensure the global data directory exists
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		log.Fatalf("failed to create data dir %s: %v", cfg.DataDir, err)
	}

	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Printf("starting flock (opencode: %s)...", cfg.OpenCodeURL)

	if err := opencode.SyncAgents(); err != nil {
		log.Printf("warning: failed to sync agents: %v", err)
	}

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

	// Create agent harness
	harness := agent.NewHarness(client, queries, cfg.Agent, broker.SubscribeInternal, cfg.Agent.DataDir)
	harness.Start()

	// Create instance manager with shared client
	manager := opencode.NewManager(queries, func(instanceID, rawJSON string) {
		broker.HandleEvent(instanceID, rawJSON)
		harness.HandleEvent(instanceID, rawJSON)
	}, client)

	// Set instance hook so harness starts/stops per-instance schedulers
	manager.SetInstanceHook(func(action string, inst *opencode.Instance) {
		switch action {
		case "register":
			harness.StartInstance(inst.ID, inst.WorkingDirectory)
		case "stop":
			harness.StopInstance(inst.ID)
		}
	})

	// Load existing instances from DB (survives flock restarts)
	if err := manager.LoadExisting(context.Background()); err != nil {
		log.Printf("warning: failed to load existing instances: %v", err)
	}

	// Start agent schedulers for existing instances
	for _, inst := range manager.List() {
		harness.StartInstance(inst.ID, inst.WorkingDirectory)
	}

	// Start global event subscription to the OpenCode server
	manager.StartEventSubscription()

	// Create HTTP server (reuse same client for flock agent - it uses the data dir as working dir)
	authEnabled := cfg.Auth.Username != "" && cfg.Auth.Password != ""
	var authPassHash string
	if authEnabled {
		hash, err := auth.HashPassword(cfg.Auth.Password)
		if err != nil {
			log.Fatal("failed to hash auth password:", err)
		}
		authPassHash = hash
		log.Printf("authentication enabled for user: %s", cfg.Auth.Username)
	}

	srv := server.New(queries, manager, broker, harness, cfg.DataDir, cfg.BasePath, client, authEnabled, cfg.Auth.Username, authPassHash)
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

	harness.Stop()
	manager.StopEventSubscription()
	httpServer.Shutdown(shutdownCtx)

	log.Println("flock stopped")
}
