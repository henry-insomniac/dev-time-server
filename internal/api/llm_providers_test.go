package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/henry-insomniac/dev-time-server/internal/api"
	"github.com/henry-insomniac/dev-time-server/internal/testsupport"
)

func TestLLMProviderConfigDoesNotReturnPlaintextAPIKey(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	store := testsupport.NewMigratedStore(t, ctx)
	router := api.NewRouter(api.Dependencies{Store: store})

	payload := []byte(`{
		"provider": "openai",
		"base_url": "https://api.openai.com/v1",
		"model": "gpt-5",
		"api_key": "sk-test-secret-value"
	}`)

	saveResponse := performJSONRequest(router, http.MethodPost, "/api/settings/llm-providers", payload)
	if saveResponse.Code != http.StatusCreated {
		t.Fatalf("expected save status 201, got %d: %s", saveResponse.Code, saveResponse.Body.String())
	}
	if bytes.Contains(saveResponse.Body.Bytes(), []byte("sk-test-secret-value")) {
		t.Fatal("save response leaked plaintext API key")
	}

	getResponse := performJSONRequest(router, http.MethodGet, "/api/settings/llm-providers", nil)
	if getResponse.Code != http.StatusOK {
		t.Fatalf("expected get status 200, got %d: %s", getResponse.Code, getResponse.Body.String())
	}
	if bytes.Contains(getResponse.Body.Bytes(), []byte("sk-test-secret-value")) {
		t.Fatal("get response leaked plaintext API key")
	}
	if !bytes.Contains(getResponse.Body.Bytes(), []byte(`"provider":"openai"`)) {
		t.Fatalf("expected provider in response, got %s", getResponse.Body.String())
	}
	if !bytes.Contains(getResponse.Body.Bytes(), []byte(`"configured":true`)) {
		t.Fatalf("expected configured flag in response, got %s", getResponse.Body.String())
	}
}

func TestLLMProviderSettingsExposeSupportedProviders(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	store := testsupport.NewMigratedStore(t, ctx)
	router := api.NewRouter(api.Dependencies{Store: store})

	response := performJSONRequest(router, http.MethodGet, "/api/settings/llm-providers", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("expected get status 200, got %d: %s", response.Code, response.Body.String())
	}

	var body struct {
		Providers []struct {
			Provider   string `json:"provider"`
			BaseURL    string `json:"base_url"`
			Model      string `json:"model"`
			Configured bool   `json:"configured"`
		} `json:"providers"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode providers: %v", err)
	}
	if len(body.Providers) != 2 {
		t.Fatalf("expected openai and deepseek providers, got %#v", body.Providers)
	}
	if body.Providers[0].Provider != "openai" || body.Providers[0].BaseURL == "" || body.Providers[0].Model == "" {
		t.Fatalf("expected openai defaults, got %#v", body.Providers[0])
	}
	if body.Providers[1].Provider != "deepseek" || body.Providers[1].BaseURL == "" || body.Providers[1].Model == "" {
		t.Fatalf("expected deepseek defaults, got %#v", body.Providers[1])
	}
	if body.Providers[0].Configured || body.Providers[1].Configured {
		t.Fatalf("expected unconfigured defaults, got %#v", body.Providers)
	}
}

func TestLLMProviderSettingsRejectUnsupportedProvider(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	store := testsupport.NewMigratedStore(t, ctx)
	router := api.NewRouter(api.Dependencies{Store: store})

	response := performJSONRequest(
		router,
		http.MethodPost,
		"/api/settings/llm-providers",
		[]byte(`{
			"provider": "anthropic",
			"base_url": "https://api.anthropic.com",
			"model": "claude",
			"api_key": "secret"
		}`),
	)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected unsupported provider status 400, got %d: %s", response.Code, response.Body.String())
	}
}

func TestInternalLLMProviderConfigReturnsDecryptedEnabledProvider(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	store := testsupport.NewMigratedStore(t, ctx)
	router := api.NewRouter(api.Dependencies{Store: store})

	saveResponse := performJSONRequest(
		router,
		http.MethodPost,
		"/api/settings/llm-providers",
		[]byte(`{
			"provider": "openai",
			"base_url": "https://api.openai.com/v1",
			"model": "gpt-4.1",
			"api_key": "sk-real-agent-key"
		}`),
	)
	if saveResponse.Code != http.StatusCreated {
		t.Fatalf("expected save status 201, got %d: %s", saveResponse.Code, saveResponse.Body.String())
	}

	response := performJSONRequest(router, http.MethodGet, "/internal/llm-provider-config", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("expected internal config status 200, got %d: %s", response.Code, response.Body.String())
	}

	var body struct {
		Provider string `json:"provider"`
		BaseURL  string `json:"base_url"`
		Model    string `json:"model"`
		APIKey   string `json:"api_key"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode internal llm config: %v", err)
	}
	if body.Provider != "openai" {
		t.Fatalf("expected openai provider, got %#v", body)
	}
	if body.BaseURL != "https://api.openai.com/v1" || body.Model != "gpt-4.1" {
		t.Fatalf("expected saved base URL and model, got %#v", body)
	}
	if body.APIKey != "sk-real-agent-key" {
		t.Fatalf("expected decrypted API key for internal agent use, got %#v", body)
	}
}
