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

func TestApprovalRequestConfirmRejectAndReplayAudit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	store := testsupport.NewMigratedStore(t, ctx)
	router := api.NewRouter(api.Dependencies{Store: store})
	project := createApprovalTestProject(t, ctx, store)

	createResponse := performJSONRequest(
		router,
		http.MethodPost,
		"/api/approval-requests",
		jsonBody(t, map[string]any{
			"project_id":    project.ID,
			"action_type":   "pr_comment",
			"target_ref":    "pull_request:18",
			"draft_body":    "Please fix CI before merge.",
			"risk_level":    "medium",
			"evidence_refs": []string{"event_check-run-812"},
			"before_payload": map[string]any{
				"status": "pending",
			},
			"after_payload": map[string]any{
				"comment": "Please fix CI before merge.",
			},
		}),
	)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("expected create status 201, got %d: %s", createResponse.Code, createResponse.Body.String())
	}

	var created struct {
		ID            string         `json:"id"`
		Status        string         `json:"status"`
		RiskLevel     string         `json:"risk_level"`
		EvidenceRefs  []string       `json:"evidence_refs"`
		ApprovalToken string         `json:"approval_token"`
		BeforePayload map[string]any `json:"before_payload"`
		AfterPayload  map[string]any `json:"after_payload"`
	}
	if err := json.NewDecoder(createResponse.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if created.Status != "pending" || created.ApprovalToken == "" {
		t.Fatalf("expected pending approval with token, got %#v", created)
	}
	if created.RiskLevel != "medium" || created.EvidenceRefs[0] != "event_check-run-812" {
		t.Fatalf("expected risk/evidence metadata, got %#v", created)
	}
	if created.BeforePayload["status"] != "pending" || created.AfterPayload["comment"] == "" {
		t.Fatalf("expected before/after payloads, got %#v", created)
	}

	confirmResponse := performJSONRequest(
		router,
		http.MethodPost,
		"/api/approval-requests/"+created.ID+"/confirm",
		jsonBody(t, map[string]any{"approval_token": created.ApprovalToken}),
	)
	if confirmResponse.Code != http.StatusOK {
		t.Fatalf("expected confirm status 200, got %d: %s", confirmResponse.Code, confirmResponse.Body.String())
	}
	var confirmed struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(confirmResponse.Body).Decode(&confirmed); err != nil {
		t.Fatalf("decode confirm response: %v", err)
	}
	if confirmed.Status != "confirmed" {
		t.Fatalf("expected confirmed status, got %#v", confirmed)
	}

	replayResponse := performJSONRequest(
		router,
		http.MethodPost,
		"/api/approval-requests/"+created.ID+"/confirm",
		jsonBody(t, map[string]any{"approval_token": created.ApprovalToken}),
	)
	if replayResponse.Code != http.StatusConflict {
		t.Fatalf("expected replay status 409, got %d: %s", replayResponse.Code, replayResponse.Body.String())
	}

	rejectResponse := performJSONRequest(
		router,
		http.MethodPost,
		"/api/approval-requests/"+created.ID+"/reject",
		jsonBody(t, map[string]any{"reason": "User changed direction"}),
	)
	if rejectResponse.Code != http.StatusConflict {
		t.Fatalf("expected rejecting confirmed approval to fail with 409, got %d: %s", rejectResponse.Code, rejectResponse.Body.String())
	}

	auditEvents, err := store.ApprovalAuditEvents(ctx, created.ID)
	if err != nil {
		t.Fatalf("load approval audit events: %v", err)
	}
	eventTypes := make([]string, 0, len(auditEvents))
	for _, event := range auditEvents {
		eventTypes = append(eventTypes, event.EventType)
	}
	if !containsAll(eventTypes, []string{
		"approval.created",
		"approval.confirmed",
		"approval.confirm_replay_blocked",
		"approval.reject_blocked",
	}) {
		t.Fatalf("expected approval audit trail, got %#v", eventTypes)
	}
}

func jsonBody(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal JSON body: %v", err)
	}
	return raw
}

func createApprovalTestProject(
	t *testing.T,
	ctx context.Context,
	store *db.Store,
) db.Project {
	t.Helper()
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
	return project
}

func containsAll(values []string, required []string) bool {
	seen := map[string]bool{}
	for _, value := range values {
		seen[value] = true
	}
	for _, value := range required {
		if !seen[value] {
			return false
		}
	}
	return true
}
