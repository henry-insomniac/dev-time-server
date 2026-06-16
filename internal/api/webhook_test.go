package api_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/henry-insomniac/dev-time-server/internal/api"
	"github.com/henry-insomniac/dev-time-server/internal/testsupport"
)

func TestGitHubWebhookRejectsInvalidSignatureWhenSecretConfigured(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	store := testsupport.NewMigratedStore(t, ctx)
	router := api.NewRouter(api.Dependencies{
		Store: store,
		GitHubApp: api.GitHubAppConfig{
			WebhookSecret: "webhook-secret",
		},
	})

	payload := []byte(`{
		"repository": {
			"id": 1001,
			"name": "dev-time",
			"full_name": "henry-insomniac/dev-time",
			"owner": { "login": "henry-insomniac" }
		}
	}`)
	request := httptest.NewRequest(http.MethodPost, "/api/github/webhook", bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-GitHub-Delivery", "delivery-bad-signature")
	request.Header.Set("X-GitHub-Event", "pull_request")
	request.Header.Set("X-Hub-Signature-256", "sha256=bad")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected invalid signature status 401, got %d: %s", response.Code, response.Body.String())
	}
}

func TestGitHubWebhookInstallationDeletedDisablesRepositoryAnalysis(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	store := testsupport.NewMigratedStore(t, ctx)
	router := api.NewRouter(api.Dependencies{
		Store: store,
		GitHubApp: api.GitHubAppConfig{
			WebhookSecret: "webhook-secret",
		},
	})
	importProject(t, router, 1001, "dev-time-agent")

	payload := []byte(`{
		"action": "deleted",
		"repositories": [
			{
				"id": 1001,
				"name": "dev-time-agent",
				"full_name": "henry-insomniac/dev-time-agent",
				"owner": { "login": "henry-insomniac" }
			}
		]
	}`)
	response := performSignedWebhookRequest(
		router,
		"delivery-installation-deleted",
		"installation",
		payload,
		"webhook-secret",
	)
	if response.Code != http.StatusAccepted {
		t.Fatalf("expected installation webhook status 202, got %d: %s", response.Code, response.Body.String())
	}

	settingsResponse := performJSONRequest(router, http.MethodGet, "/api/settings/github", nil)
	var body struct {
		Repositories []struct {
			FullName        string `json:"full_name"`
			AnalysisEnabled bool   `json:"analysis_enabled"`
			SyncStatus      string `json:"sync_status"`
		} `json:"repositories"`
	}
	if err := json.NewDecoder(settingsResponse.Body).Decode(&body); err != nil {
		t.Fatalf("decode github settings: %v", err)
	}
	if len(body.Repositories) != 1 ||
		body.Repositories[0].FullName != "henry-insomniac/dev-time-agent" ||
		body.Repositories[0].AnalysisEnabled ||
		body.Repositories[0].SyncStatus != "failed" {
		t.Fatalf("expected installation deleted to disable repository analysis, got %#v", body.Repositories)
	}
}

func TestGitHubWebhookRecordsDeliveryIdempotently(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	store := testsupport.NewMigratedStore(t, ctx)
	router := api.NewRouter(api.Dependencies{Store: store})

	payload := []byte(`{
		"repository": {
			"id": 1001,
			"name": "dev-time",
			"full_name": "henry-insomniac/dev-time",
			"owner": { "login": "henry-insomniac" }
		},
		"action": "opened"
	}`)

	first := performWebhookRequest(router, "delivery-1", "pull_request", payload)
	if first.Code != http.StatusAccepted {
		t.Fatalf("expected status 202, got %d: %s", first.Code, first.Body.String())
	}

	second := performWebhookRequest(router, "delivery-1", "pull_request", payload)
	if second.Code != http.StatusOK {
		t.Fatalf("expected duplicate status 200, got %d: %s", second.Code, second.Body.String())
	}

	firstEvent := decodeWebhookEvent(t, first)
	secondEvent := decodeWebhookEvent(t, second)

	if firstEvent.Status != "recorded" {
		t.Fatalf("expected first status recorded, got %q", firstEvent.Status)
	}
	if secondEvent.Status != "duplicate" {
		t.Fatalf("expected second status duplicate, got %q", secondEvent.Status)
	}
	if secondEvent.EventID != firstEvent.EventID {
		t.Fatalf("expected duplicate event id %q, got %q", firstEvent.EventID, secondEvent.EventID)
	}
}

func performSignedWebhookRequest(
	handler http.Handler,
	deliveryID string,
	eventType string,
	body []byte,
	secret string,
) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/api/github/webhook", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-GitHub-Delivery", deliveryID)
	request.Header.Set("X-GitHub-Event", eventType)
	request.Header.Set("X-Hub-Signature-256", "sha256="+signWebhookBody(secret, body))
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	return response
}

func signWebhookBody(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func TestGitHubWebhookRecordsUnsupportedEventAsIgnored(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	store := testsupport.NewMigratedStore(t, ctx)
	router := api.NewRouter(api.Dependencies{Store: store})

	payload := []byte(`{
		"repository": {
			"id": 1002,
			"name": "dev-time-agent",
			"full_name": "henry-insomniac/dev-time-agent",
			"owner": { "login": "henry-insomniac" }
		}
	}`)

	response := performWebhookRequest(router, "delivery-unsupported", "meta", payload)
	if response.Code != http.StatusAccepted {
		t.Fatalf("expected status 202, got %d: %s", response.Code, response.Body.String())
	}

	event := decodeWebhookEvent(t, response)
	if event.Status != "ignored" {
		t.Fatalf("expected unsupported event to be ignored, got %q", event.Status)
	}
}

func TestGitHubWebhookMarksRepositorySyncSucceeded(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	store := testsupport.NewMigratedStore(t, ctx)
	router := api.NewRouter(api.Dependencies{Store: store})
	importProject(t, router, 1001, "dev-time-server")

	syncResponse := performJSONRequest(
		router,
		http.MethodPost,
		"/api/settings/github/repositories/repo_1001/sync",
		nil,
	)
	if syncResponse.Code != http.StatusAccepted {
		t.Fatalf("expected trigger repository sync 202, got %d: %s", syncResponse.Code, syncResponse.Body.String())
	}

	webhookResponse := performWebhookRequest(
		router,
		"repo-sync-check-run-1",
		"check_run",
		[]byte(`{
			"repository": {
				"id": 1001,
				"name": "dev-time-server",
				"full_name": "henry-insomniac/dev-time-server",
				"owner": { "login": "henry-insomniac" }
			},
			"check_run": {
				"id": 421,
				"name": "test",
				"status": "completed",
				"conclusion": "success"
			}
		}`),
	)
	if webhookResponse.Code != http.StatusAccepted {
		t.Fatalf("expected webhook status 202, got %d: %s", webhookResponse.Code, webhookResponse.Body.String())
	}

	settingsResponse := performJSONRequest(
		router,
		http.MethodGet,
		"/api/settings/github",
		nil,
	)
	if settingsResponse.Code != http.StatusOK {
		t.Fatalf("expected github settings 200, got %d: %s", settingsResponse.Code, settingsResponse.Body.String())
	}

	var body struct {
		Repositories []struct {
			ID           string  `json:"id"`
			SyncStatus   string  `json:"sync_status"`
			LastSyncedAt *string `json:"last_synced_at"`
		} `json:"repositories"`
	}
	if err := json.NewDecoder(settingsResponse.Body).Decode(&body); err != nil {
		t.Fatalf("decode github settings: %v", err)
	}
	if len(body.Repositories) != 1 ||
		body.Repositories[0].ID != "repo_1001" ||
		body.Repositories[0].SyncStatus != "succeeded" ||
		body.Repositories[0].LastSyncedAt == nil {
		t.Fatalf("expected repository sync succeeded in settings, got %#v", body.Repositories)
	}
}

func performWebhookRequest(
	handler http.Handler,
	deliveryID string,
	eventType string,
	body []byte,
) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/api/github/webhook", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-GitHub-Delivery", deliveryID)
	request.Header.Set("X-GitHub-Event", eventType)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	return response
}

func decodeWebhookEvent(t *testing.T, response *httptest.ResponseRecorder) struct {
	EventID string `json:"event_id"`
	Status  string `json:"status"`
} {
	t.Helper()

	var body struct {
		EventID string `json:"event_id"`
		Status  string `json:"status"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode webhook response: %v", err)
	}
	if body.EventID == "" {
		t.Fatal("expected event_id in webhook response")
	}

	return body
}
