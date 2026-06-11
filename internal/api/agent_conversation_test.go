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

func TestAgentConversationTurnAnswersWithEvidenceRefs(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	store := testsupport.NewMigratedStore(t, ctx)
	router := api.NewRouter(api.Dependencies{Store: store})

	projectID, assessmentID := createProjectRisk(t, router)

	conversationResponse := performJSONRequest(
		router,
		http.MethodGet,
		"/api/projects/"+projectID+"/agent-conversation?risk_assessment_id="+assessmentID,
		nil,
	)
	if conversationResponse.Code != http.StatusOK {
		t.Fatalf("expected conversation status 200, got %d: %s", conversationResponse.Code, conversationResponse.Body.String())
	}

	var conversation struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(conversationResponse.Body).Decode(&conversation); err != nil {
		t.Fatalf("decode conversation response: %v", err)
	}

	turnResponse := performJSONRequest(
		router,
		http.MethodPost,
		"/api/agent-conversations/"+conversation.ID+"/turns",
		[]byte(`{
			"message": "为什么这是高风险？",
			"risk_assessment_id": "`+assessmentID+`"
		}`),
	)
	if turnResponse.Code != http.StatusCreated {
		t.Fatalf("expected turn status 201, got %d: %s", turnResponse.Code, turnResponse.Body.String())
	}

	var turn struct {
		AgentResponse string   `json:"agent_response"`
		EvidenceRefs  []string `json:"evidence_refs"`
	}
	if err := json.NewDecoder(turnResponse.Body).Decode(&turn); err != nil {
		t.Fatalf("decode turn response: %v", err)
	}
	if turn.AgentResponse == "" {
		t.Fatal("expected agent response")
	}
	if len(turn.EvidenceRefs) != 1 || turn.EvidenceRefs[0] != "event_check-run-conversation-1" {
		t.Fatalf("expected evidence refs from risk signal, got %#v", turn.EvidenceRefs)
	}
}

func createProjectRisk(t *testing.T, router http.Handler) (string, string) {
	t.Helper()

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
		"check-run-conversation-1",
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
	var risk struct {
		Assessment struct {
			ID string `json:"id"`
		} `json:"assessment"`
	}
	if err := json.NewDecoder(riskResponse.Body).Decode(&risk); err != nil {
		t.Fatalf("decode risk response: %v", err)
	}

	return projectID, risk.Assessment.ID
}
