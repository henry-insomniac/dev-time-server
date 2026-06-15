package config

import "os"

type Config struct {
	ServerAddr          string
	DatabaseURL         string
	AgentRuntimeBaseURL string
	AllowNoDatabase     bool
}

func Load() Config {
	return Config{
		ServerAddr: valueOrDefault("DEV_TIME_SERVER_ADDR", ":8080"),
		DatabaseURL: valueOrDefault(
			"DATABASE_URL",
			"postgres://dev_time:dev_time@localhost:5432/dev_time?sslmode=disable",
		),
		AgentRuntimeBaseURL: os.Getenv("DEV_TIME_AGENT_RUNTIME_BASE_URL"),
		AllowNoDatabase:     truthyEnv("DEV_TIME_ALLOW_NO_DATABASE"),
	}
}

func valueOrDefault(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func truthyEnv(name string) bool {
	value := os.Getenv(name)
	return value == "1" || value == "true" || value == "TRUE" || value == "yes"
}
