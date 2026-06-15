package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/henry-insomniac/dev-time-server/internal/api"
	"github.com/henry-insomniac/dev-time-server/internal/testsupport"
)

func TestProjectRiskReportsFailedCheckRunAsBlockingRisk(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	store := testsupport.NewMigratedStore(t, ctx)
	router := api.NewRouter(api.Dependencies{Store: store})

	importResponse := performJSONRequest(
		router,
		http.MethodPost,
		"/api/github/repositories/import",
		[]byte(`{
			"github_id": 1001,
			"owner": "henry-insomniac",
			"name": "dev-time",
			"full_name": "henry-insomniac/dev-time"
		}`),
	)
	if importResponse.Code != http.StatusCreated {
		t.Fatalf("expected import status 201, got %d: %s", importResponse.Code, importResponse.Body.String())
	}

	projectID := decodeProjectID(t, importResponse)
	webhookResponse := performWebhookRequest(
		router,
		"check-run-1",
		"check_run",
		[]byte(`{
			"repository": {
				"id": 1001,
				"name": "dev-time",
				"full_name": "henry-insomniac/dev-time",
				"owner": { "login": "henry-insomniac" }
			},
			"check_run": {
				"id": 421,
				"name": "test",
				"status": "completed",
				"conclusion": "failure",
				"html_url": "https://github.com/henry-insomniac/dev-time/actions/runs/421"
			}
		}`),
	)
	if webhookResponse.Code != http.StatusAccepted {
		t.Fatalf("expected webhook status 202, got %d: %s", webhookResponse.Code, webhookResponse.Body.String())
	}

	riskResponse := performJSONRequest(router, http.MethodGet, "/api/projects/"+projectID+"/risk", nil)
	if riskResponse.Code != http.StatusOK {
		t.Fatalf("expected risk status 200, got %d: %s", riskResponse.Code, riskResponse.Body.String())
	}

	var body struct {
		Assessment struct {
			Score int    `json:"score"`
			Level string `json:"level"`
		} `json:"assessment"`
		Signals []struct {
			Category     string   `json:"category"`
			Reason       string   `json:"reason"`
			EvidenceRefs []string `json:"evidence_refs"`
		} `json:"signals"`
	}
	if err := json.NewDecoder(riskResponse.Body).Decode(&body); err != nil {
		t.Fatalf("decode risk response: %v", err)
	}

	if body.Assessment.Score < 56 {
		t.Fatalf("expected high risk score, got %d", body.Assessment.Score)
	}
	if body.Assessment.Level != "high" {
		t.Fatalf("expected high risk level, got %q", body.Assessment.Level)
	}
	if len(body.Signals) != 1 {
		t.Fatalf("expected one risk signal, got %d", len(body.Signals))
	}
	if body.Signals[0].Category != "blocked" {
		t.Fatalf("expected blocked category, got %q", body.Signals[0].Category)
	}
	if len(body.Signals[0].EvidenceRefs) != 1 || body.Signals[0].EvidenceRefs[0] != "event_check-run-1" {
		t.Fatalf("expected failed check run evidence ref, got %#v", body.Signals[0].EvidenceRefs)
	}
}

func TestProjectRiskExcludesDisabledAnalysisRepository(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	store := testsupport.NewMigratedStore(t, ctx)
	router := api.NewRouter(api.Dependencies{Store: store})

	importResponse := performJSONRequest(
		router,
		http.MethodPost,
		"/api/github/repositories/import",
		[]byte(`{
			"github_id": 1001,
			"owner": "henry-insomniac",
			"name": "dev-time",
			"full_name": "henry-insomniac/dev-time"
		}`),
	)
	if importResponse.Code != http.StatusCreated {
		t.Fatalf("expected import status 201, got %d: %s", importResponse.Code, importResponse.Body.String())
	}
	projectID := decodeProjectID(t, importResponse)

	toggleResponse := performJSONRequest(
		router,
		http.MethodPatch,
		"/api/settings/github/repositories/repo_1001/analysis",
		[]byte(`{"analysis_enabled": false}`),
	)
	if toggleResponse.Code != http.StatusOK {
		t.Fatalf("expected toggle analysis status 200, got %d: %s", toggleResponse.Code, toggleResponse.Body.String())
	}

	riskResponse := performJSONRequest(router, http.MethodGet, "/api/projects/"+projectID+"/risk", nil)
	if riskResponse.Code != http.StatusNotFound {
		t.Fatalf("expected disabled repository risk status 404, got %d: %s", riskResponse.Code, riskResponse.Body.String())
	}
}
