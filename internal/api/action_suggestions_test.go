package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/henry-insomniac/dev-time-server/internal/api"
	"github.com/henry-insomniac/dev-time-server/internal/db"
	"github.com/henry-insomniac/dev-time-server/internal/testsupport"
)

func TestConfirmActionSuggestionMarksDraftSucceeded(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	store := testsupport.NewMigratedStore(t, ctx)
	router := api.NewRouter(api.Dependencies{Store: store})

	repository, err := store.UpsertRepository(ctx, db.RepositoryInput{
		GitHubID: 1001,
		Owner:    "henry-insomniac",
		Name:     "dev-time",
		FullName: "henry-insomniac/dev-time",
	})
	if err != nil {
		t.Fatalf("upsert repository: %v", err)
	}
	project, err := store.EnsureProjectForRepository(ctx, repository.ID, "dev-time")
	if err != nil {
		t.Fatalf("ensure project: %v", err)
	}
	suggestion, err := store.CreateActionSuggestion(ctx, db.ActionSuggestionInput{
		ProjectID:    project.ID,
		ActionType:   "pr_comment",
		TargetRef:    "pull_request:18",
		DraftBody:    "Please fix the failing test before requesting review.",
		EvidenceRefs: []string{"event_check-run-1"},
	})
	if err != nil {
		t.Fatalf("create action suggestion: %v", err)
	}

	response := performJSONRequest(
		router,
		http.MethodPost,
		"/api/action-suggestions/"+suggestion.ID+"/confirm",
		nil,
	)
	if response.Code != http.StatusOK {
		t.Fatalf("expected confirm status 200, got %d: %s", response.Code, response.Body.String())
	}

	var body struct {
		ID           string   `json:"id"`
		Status       string   `json:"status"`
		EvidenceRefs []string `json:"evidence_refs"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode confirm response: %v", err)
	}
	if body.ID != suggestion.ID {
		t.Fatalf("expected suggestion id %q, got %q", suggestion.ID, body.ID)
	}
	if body.Status != "succeeded" {
		t.Fatalf("expected succeeded status, got %q", body.Status)
	}
	if len(body.EvidenceRefs) != 1 || body.EvidenceRefs[0] != "event_check-run-1" {
		t.Fatalf("expected evidence refs to be preserved, got %#v", body.EvidenceRefs)
	}
}
