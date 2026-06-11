package config

import "os"

type Config struct {
	ServerAddr string
}

func Load() Config {
	return Config{
		ServerAddr: valueOrDefault("DEV_TIME_SERVER_ADDR", ":8080"),
	}
}

func valueOrDefault(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
