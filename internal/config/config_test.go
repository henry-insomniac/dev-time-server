package config_test

import (
	"testing"

	"github.com/henry-insomniac/dev-time-server/internal/config"
)

func TestLoadUsesConfiguredServerAddress(t *testing.T) {
	t.Setenv("DEV_TIME_SERVER_ADDR", "127.0.0.1:18080")

	loaded := config.Load()

	if loaded.ServerAddr != "127.0.0.1:18080" {
		t.Fatalf("expected configured server addr, got %q", loaded.ServerAddr)
	}
}

func TestLoadUsesDefaultServerAddress(t *testing.T) {
	t.Setenv("DEV_TIME_SERVER_ADDR", "")

	loaded := config.Load()

	if loaded.ServerAddr != ":8080" {
		t.Fatalf("expected default server addr, got %q", loaded.ServerAddr)
	}
}
