package config

import "os"

type Config struct {
	ServerAddr             string
	DatabaseURL            string
	AgentRuntimeBaseURL    string
	AllowNoDatabase        bool
	GitHubAppID            string
	GitHubAppSlug          string
	GitHubPrivateKeyPath   string
	GitHubSetupStateSecret string
	GitHubAPIBaseURL       string
	GitHubInstallBaseURL   string
	FrontendBaseURL        string
}

func Load() Config {
	return Config{
		ServerAddr: valueOrDefault("DEV_TIME_SERVER_ADDR", ":8080"),
		DatabaseURL: valueOrDefault(
			"DATABASE_URL",
			"postgres://dev_time:dev_time@localhost:5432/dev_time?sslmode=disable",
		),
		AgentRuntimeBaseURL:    os.Getenv("DEV_TIME_AGENT_RUNTIME_BASE_URL"),
		AllowNoDatabase:        truthyEnv("DEV_TIME_ALLOW_NO_DATABASE"),
		GitHubAppID:            os.Getenv("GITHUB_APP_ID"),
		GitHubAppSlug:          os.Getenv("GITHUB_APP_SLUG"),
		GitHubPrivateKeyPath:   os.Getenv("GITHUB_APP_PRIVATE_KEY_PATH"),
		GitHubSetupStateSecret: os.Getenv("GITHUB_APP_SETUP_STATE_SECRET"),
		GitHubAPIBaseURL:       valueOrDefault("GITHUB_API_BASE_URL", "https://api.github.com"),
		GitHubInstallBaseURL:   valueOrDefault("GITHUB_INSTALLATION_BASE_URL", "https://github.com"),
		FrontendBaseURL:        valueOrDefault("DEV_TIME_FRONTEND_BASE_URL", "http://localhost:5173"),
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
