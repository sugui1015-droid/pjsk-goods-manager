package main

import (
	"context"
	"embed"
	"log"
	"net/http"
	"time"

	"pjsk/backend/internal/api"
	"pjsk/backend/internal/config"
	"pjsk/backend/internal/database"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	dbPool, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("connect to database: %v", err)
	}
	defer dbPool.Close()

	log.Println("database connected")

	if err := database.RunMigrations(ctx, dbPool, migrationFS, "migrations"); err != nil {
		log.Fatalf("run database migrations: %v", err)
	}

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           api.NewRouter(cfg, dbPool),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("backend listening on http://localhost:%s", cfg.Port)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
