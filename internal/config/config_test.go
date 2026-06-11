package config_test

import (
	"testing"

	"github.com/henry-insomniac/dev-time-server/internal/config"
)

func TestLoadUsesConfiguredServerAddress(t *testing.T) {
	t.Setenv("DEV_TIME_SERVER_ADDR", "127.0.0.1:18080")
	t.Setenv("DATABASE_URL", "postgres://custom:custom@localhost:5432/custom")

	loaded := config.Load()

	if loaded.ServerAddr != "127.0.0.1:18080" {
		t.Fatalf("expected configured server addr, got %q", loaded.ServerAddr)
	}
	if loaded.DatabaseURL != "postgres://custom:custom@localhost:5432/custom" {
		t.Fatalf("expected configured database url, got %q", loaded.DatabaseURL)
	}
}

func TestLoadUsesDefaultServerAddress(t *testing.T) {
	t.Setenv("DEV_TIME_SERVER_ADDR", "")
	t.Setenv("DATABASE_URL", "")

	loaded := config.Load()

	if loaded.ServerAddr != ":8080" {
		t.Fatalf("expected default server addr, got %q", loaded.ServerAddr)
	}
	if loaded.DatabaseURL != "postgres://dev_time:dev_time@localhost:5432/dev_time?sslmode=disable" {
		t.Fatalf("expected default database url, got %q", loaded.DatabaseURL)
	}
}
