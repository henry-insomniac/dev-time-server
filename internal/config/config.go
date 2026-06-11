package config

import "os"

type Config struct {
	ServerAddr  string
	DatabaseURL string
}

func Load() Config {
	return Config{
		ServerAddr: valueOrDefault("DEV_TIME_SERVER_ADDR", ":8080"),
		DatabaseURL: valueOrDefault(
			"DATABASE_URL",
			"postgres://dev_time:dev_time@localhost:5432/dev_time?sslmode=disable",
		),
	}
}

func valueOrDefault(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
