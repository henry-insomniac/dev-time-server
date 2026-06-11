package api_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestAgentConversationTurnUsesConfiguredLLMProvider(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	var llmRequest struct {
		Authorization string
		Payload       struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
	}
	llmServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/chat/completions" {
			t.Fatalf("expected chat completions path, got %s", request.URL.Path)
		}
		llmRequest.Authorization = request.Header.Get("Authorization")
		rawBody, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("read llm request: %v", err)
		}
		if err := json.Unmarshal(rawBody, &llmRequest.Payload); err != nil {
			t.Fatalf("decode llm request: %v", err)
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{
			"choices": [
				{
					"message": {
						"content": "这是 DeepSeek 基于失败 check_run 给出的中文风险解释。"
					}
				}
			]
		}`))
	}))
	defer llmServer.Close()

	store := testsupport.NewMigratedStore(t, ctx)
	router := api.NewRouter(api.Dependencies{Store: store})

	projectID, assessmentID := createProjectRisk(t, router)
	saveResponse := performJSONRequest(
		router,
		http.MethodPost,
		"/api/settings/llm-providers",
		[]byte(`{
			"provider": "deepseek",
			"base_url": "`+llmServer.URL+`",
			"model": "deepseek-chat",
			"api_key": "sk-deepseek-test"
		}`),
	)
	if saveResponse.Code != http.StatusCreated {
		t.Fatalf("expected save provider status 201, got %d: %s", saveResponse.Code, saveResponse.Body.String())
	}

	conversationResponse := performJSONRequest(
		router,
		http.MethodGet,
		"/api/projects/"+projectID+"/agent-conversation?risk_assessment_id="+assessmentID,
		nil,
	)
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
		AgentResponse string `json:"agent_response"`
	}
	if err := json.NewDecoder(turnResponse.Body).Decode(&turn); err != nil {
		t.Fatalf("decode turn response: %v", err)
	}
	if turn.AgentResponse != "这是 DeepSeek 基于失败 check_run 给出的中文风险解释。" {
		t.Fatalf("expected llm response, got %q", turn.AgentResponse)
	}
	if llmRequest.Authorization != "Bearer sk-deepseek-test" {
		t.Fatalf("expected llm authorization header, got %q", llmRequest.Authorization)
	}
	if llmRequest.Payload.Model != "deepseek-chat" {
		t.Fatalf("expected deepseek model, got %q", llmRequest.Payload.Model)
	}
	if len(llmRequest.Payload.Messages) != 2 {
		t.Fatalf("expected system and user messages, got %#v", llmRequest.Payload.Messages)
	}
	if !strings.Contains(llmRequest.Payload.Messages[1].Content, "为什么这是高风险？") ||
		!strings.Contains(llmRequest.Payload.Messages[1].Content, "event_check-run-conversation-1") {
		t.Fatalf("expected question and evidence in prompt, got %q", llmRequest.Payload.Messages[1].Content)
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
