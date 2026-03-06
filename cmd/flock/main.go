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

	"github.com/nbitslabs/flock/internal/db"
	"github.com/nbitslabs/flock/internal/db/sqlc"
	"github.com/nbitslabs/flock/internal/opencode"
	"github.com/nbitslabs/flock/internal/server"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	dbPath := flag.String("db", "flock.db", "database path")
	flag.Parse()

	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("starting flock...")

	// Open database
	database, err := db.Open(*dbPath)
	if err != nil {
		log.Fatal("failed to open database:", err)
	}
	defer database.Close()

	queries := sqlc.New(database)

	// Create SSE broker
	broker := server.NewSSEBroker()

	// Create instance manager
	manager := opencode.NewManager(queries, func(instanceID, rawJSON string) {
		broker.HandleEvent(instanceID, rawJSON)
	})

	// Mark stale instances as stopped (handles crash recovery where
	// instances are still marked as running but the processes are dead)
	if err := queries.MarkStaleInstancesStopped(context.Background()); err != nil {
		log.Printf("warning: failed to mark stale instances: %v", err)
	}

	// Create HTTP server
	srv := server.New(queries, manager, broker)
	httpServer := &http.Server{
		Addr:    *addr,
		Handler: srv,
	}

	// Graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("flock listening on %s", *addr)
		if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatal("server error:", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down...")

	// Stop all OpenCode instances
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	manager.StopAll(shutdownCtx)
	httpServer.Shutdown(shutdownCtx)

	log.Println("flock stopped")
}
