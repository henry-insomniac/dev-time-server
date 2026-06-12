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
		Intent        string   `json:"intent"`
		TraceEvents   []struct {
			EventType    string   `json:"event_type"`
			Intent       string   `json:"intent"`
			EvidenceRefs []string `json:"evidence_refs"`
		} `json:"trace_events"`
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
	if turn.Intent != "risk_explain" {
		t.Fatalf("expected risk_explain intent, got %q", turn.Intent)
	}
	if len(turn.TraceEvents) != 1 {
		t.Fatalf("expected one trace event, got %#v", turn.TraceEvents)
	}
	if turn.TraceEvents[0].EventType != "intent_routed" || turn.TraceEvents[0].Intent != "risk_explain" {
		t.Fatalf("expected intent_routed risk_explain trace, got %#v", turn.TraceEvents[0])
	}
	if len(turn.TraceEvents[0].EvidenceRefs) != 1 || turn.TraceEvents[0].EvidenceRefs[0] != "event_check-run-conversation-1" {
		t.Fatalf("expected trace evidence refs, got %#v", turn.TraceEvents[0].EvidenceRefs)
	}
}

func TestAgentConversationTurnHandlesGreetingWithoutRiskEvidence(t *testing.T) {
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
			"message": "你好",
			"risk_assessment_id": "`+assessmentID+`"
		}`),
	)
	if turnResponse.Code != http.StatusCreated {
		t.Fatalf("expected turn status 201, got %d: %s", turnResponse.Code, turnResponse.Body.String())
	}

	var turn struct {
		AgentResponse string   `json:"agent_response"`
		EvidenceRefs  []string `json:"evidence_refs"`
		Intent        string   `json:"intent"`
	}
	if err := json.NewDecoder(turnResponse.Body).Decode(&turn); err != nil {
		t.Fatalf("decode turn response: %v", err)
	}
	if !strings.Contains(turn.AgentResponse, "你好") {
		t.Fatalf("expected greeting response, got %q", turn.AgentResponse)
	}
	if len(turn.EvidenceRefs) != 0 {
		t.Fatalf("expected greeting without risk evidence refs, got %#v", turn.EvidenceRefs)
	}
	if turn.Intent != "smalltalk" {
		t.Fatalf("expected smalltalk intent, got %q", turn.Intent)
	}
}

func TestAgentConversationTurnIntroducesItselfWithoutRiskEvidence(t *testing.T) {
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
			"message": "介绍你自己",
			"risk_assessment_id": "`+assessmentID+`"
		}`),
	)
	if turnResponse.Code != http.StatusCreated {
		t.Fatalf("expected turn status 201, got %d: %s", turnResponse.Code, turnResponse.Body.String())
	}

	var turn struct {
		AgentResponse string   `json:"agent_response"`
		EvidenceRefs  []string `json:"evidence_refs"`
		Intent        string   `json:"intent"`
	}
	if err := json.NewDecoder(turnResponse.Body).Decode(&turn); err != nil {
		t.Fatalf("decode turn response: %v", err)
	}
	if turn.Intent != "self_intro" {
		t.Fatalf("expected self_intro intent, got %q", turn.Intent)
	}
	if !strings.Contains(turn.AgentResponse, "Dev Time Agent") ||
		!strings.Contains(turn.AgentResponse, "项目风险") {
		t.Fatalf("expected self introduction response, got %q", turn.AgentResponse)
	}
	if len(turn.EvidenceRefs) != 0 {
		t.Fatalf("expected self intro without risk evidence refs, got %#v", turn.EvidenceRefs)
	}
}

func TestAgentConversationTurnClarifiesAmbiguousMessageWithoutLoadingEvidence(t *testing.T) {
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
			"message": "你怎么看",
			"risk_assessment_id": "missing-risk-assessment"
		}`),
	)
	if turnResponse.Code != http.StatusCreated {
		t.Fatalf("expected turn status 201, got %d: %s", turnResponse.Code, turnResponse.Body.String())
	}

	var turn struct {
		AgentResponse string   `json:"agent_response"`
		EvidenceRefs  []string `json:"evidence_refs"`
		Intent        string   `json:"intent"`
	}
	if err := json.NewDecoder(turnResponse.Body).Decode(&turn); err != nil {
		t.Fatalf("decode turn response: %v", err)
	}
	if turn.Intent != "clarify" {
		t.Fatalf("expected clarify intent, got %q", turn.Intent)
	}
	if strings.Contains(turn.AgentResponse, "当前风险原因") ||
		strings.Contains(turn.AgentResponse, "test failed") {
		t.Fatalf("expected clarify without risk reason, got %q", turn.AgentResponse)
	}
	if len(turn.EvidenceRefs) != 0 {
		t.Fatalf("expected clarify without evidence refs, got %#v", turn.EvidenceRefs)
	}
}

func TestAgentConversationTurnUsesConfiguredAgentRuntime(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	var runtimeRequest struct {
		ConversationID   string          `json:"conversation_id"`
		RiskAssessmentID string          `json:"risk_assessment_id"`
		Message          string          `json:"message"`
		EvidenceBundle   json.RawMessage `json:"evidence_bundle"`
	}
	intentCalls := 0
	turnCalls := 0
	agentRuntime := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/conversation/intent":
			intentCalls++
			var intentRequest struct {
				Message string `json:"message"`
			}
			if err := json.NewDecoder(request.Body).Decode(&intentRequest); err != nil {
				t.Fatalf("decode runtime intent request: %v", err)
			}
			if intentRequest.Message != "给我下一步行动计划" {
				t.Fatalf("expected runtime intent message, got %q", intentRequest.Message)
			}
			_, _ = response.Write([]byte(`{
				"intent": "action_plan",
				"confidence": 0.9,
				"requires_evidence": true,
				"requires_tool": false,
				"requires_approval": false,
				"clarifying_question": ""
			}`))
		case "/conversation/turn":
			turnCalls++
			if err := json.NewDecoder(request.Body).Decode(&runtimeRequest); err != nil {
				t.Fatalf("decode runtime request: %v", err)
			}
			_, _ = response.Write([]byte(`{
				"agent_response": "Agent Runtime 已识别为行动规划请求。",
				"evidence_refs": ["event_check-run-conversation-1"],
				"intent": "action_plan"
			}`))
		default:
			t.Fatalf("expected conversation runtime path, got %s", request.URL.Path)
		}
	}))
	defer agentRuntime.Close()

	store := testsupport.NewMigratedStore(t, ctx)
	router := api.NewRouter(api.Dependencies{
		Store:               store,
		AgentRuntimeBaseURL: agentRuntime.URL,
	})

	projectID, assessmentID := createProjectRisk(t, router)

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
			"message": "给我下一步行动计划",
			"risk_assessment_id": "`+assessmentID+`"
		}`),
	)
	if turnResponse.Code != http.StatusCreated {
		t.Fatalf("expected turn status 201, got %d: %s", turnResponse.Code, turnResponse.Body.String())
	}

	var turn struct {
		AgentResponse string   `json:"agent_response"`
		EvidenceRefs  []string `json:"evidence_refs"`
		Intent        string   `json:"intent"`
	}
	if err := json.NewDecoder(turnResponse.Body).Decode(&turn); err != nil {
		t.Fatalf("decode turn response: %v", err)
	}
	if runtimeRequest.ConversationID != conversation.ID {
		t.Fatalf("expected runtime conversation id %q, got %q", conversation.ID, runtimeRequest.ConversationID)
	}
	if intentCalls != 1 || turnCalls != 1 {
		t.Fatalf("expected one intent call and one turn call, got intent=%d turn=%d", intentCalls, turnCalls)
	}
	if runtimeRequest.RiskAssessmentID != assessmentID {
		t.Fatalf("expected runtime risk assessment id %q, got %q", assessmentID, runtimeRequest.RiskAssessmentID)
	}
	if runtimeRequest.Message != "给我下一步行动计划" {
		t.Fatalf("expected runtime message, got %q", runtimeRequest.Message)
	}
	if !strings.Contains(string(runtimeRequest.EvidenceBundle), "event_check-run-conversation-1") {
		t.Fatalf("expected runtime evidence bundle, got %s", string(runtimeRequest.EvidenceBundle))
	}
	if turn.AgentResponse != "Agent Runtime 已识别为行动规划请求。" {
		t.Fatalf("expected runtime response, got %q", turn.AgentResponse)
	}
	if turn.Intent != "action_plan" {
		t.Fatalf("expected action_plan intent, got %q", turn.Intent)
	}
	if len(turn.EvidenceRefs) != 1 || turn.EvidenceRefs[0] != "event_check-run-conversation-1" {
		t.Fatalf("expected runtime evidence refs, got %#v", turn.EvidenceRefs)
	}
}

func TestAgentConversationTurnUsesRuntimeIntentBeforeEvidence(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	intentCalls := 0
	turnCalls := 0
	agentRuntime := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/conversation/intent":
			intentCalls++
			_, _ = response.Write([]byte(`{
				"intent": "clarify",
				"confidence": 0.35,
				"requires_evidence": false,
				"requires_tool": false,
				"requires_approval": false,
				"clarifying_question": "你想让我评估当前风险、解释证据，还是生成下一步行动计划？"
			}`))
		case "/conversation/turn":
			turnCalls++
			t.Fatalf("did not expect turn runtime call for clarify intent")
		default:
			t.Fatalf("expected conversation runtime path, got %s", request.URL.Path)
		}
	}))
	defer agentRuntime.Close()

	store := testsupport.NewMigratedStore(t, ctx)
	router := api.NewRouter(api.Dependencies{
		Store:               store,
		AgentRuntimeBaseURL: agentRuntime.URL,
	})

	projectID, assessmentID := createProjectRisk(t, router)
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
			"message": "你怎么看",
			"risk_assessment_id": "missing-risk-assessment"
		}`),
	)
	if turnResponse.Code != http.StatusCreated {
		t.Fatalf("expected turn status 201, got %d: %s", turnResponse.Code, turnResponse.Body.String())
	}

	var turn struct {
		AgentResponse string   `json:"agent_response"`
		EvidenceRefs  []string `json:"evidence_refs"`
		Intent        string   `json:"intent"`
	}
	if err := json.NewDecoder(turnResponse.Body).Decode(&turn); err != nil {
		t.Fatalf("decode turn response: %v", err)
	}
	if intentCalls != 1 || turnCalls != 0 {
		t.Fatalf("expected one intent call and no turn call, got intent=%d turn=%d", intentCalls, turnCalls)
	}
	if turn.Intent != "clarify" {
		t.Fatalf("expected clarify intent, got %q", turn.Intent)
	}
	if len(turn.EvidenceRefs) != 0 {
		t.Fatalf("expected clarify without evidence refs, got %#v", turn.EvidenceRefs)
	}
}

func TestAgentConversationTurnRoutesActionPlanWithoutRuntime(t *testing.T) {
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
			"message": "给我下一步行动计划",
			"risk_assessment_id": "`+assessmentID+`"
		}`),
	)
	if turnResponse.Code != http.StatusCreated {
		t.Fatalf("expected turn status 201, got %d: %s", turnResponse.Code, turnResponse.Body.String())
	}

	var turn struct {
		AgentResponse string   `json:"agent_response"`
		EvidenceRefs  []string `json:"evidence_refs"`
		Intent        string   `json:"intent"`
	}
	if err := json.NewDecoder(turnResponse.Body).Decode(&turn); err != nil {
		t.Fatalf("decode turn response: %v", err)
	}
	if turn.Intent != "action_plan" {
		t.Fatalf("expected action_plan intent, got %q", turn.Intent)
	}
	if !strings.Contains(turn.AgentResponse, "行动计划") {
		t.Fatalf("expected action plan response, got %q", turn.AgentResponse)
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
