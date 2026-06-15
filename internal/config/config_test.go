package config_test

import (
	"testing"

	"github.com/henry-insomniac/dev-time-server/internal/config"
)

func TestLoadUsesConfiguredServerAddress(t *testing.T) {
	t.Setenv("DEV_TIME_SERVER_ADDR", "127.0.0.1:18080")
	t.Setenv("DATABASE_URL", "postgres://custom:custom@localhost:5432/custom")
	t.Setenv("DEV_TIME_AGENT_RUNTIME_BASE_URL", "http://127.0.0.1:8000")
	t.Setenv("DEV_TIME_ALLOW_NO_DATABASE", "true")

	loaded := config.Load()

	if loaded.ServerAddr != "127.0.0.1:18080" {
		t.Fatalf("expected configured server addr, got %q", loaded.ServerAddr)
	}
	if loaded.DatabaseURL != "postgres://custom:custom@localhost:5432/custom" {
		t.Fatalf("expected configured database url, got %q", loaded.DatabaseURL)
	}
	if loaded.AgentRuntimeBaseURL != "http://127.0.0.1:8000" {
		t.Fatalf("expected configured agent runtime base url, got %q", loaded.AgentRuntimeBaseURL)
	}
	if !loaded.AllowNoDatabase {
		t.Fatal("expected no database dev mode to be enabled")
	}
}

func TestLoadUsesDefaultServerAddress(t *testing.T) {
	t.Setenv("DEV_TIME_SERVER_ADDR", "")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("DEV_TIME_AGENT_RUNTIME_BASE_URL", "")
	t.Setenv("DEV_TIME_ALLOW_NO_DATABASE", "")

	loaded := config.Load()

	if loaded.ServerAddr != ":8080" {
		t.Fatalf("expected default server addr, got %q", loaded.ServerAddr)
	}
	if loaded.DatabaseURL != "postgres://dev_time:dev_time@localhost:5432/dev_time?sslmode=disable" {
		t.Fatalf("expected default database url, got %q", loaded.DatabaseURL)
	}
	if loaded.AgentRuntimeBaseURL != "" {
		t.Fatalf("expected empty default agent runtime base url, got %q", loaded.AgentRuntimeBaseURL)
	}
	if loaded.AllowNoDatabase {
		t.Fatal("expected no database dev mode to be disabled by default")
	}
}
