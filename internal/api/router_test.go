package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/henry-insomniac/dev-time-server/internal/api"
)

func TestHealthzReportsServiceIsReady(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()

	api.NewRouter().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.Code)
	}

	var body struct {
		Status  string `json:"status"`
		Service string `json:"service"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode health response: %v", err)
	}

	if body.Status != "ok" {
		t.Fatalf("expected status ok, got %q", body.Status)
	}
	if body.Service != "dev-time-server" {
		t.Fatalf("expected service dev-time-server, got %q", body.Service)
	}
}

func TestRouterAllowsLocalDevCORS(t *testing.T) {
	request := httptest.NewRequest(http.MethodOptions, "/api/projects", nil)
	request.Header.Set("Origin", "http://localhost:5173")
	response := httptest.NewRecorder()

	api.NewRouter().ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("expected preflight status 204, got %d", response.Code)
	}
	if response.Header().Get("Access-Control-Allow-Origin") != "http://localhost:5173" {
		t.Fatalf("expected localhost origin to be allowed, got %q", response.Header().Get("Access-Control-Allow-Origin"))
	}
	if response.Header().Get("Access-Control-Allow-Methods") == "" {
		t.Fatal("expected allowed methods header")
	}
}

func TestGitHubSettingsWithoutStoreReportsDisconnected(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/settings/github", nil)
	response := httptest.NewRecorder()

	api.NewRouter().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected github settings status 200, got %d: %s", response.Code, response.Body.String())
	}

	var body struct {
		Connected     bool     `json:"connected"`
		Provider      string   `json:"provider"`
		Repositories  []string `json:"repositories"`
		StorageStatus string   `json:"storage_status"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode github settings response: %v", err)
	}
	if body.Connected {
		t.Fatalf("expected disconnected github settings without store, got %#v", body)
	}
	if body.Provider != "github_app" {
		t.Fatalf("expected github_app provider, got %q", body.Provider)
	}
	if len(body.Repositories) != 0 {
		t.Fatalf("expected no repositories without store, got %#v", body.Repositories)
	}
	if body.StorageStatus != "unavailable" {
		t.Fatalf("expected unavailable storage status, got %q", body.StorageStatus)
	}
}
