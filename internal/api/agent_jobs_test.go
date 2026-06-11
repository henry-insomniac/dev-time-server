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

func TestAgentJobCanBeCreatedClaimedAndCompleted(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	store := testsupport.NewMigratedStore(t, ctx)
	router := api.NewRouter(api.Dependencies{Store: store})

	_, assessmentID := createProjectRisk(t, router)

	refreshResponse := performJSONRequest(
		router,
		http.MethodPost,
		"/api/risk-assessments/"+assessmentID+"/refresh-agent",
		[]byte(`{"agent_type":"risk_scout"}`),
	)
	if refreshResponse.Code != http.StatusCreated {
		t.Fatalf("expected refresh status 201, got %d: %s", refreshResponse.Code, refreshResponse.Body.String())
	}

	claimResponse := performJSONRequest(router, http.MethodPost, "/internal/agent-jobs/claim", nil)
	if claimResponse.Code != http.StatusOK {
		t.Fatalf("expected claim status 200, got %d: %s", claimResponse.Code, claimResponse.Body.String())
	}

	var claimed struct {
		JobID            string `json:"job_id"`
		ProjectID        string `json:"project_id"`
		RiskAssessmentID string `json:"risk_assessment_id"`
		AgentType        string `json:"agent_type"`
	}
	if err := json.NewDecoder(claimResponse.Body).Decode(&claimed); err != nil {
		t.Fatalf("decode claimed job: %v", err)
	}
	if claimed.JobID == "" || claimed.AgentType != "risk_scout" {
		t.Fatalf("expected claimed risk_scout job, got %#v", claimed)
	}

	completeResponse := performJSONRequest(
		router,
		http.MethodPost,
		"/internal/agent-jobs/"+claimed.JobID+"/complete",
		[]byte(`{
			"summary": "dev-time is high risk because test failed.",
			"evidence_refs": ["event_check-run-conversation-1"],
			"model": "deterministic",
			"prompt_version": "risk-scout@v1",
			"action_suggestions": [
				{
					"action_type": "pr_comment",
					"target_ref": "pull_request:18",
					"draft_body": "Please fix the failing test before requesting review.",
					"evidence_refs": ["event_check-run-conversation-1"]
				}
			]
		}`),
	)
	if completeResponse.Code != http.StatusOK {
		t.Fatalf("expected complete status 200, got %d: %s", completeResponse.Code, completeResponse.Body.String())
	}

	var completed struct {
		Status              string   `json:"status"`
		ActionSuggestionIDs []string `json:"action_suggestion_ids"`
	}
	if err := json.NewDecoder(completeResponse.Body).Decode(&completed); err != nil {
		t.Fatalf("decode completed job: %v", err)
	}
	if completed.Status != "succeeded" {
		t.Fatalf("expected succeeded job status, got %q", completed.Status)
	}
	if len(completed.ActionSuggestionIDs) != 1 {
		t.Fatalf("expected one action suggestion id, got %#v", completed.ActionSuggestionIDs)
	}

	confirmResponse := performJSONRequest(
		router,
		http.MethodPost,
		"/api/action-suggestions/"+completed.ActionSuggestionIDs[0]+"/confirm",
		nil,
	)
	if confirmResponse.Code != http.StatusOK {
		t.Fatalf("expected confirm status 200, got %d: %s", confirmResponse.Code, confirmResponse.Body.String())
	}

	runsResponse := performJSONRequest(
		router,
		http.MethodGet,
		"/api/projects/"+claimed.ProjectID+"/agent-runs",
		nil,
	)
	if runsResponse.Code != http.StatusOK {
		t.Fatalf("expected agent runs status 200, got %d: %s", runsResponse.Code, runsResponse.Body.String())
	}

	var runsBody struct {
		AgentRuns []struct {
			AgentType string `json:"agent_type"`
			Status    string `json:"status"`
			Summary   string `json:"summary"`
			Steps     []struct {
				StepType     string   `json:"step_type"`
				Title        string   `json:"title"`
				EvidenceRefs []string `json:"evidence_refs"`
			} `json:"steps"`
		} `json:"agent_runs"`
	}
	if err := json.NewDecoder(runsResponse.Body).Decode(&runsBody); err != nil {
		t.Fatalf("decode agent runs response: %v", err)
	}
	if len(runsBody.AgentRuns) != 1 {
		t.Fatalf("expected one agent run, got %#v", runsBody.AgentRuns)
	}
	run := runsBody.AgentRuns[0]
	if run.AgentType != "risk_scout" || run.Status != "succeeded" {
		t.Fatalf("expected succeeded risk_scout run, got %#v", run)
	}
	if run.Summary != "dev-time is high risk because test failed." {
		t.Fatalf("expected agent run summary to match artifact, got %q", run.Summary)
	}
	if len(run.Steps) < 3 {
		t.Fatalf("expected queued, running, and completed steps, got %#v", run.Steps)
	}
	if run.Steps[len(run.Steps)-1].StepType != "completed" {
		t.Fatalf("expected last step completed, got %#v", run.Steps)
	}
	if run.Steps[len(run.Steps)-1].EvidenceRefs[0] != "event_check-run-conversation-1" {
		t.Fatalf("expected completed step evidence refs, got %#v", run.Steps[len(run.Steps)-1].EvidenceRefs)
	}
}
