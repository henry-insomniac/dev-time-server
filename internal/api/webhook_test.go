package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/henry-insomniac/dev-time-server/internal/api"
	"github.com/henry-insomniac/dev-time-server/internal/testsupport"
)

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
