package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"pjsk/backend/internal/config"

	"github.com/jackc/pgx/v5/pgxpool"
)

type server struct {
	config config.Config
	dbPool *pgxpool.Pool
}

type healthResponse struct {
	Service  string `json:"service"`
	Status   string `json:"status"`
	Database string `json:"database"`
	Time     string `json:"time"`
}

type appConfigResponse struct {
	Name            string   `json:"name"`
	Stage           string   `json:"stage"`
	LegacyAdminPort string   `json:"legacyAdminPort"`
	LegacyUserPort  string   `json:"legacyUserPort"`
	FrontendOrigins []string `json:"frontendOrigins"`
	Modules         []module `json:"modules"`
}

type module struct {
	Key         string `json:"key"`
	Title       string `json:"title"`
	Status      string `json:"status"`
	Description string `json:"description"`
}

func NewRouter(cfg config.Config, dbPool *pgxpool.Pool) http.Handler {
	server := &server{
		config: cfg,
		dbPool: dbPool,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", server.healthHandler)
	mux.HandleFunc("/api/config", server.configHandler)

	return withCORS(loggingMiddleware(mux), cfg.FrontendOrigins)
}

func (s *server) healthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	databaseStatus := "connected"
	status := "ok"
	httpStatus := http.StatusOK

	if s.dbPool == nil {
		databaseStatus = "not_initialized"
		status = "error"
		httpStatus = http.StatusServiceUnavailable
	} else if err := s.dbPool.Ping(ctx); err != nil {
		databaseStatus = "disconnected"
		status = "error"
		httpStatus = http.StatusServiceUnavailable
	}

	writeJSON(w, httpStatus, healthResponse{
		Service:  "pjsk-backend",
		Status:   status,
		Database: databaseStatus,
		Time:     time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *server) configHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	configResponse := appConfigResponse{
		Name:            "PJSK Goods Next",
		Stage:           "foundation",
		LegacyAdminPort: s.config.LegacyAdminPort,
		LegacyUserPort:  s.config.LegacyUserPort,
		FrontendOrigins: s.config.FrontendOrigins,
		Modules: []module{
			{
				Key:         "frontend-shell",
				Title:       "Frontend shell",
				Status:      "ready",
				Description: "Vue 3 shell is online and can read backend status.",
			},
			{
				Key:         "backend-core",
				Title:       "Backend core",
				Status:      "ready",
				Description: "Go server is connected to PostgreSQL and runs migrations on startup.",
			},
			{
				Key:         "database-core",
				Title:       "Database core",
				Status:      "ready",
				Description: "Core project, order, and payment tables are available.",
			},
			{
				Key:         "excel-import",
				Title:       "Excel import",
				Status:      "queued",
				Description: "Parser and validation will be implemented after the data models are stable.",
			},
			{
				Key:         "payment-workflow",
				Title:       "Payment workflow",
				Status:      "queued",
				Description: "Payment submission and audit APIs will follow after the data models settle.",
			},
		},
	}

	writeJSON(w, http.StatusOK, configResponse)
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf(
			"%s %s %s",
			r.Method,
			r.URL.Path,
			time.Since(start).Round(time.Millisecond),
		)
	})
}

func withCORS(next http.Handler, allowedOrigins []string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && isAllowedOrigin(origin, allowedOrigins) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
		}

		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set(
			"Access-Control-Allow-Headers",
			"Content-Type, Authorization",
		)

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func isAllowedOrigin(origin string, allowedOrigins []string) bool {
	for _, item := range allowedOrigins {
		if strings.EqualFold(origin, item) {
			return true
		}
	}

	return false
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("encode JSON response: %v", err)
	}
}
