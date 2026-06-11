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

func TestProjectActionSuggestionsAreListedByProject(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	store := testsupport.NewMigratedStore(t, ctx)
	router := api.NewRouter(api.Dependencies{Store: store})

	repository, err := store.UpsertRepository(ctx, db.RepositoryInput{
		GitHubID: 1001,
		Owner:    "henry-insomniac",
		Name:     "dev-time-server",
		FullName: "henry-insomniac/dev-time-server",
	})
	if err != nil {
		t.Fatalf("upsert repository: %v", err)
	}
	project, err := store.EnsureProjectForRepository(ctx, repository.ID, "dev-time-server")
	if err != nil {
		t.Fatalf("ensure project: %v", err)
	}
	otherRepository, err := store.UpsertRepository(ctx, db.RepositoryInput{
		GitHubID: 1002,
		Owner:    "henry-insomniac",
		Name:     "dev-time-agent",
		FullName: "henry-insomniac/dev-time-agent",
	})
	if err != nil {
		t.Fatalf("upsert other repository: %v", err)
	}
	otherProject, err := store.EnsureProjectForRepository(ctx, otherRepository.ID, "dev-time-agent")
	if err != nil {
		t.Fatalf("ensure other project: %v", err)
	}

	suggestion, err := store.CreateActionSuggestion(ctx, db.ActionSuggestionInput{
		ProjectID:    project.ID,
		ActionType:   "pr_comment",
		TargetRef:    "pull_request:18",
		DraftBody:    "Please fix go test before review.",
		EvidenceRefs: []string{"event_check-run-1"},
	})
	if err != nil {
		t.Fatalf("create action suggestion: %v", err)
	}
	if _, err := store.CreateActionSuggestion(ctx, db.ActionSuggestionInput{
		ProjectID:    otherProject.ID,
		ActionType:   "issue_comment",
		TargetRef:    "issue:7",
		DraftBody:    "Other project suggestion.",
		EvidenceRefs: []string{"event_issue-7"},
	}); err != nil {
		t.Fatalf("create other action suggestion: %v", err)
	}

	response := performJSONRequest(
		router,
		http.MethodGet,
		"/api/projects/"+project.ID+"/action-suggestions",
		nil,
	)
	if response.Code != http.StatusOK {
		t.Fatalf("expected list status 200, got %d: %s", response.Code, response.Body.String())
	}

	var body struct {
		ActionSuggestions []struct {
			ID           string   `json:"id"`
			ProjectID    string   `json:"project_id"`
			ActionType   string   `json:"action_type"`
			Status       string   `json:"status"`
			TargetRef    string   `json:"target_ref"`
			DraftBody    string   `json:"draft_body"`
			EvidenceRefs []string `json:"evidence_refs"`
		} `json:"action_suggestions"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(body.ActionSuggestions) != 1 {
		t.Fatalf("expected one action suggestion, got %#v", body.ActionSuggestions)
	}
	listed := body.ActionSuggestions[0]
	if listed.ID != suggestion.ID || listed.ProjectID != project.ID {
		t.Fatalf("expected project suggestion %q, got %#v", suggestion.ID, listed)
	}
	if listed.Status != "pending_user_confirmation" {
		t.Fatalf("expected pending status, got %q", listed.Status)
	}
	if listed.EvidenceRefs[0] != "event_check-run-1" {
		t.Fatalf("expected evidence ref to be preserved, got %#v", listed.EvidenceRefs)
	}
}
