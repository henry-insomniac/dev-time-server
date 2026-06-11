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

func TestEvidenceBundleIncludesRiskSignalsAndReferencedEvents(t *testing.T) {
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
	projectID := decodeProjectID(t, importResponse)

	performWebhookRequest(
		router,
		"check-run-evidence-1",
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
				"conclusion": "failure"
			}
		}`),
	)

	riskResponse := performJSONRequest(router, http.MethodGet, "/api/projects/"+projectID+"/risk", nil)
	var riskBody struct {
		Assessment struct {
			ID string `json:"id"`
		} `json:"assessment"`
	}
	if err := json.NewDecoder(riskResponse.Body).Decode(&riskBody); err != nil {
		t.Fatalf("decode risk response: %v", err)
	}

	bundleResponse := performJSONRequest(
		router,
		http.MethodGet,
		"/internal/risk-assessments/"+riskBody.Assessment.ID+"/evidence-bundle",
		nil,
	)
	if bundleResponse.Code != http.StatusOK {
		t.Fatalf("expected evidence bundle status 200, got %d: %s", bundleResponse.Code, bundleResponse.Body.String())
	}

	var bundle struct {
		Project struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"project"`
		Assessment struct {
			ID    string `json:"id"`
			Score int    `json:"score"`
		} `json:"assessment"`
		Signals []struct {
			Category     string   `json:"category"`
			EvidenceRefs []string `json:"evidence_refs"`
		} `json:"signals"`
		Events []struct {
			ID        string `json:"id"`
			EventType string `json:"event_type"`
		} `json:"events"`
		AllowedActions []string `json:"allowed_actions"`
	}
	if err := json.NewDecoder(bundleResponse.Body).Decode(&bundle); err != nil {
		t.Fatalf("decode evidence bundle response: %v", err)
	}

	if bundle.Project.ID != projectID {
		t.Fatalf("expected project id %q, got %q", projectID, bundle.Project.ID)
	}
	if bundle.Assessment.ID != riskBody.Assessment.ID {
		t.Fatalf("expected assessment id %q, got %q", riskBody.Assessment.ID, bundle.Assessment.ID)
	}
	if len(bundle.Signals) != 1 || bundle.Signals[0].EvidenceRefs[0] != "event_check-run-evidence-1" {
		t.Fatalf("expected signal evidence ref, got %#v", bundle.Signals)
	}
	if len(bundle.Events) != 1 || bundle.Events[0].ID != "event_check-run-evidence-1" {
		t.Fatalf("expected referenced event in bundle, got %#v", bundle.Events)
	}
	if len(bundle.AllowedActions) == 0 {
		t.Fatal("expected allowed actions in evidence bundle")
	}
}
