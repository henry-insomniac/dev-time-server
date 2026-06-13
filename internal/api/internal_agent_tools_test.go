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

func TestInternalAgentToolsExposeProjectCIPRAndDraftSuggestion(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	store := testsupport.NewMigratedStore(t, ctx)
	router := api.NewRouter(api.Dependencies{Store: store})
	projectID, assessmentID := createProjectRisk(t, router)
	performWebhookRequest(
		router,
		"pull-request-agent-tool-1",
		"pull_request",
		[]byte(`{
			"repository": {
				"id": 1001,
				"name": "dev-time",
				"full_name": "henry-insomniac/dev-time",
				"owner": { "login": "henry-insomniac" }
			},
			"pull_request": {
				"number": 18,
				"title": "Add agent tool layer",
				"state": "open",
				"html_url": "https://github.com/henry-insomniac/dev-time/pull/18"
			}
		}`),
	)

	statusResponse := performJSONRequest(
		router,
		http.MethodGet,
		"/internal/risk-assessments/"+assessmentID+"/project-status",
		nil,
	)
	if statusResponse.Code != http.StatusOK {
		t.Fatalf("expected project status 200, got %d: %s", statusResponse.Code, statusResponse.Body.String())
	}
	var statusBody struct {
		Project struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"project"`
		Assessment struct {
			ID    string `json:"id"`
			Level string `json:"level"`
			Score int    `json:"score"`
		} `json:"assessment"`
		TopRiskReason string   `json:"top_risk_reason"`
		EvidenceRefs  []string `json:"evidence_refs"`
	}
	if err := json.NewDecoder(statusResponse.Body).Decode(&statusBody); err != nil {
		t.Fatalf("decode project status: %v", err)
	}
	if statusBody.Project.ID != projectID || statusBody.Assessment.ID != assessmentID {
		t.Fatalf("expected project and assessment identity, got %#v", statusBody)
	}
	if statusBody.Assessment.Level != "high" || statusBody.TopRiskReason == "" {
		t.Fatalf("expected high risk project status, got %#v", statusBody)
	}

	ciResponse := performJSONRequest(
		router,
		http.MethodGet,
		"/internal/risk-assessments/"+assessmentID+"/ci-checks",
		nil,
	)
	if ciResponse.Code != http.StatusOK {
		t.Fatalf("expected ci checks 200, got %d: %s", ciResponse.Code, ciResponse.Body.String())
	}
	var ciBody struct {
		Checks []struct {
			EvidenceRef string `json:"evidence_ref"`
			Name        string `json:"name"`
			Conclusion  string `json:"conclusion"`
		} `json:"checks"`
	}
	if err := json.NewDecoder(ciResponse.Body).Decode(&ciBody); err != nil {
		t.Fatalf("decode ci checks: %v", err)
	}
	if len(ciBody.Checks) != 1 ||
		ciBody.Checks[0].EvidenceRef != "event_check-run-conversation-1" ||
		ciBody.Checks[0].Conclusion != "failure" {
		t.Fatalf("expected failed check run, got %#v", ciBody.Checks)
	}

	prResponse := performJSONRequest(
		router,
		http.MethodGet,
		"/internal/risk-assessments/"+assessmentID+"/pull-requests",
		nil,
	)
	if prResponse.Code != http.StatusOK {
		t.Fatalf("expected pull requests 200, got %d: %s", prResponse.Code, prResponse.Body.String())
	}
	var prBody struct {
		PullRequests []struct {
			EvidenceRef string `json:"evidence_ref"`
			Number      int    `json:"number"`
			Title       string `json:"title"`
		} `json:"pull_requests"`
	}
	if err := json.NewDecoder(prResponse.Body).Decode(&prBody); err != nil {
		t.Fatalf("decode pull requests: %v", err)
	}
	if len(prBody.PullRequests) != 1 || prBody.PullRequests[0].Number != 18 {
		t.Fatalf("expected related PR #18, got %#v", prBody.PullRequests)
	}

	draftResponse := performJSONRequest(
		router,
		http.MethodPost,
		"/internal/action-suggestions",
		[]byte(`{
			"project_id": "`+projectID+`",
			"action_type": "pr_comment",
			"target_ref": "pull_request:18",
			"draft_body": "go test 失败阻塞交付，请先修复后再继续合并。",
			"evidence_refs": ["event_check-run-conversation-1"]
		}`),
	)
	if draftResponse.Code != http.StatusCreated {
		t.Fatalf("expected action suggestion draft 201, got %d: %s", draftResponse.Code, draftResponse.Body.String())
	}
	var draftBody struct {
		ID        string `json:"id"`
		Status    string `json:"status"`
		TargetRef string `json:"target_ref"`
	}
	if err := json.NewDecoder(draftResponse.Body).Decode(&draftBody); err != nil {
		t.Fatalf("decode draft action suggestion: %v", err)
	}
	if draftBody.ID == "" || draftBody.Status != "pending_user_confirmation" ||
		draftBody.TargetRef != "pull_request:18" {
		t.Fatalf("expected pending action suggestion draft, got %#v", draftBody)
	}
}
