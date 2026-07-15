package main

import (
	"context"
	"embed"
	"log"
	"net"
	"net/http"
	"time"

	"pjsk/backend/internal/api"
	"pjsk/backend/internal/config"
	"pjsk/backend/internal/database"
	"pjsk/backend/internal/logsafe"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

const (
	// databaseConnectTimeout bounds pool creation and the first Ping only, so a
	// bad DSN or an unreachable database fails startup fast instead of stalling
	// the service wrapper's restart-with-backoff loop.
	databaseConnectTimeout = 10 * time.Second

	// databaseMigrationTimeout is deliberately separate from — and much larger
	// than — the connect budget: a fresh database applies every migration in
	// sequence, which is on the order of a hundred round trips and can take real
	// time against a remote database. It stays finite so a migration blocked on a
	// lock can never hang startup forever.
	databaseMigrationTimeout = 2 * time.Minute
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	connectCtx, cancelConnect := context.WithTimeout(context.Background(), databaseConnectTimeout)
	defer cancelConnect()

	dbPool, err := database.Connect(connectCtx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("connect to database: %s", logsafe.Category(err))
	}
	defer dbPool.Close()

	log.Println("database connected")

	migrateCtx, cancelMigrate := context.WithTimeout(context.Background(), databaseMigrationTimeout)
	defer cancelMigrate()

	if err := database.RunMigrations(migrateCtx, dbPool, migrationFS, "migrations"); err != nil {
		log.Fatalf("run database migrations: %s", logsafe.Category(err))
	}

	server := &http.Server{
		Addr:              net.JoinHostPort(cfg.Host, cfg.Port),
		Handler:           api.NewRouter(cfg, dbPool),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("backend listening on %s", server.Addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
