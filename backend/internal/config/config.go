package config

import (
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	Port            string
	DatabaseURL     string
	LegacyAdminPort string
	LegacyUserPort  string
	FrontendOrigins []string
}

func Load() (Config, error) {
	if err := godotenv.Load(); err != nil {
		log.Printf(".env not loaded: %v", err)
	}

	databaseURL, err := databaseURLFromEnv()
	if err != nil {
		return Config{}, err
	}

	return Config{
		Port:            EnvOr("APP_PORT", EnvOr("SERVER_PORT", EnvOr("BACKEND_PORT", "8080"))),
		DatabaseURL:     databaseURL,
		LegacyAdminPort: EnvOr("LEGACY_STREAMLIT_ADMIN_PORT", "8512"),
		LegacyUserPort:  EnvOr("LEGACY_STREAMLIT_USER_PORT", "8513"),
		FrontendOrigins: []string{
			"http://localhost:5173",
			"http://127.0.0.1:5173",
		},
	}, nil
}

func EnvOr(name string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}

	return value
}

func databaseURLFromEnv() (string, error) {
	if databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL")); databaseURL != "" {
		return databaseURL, nil
	}

	host := EnvOr("DATABASE_HOST", "localhost")
	port := EnvOr("DATABASE_PORT", "5432")
	user := strings.TrimSpace(os.Getenv("DATABASE_USER"))
	password := os.Getenv("DATABASE_PASSWORD")
	name := strings.TrimSpace(os.Getenv("DATABASE_NAME"))

	if user == "" {
		return "", fmt.Errorf("DATABASE_USER is missing")
	}
	if name == "" {
		return "", fmt.Errorf("DATABASE_NAME is missing")
	}

	u := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(user, password),
		Host:   fmt.Sprintf("%s:%s", host, port),
		Path:   name,
	}
	query := u.Query()
	query.Set("sslmode", EnvOr("DATABASE_SSLMODE", "disable"))
	u.RawQuery = query.Encode()

	return u.String(), nil
}
