package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	HTTPAddr      string
	DatabaseURL   string
	WebhookSecret string

	// Воркеры
	TransferWorkerCount      int
	NotificationWorkerCount  int

	// Биржа
	ExchangeFailRate float64 // для мока: вероятность ошибки (0.0 = никогда)
}

func Load() (*Config, error) {
	cfg := &Config{
		HTTPAddr:                getEnv("HTTP_ADDR", ":8080"),
		DatabaseURL:             getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/hedge?sslmode=disable"),
		WebhookSecret:           getEnv("WEBHOOK_SECRET", ""),
		TransferWorkerCount:     getEnvInt("TRANSFER_WORKER_COUNT", 2),
		NotificationWorkerCount: getEnvInt("NOTIFICATION_WORKER_COUNT", 1),
		ExchangeFailRate:        getEnvFloat("EXCHANGE_FAIL_RATE", 0.1),
	}

	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		n, err := strconv.Atoi(v)
		if err == nil {
			return n
		}
	}
	return fallback
}

func getEnvFloat(key string, fallback float64) float64 {
	if v := os.Getenv(key); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err == nil {
			return f
		}
	}
	return fallback
}
