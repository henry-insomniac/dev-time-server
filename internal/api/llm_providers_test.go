package api_test

import (
	"bytes"
	"context"
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
