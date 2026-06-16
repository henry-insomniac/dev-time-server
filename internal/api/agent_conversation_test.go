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

func TestAgentConversationTurnStreamsDeltaAndFinalTurn(t *testing.T) {
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

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/agent-conversations/"+conversation.ID+"/turns/stream",
		strings.NewReader(`{
			"message": "为什么这是高风险？",
			"risk_assessment_id": "`+assessmentID+`"
		}`),
	)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("expected stream status 201, got %d: %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("expected event-stream content type, got %q", response.Header().Get("Content-Type"))
	}
	body := response.Body.String()
	if !strings.Contains(body, "event: delta\n") ||
		!strings.Contains(body, "event: turn\n") ||
		!strings.Contains(body, `"intent":"risk_explain"`) {
		t.Fatalf("expected delta and final turn events, got %q", body)
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
		Domain        string   `json:"domain"`
		Capabilities  []string `json:"capabilities"`
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

func TestAgentConversationTurnFallsBackWhenAgentRuntimeIsOffline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	store := testsupport.NewMigratedStore(t, ctx)
	router := api.NewRouter(api.Dependencies{
		Store:               store,
		AgentRuntimeBaseURL: "http://127.0.0.1:1",
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
			"message": "为什么这是高风险？",
			"risk_assessment_id": "`+assessmentID+`"
		}`),
	)
	if turnResponse.Code != http.StatusCreated {
		t.Fatalf("expected turn status 201, got %d: %s", turnResponse.Code, turnResponse.Body.String())
	}

	var turn struct {
		AgentResponse string `json:"agent_response"`
		Intent        string `json:"intent"`
	}
	if err := json.NewDecoder(turnResponse.Body).Decode(&turn); err != nil {
		t.Fatalf("decode turn response: %v", err)
	}
	if turn.Intent != "risk_explain" ||
		!strings.Contains(turn.AgentResponse, "智能分析离线") ||
		!strings.Contains(turn.AgentResponse, "当前风险原因") {
		t.Fatalf("expected offline fallback risk explanation, got %#v", turn)
	}
}

func TestAgentConversationTurnListsGitHubRepositoriesWithoutRuntime(t *testing.T) {
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
			"message": "查看我的 github 所有项目",
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
		Domain        string   `json:"domain"`
		Capabilities  []string `json:"capabilities"`
	}
	if err := json.NewDecoder(turnResponse.Body).Decode(&turn); err != nil {
		t.Fatalf("decode turn response: %v", err)
	}
	if turn.Intent != "github_repository_list" {
		t.Fatalf("expected github_repository_list intent, got %q", turn.Intent)
	}
	if turn.Domain != "github" {
		t.Fatalf("expected github domain, got %q", turn.Domain)
	}
	if len(turn.Capabilities) != 1 || turn.Capabilities[0] != "github.repos.list" {
		t.Fatalf("expected github.repos.list capability, got %#v", turn.Capabilities)
	}
	if !strings.Contains(turn.AgentResponse, "henry-insomniac/dev-time") {
		t.Fatalf("expected github repository list, got %q", turn.AgentResponse)
	}
	if strings.Contains(turn.AgentResponse, "评估当前风险") {
		t.Fatalf("expected github answer instead of risk clarification, got %q", turn.AgentResponse)
	}
	if len(turn.EvidenceRefs) != 0 {
		t.Fatalf("expected no risk evidence refs, got %#v", turn.EvidenceRefs)
	}
}

func TestAgentConversationTurnListsAllGitHubRepositoriesWithoutRuntime(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	store := testsupport.NewMigratedStore(t, ctx)
	router := api.NewRouter(api.Dependencies{Store: store})

	projectID, assessmentID := createProjectRisk(t, router)
	discoverResponse := performJSONRequest(
		router,
		http.MethodPost,
		"/api/settings/github/repositories/discover",
		[]byte(`{
			"repositories": [
				{
					"github_id": 1002,
					"owner": "henry-insomniac",
					"name": "dev-time-agent",
					"full_name": "henry-insomniac/dev-time-agent"
				}
			]
		}`),
	)
	if discoverResponse.Code != http.StatusOK {
		t.Fatalf("expected discover repositories 200, got %d: %s", discoverResponse.Code, discoverResponse.Body.String())
	}

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
			"message": "查看我的 github 项目",
			"risk_assessment_id": "`+assessmentID+`"
		}`),
	)
	if turnResponse.Code != http.StatusCreated {
		t.Fatalf("expected turn status 201, got %d: %s", turnResponse.Code, turnResponse.Body.String())
	}

	var turn struct {
		AgentResponse string   `json:"agent_response"`
		Intent        string   `json:"intent"`
		Domain        string   `json:"domain"`
		Capabilities  []string `json:"capabilities"`
	}
	if err := json.NewDecoder(turnResponse.Body).Decode(&turn); err != nil {
		t.Fatalf("decode turn response: %v", err)
	}
	if turn.Intent != "github_repository_list" {
		t.Fatalf("expected github_repository_list intent, got %q", turn.Intent)
	}
	if turn.Domain != "github" {
		t.Fatalf("expected github domain, got %q", turn.Domain)
	}
	if len(turn.Capabilities) != 1 || turn.Capabilities[0] != "github.repos.list" {
		t.Fatalf("expected github.repos.list capability, got %#v", turn.Capabilities)
	}
	if !strings.Contains(turn.AgentResponse, "henry-insomniac/dev-time") ||
		!strings.Contains(turn.AgentResponse, "henry-insomniac/dev-time-agent") {
		t.Fatalf("expected all github repositories, got %q", turn.AgentResponse)
	}
}

func TestAgentConversationTurnShowsSpecificGitHubRepositoryWithoutRuntime(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	store := testsupport.NewMigratedStore(t, ctx)
	router := api.NewRouter(api.Dependencies{Store: store})

	projectID, assessmentID := createProjectRisk(t, router)
	discoverResponse := performJSONRequest(
		router,
		http.MethodPost,
		"/api/settings/github/repositories/discover",
		[]byte(`{
			"repositories": [
				{
					"github_id": 1002,
					"owner": "henry-insomniac",
					"name": "dev-time-agent",
					"full_name": "henry-insomniac/dev-time-agent"
				}
			]
		}`),
	)
	if discoverResponse.Code != http.StatusOK {
		t.Fatalf("expected discover repositories 200, got %d: %s", discoverResponse.Code, discoverResponse.Body.String())
	}
	var discoverBody struct {
		Repositories []struct {
			ID       string `json:"id"`
			FullName string `json:"full_name"`
		} `json:"repositories"`
	}
	if err := json.NewDecoder(discoverResponse.Body).Decode(&discoverBody); err != nil {
		t.Fatalf("decode discover response: %v", err)
	}
	repositoryID := ""
	for _, repository := range discoverBody.Repositories {
		if repository.FullName == "henry-insomniac/dev-time-agent" {
			repositoryID = repository.ID
			break
		}
	}
	if repositoryID == "" {
		t.Fatalf("expected discovered dev-time-agent repository, got %#v", discoverBody.Repositories)
	}
	loadResponse := performJSONRequest(
		router,
		http.MethodPost,
		"/api/settings/github/repositories/"+repositoryID+"/load-project",
		nil,
	)
	if loadResponse.Code != http.StatusCreated {
		t.Fatalf("expected load repository 201, got %d: %s", loadResponse.Code, loadResponse.Body.String())
	}

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
			"message": "查看 dev-time-agent 项目",
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
		Domain        string   `json:"domain"`
		Entities      map[string]struct {
			FullName string `json:"full_name"`
		} `json:"entities"`
		Capabilities []string `json:"capabilities"`
	}
	if err := json.NewDecoder(turnResponse.Body).Decode(&turn); err != nil {
		t.Fatalf("decode turn response: %v", err)
	}
	if turn.Intent != "github_repository_detail" {
		t.Fatalf("expected github_repository_detail intent, got %q", turn.Intent)
	}
	if turn.Domain != "github" {
		t.Fatalf("expected github domain, got %q", turn.Domain)
	}
	if turn.Entities["repository"].FullName != "henry-insomniac/dev-time-agent" {
		t.Fatalf("expected repository entity, got %#v", turn.Entities)
	}
	if len(turn.Capabilities) != 1 || turn.Capabilities[0] != "github.repo.detail" {
		t.Fatalf("expected github.repo.detail capability, got %#v", turn.Capabilities)
	}
	if !strings.Contains(turn.AgentResponse, "henry-insomniac/dev-time-agent") ||
		!strings.Contains(turn.AgentResponse, repositoryID) ||
		!strings.Contains(turn.AgentResponse, "project_repo_1002") {
		t.Fatalf("expected specific github repository detail, got %q", turn.AgentResponse)
	}
	if strings.Contains(turn.AgentResponse, "评估当前风险") {
		t.Fatalf("expected github answer instead of risk clarification, got %q", turn.AgentResponse)
	}
	if len(turn.EvidenceRefs) != 0 {
		t.Fatalf("expected no risk evidence refs, got %#v", turn.EvidenceRefs)
	}
}

func TestAgentConversationTurnHandlesGitHubDomainQuestionsWithoutRuntime(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	store := testsupport.NewMigratedStore(t, ctx)
	router := api.NewRouter(api.Dependencies{Store: store})

	createGitHubDomainFixture(t, router)
	projectID := "project_repo_1002"
	riskResponse := performJSONRequest(router, http.MethodGet, "/api/projects/"+projectID+"/risk", nil)
	if riskResponse.Code != http.StatusOK {
		t.Fatalf("expected risk response 200, got %d: %s", riskResponse.Code, riskResponse.Body.String())
	}
	var risk struct {
		Assessment struct {
			ID string `json:"id"`
		} `json:"assessment"`
	}
	if err := json.NewDecoder(riskResponse.Body).Decode(&risk); err != nil {
		t.Fatalf("decode risk response: %v", err)
	}

	conversationResponse := performJSONRequest(
		router,
		http.MethodGet,
		"/api/projects/"+projectID+"/agent-conversation?risk_assessment_id="+risk.Assessment.ID,
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

	cases := []struct {
		name                       string
		message                    string
		expectedIntent             string
		expectedCapability         string
		expectedRepositoryFullName string
		expectedResponse           []string
	}{
		{
			name:                       "pull requests",
			message:                    "查看 dev-time-agent 的 PR",
			expectedIntent:             "github_pull_requests_list",
			expectedCapability:         "github.pull_requests.list",
			expectedRepositoryFullName: "henry-insomniac/dev-time-agent",
			expectedResponse: []string{
				"henry-insomniac/dev-time-agent",
				"PR #18",
				"Add GitHub tool layer",
			},
		},
		{
			name:                       "issues",
			message:                    "查看 dev-time-agent 的 issue",
			expectedIntent:             "github_issues_list",
			expectedCapability:         "github.issues.list",
			expectedRepositoryFullName: "henry-insomniac/dev-time-agent",
			expectedResponse: []string{
				"henry-insomniac/dev-time-agent",
				"Issue #42",
				"Add issue reader",
			},
		},
		{
			name:                       "checks",
			message:                    "查看 dev-time-agent 的 CI",
			expectedIntent:             "github_checks_list",
			expectedCapability:         "github.checks.list",
			expectedRepositoryFullName: "henry-insomniac/dev-time-agent",
			expectedResponse: []string{
				"henry-insomniac/dev-time-agent",
				"test",
				"failure",
			},
		},
		{
			name:               "auth status",
			message:            "github 授权状态",
			expectedIntent:     "github_auth_status",
			expectedCapability: "github.auth.status",
			expectedResponse: []string{
				"GitHub 已连接",
				"1 个仓库",
			},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			turnResponse := performJSONRequest(
				router,
				http.MethodPost,
				"/api/agent-conversations/"+conversation.ID+"/turns",
				[]byte(`{
					"message": "`+testCase.message+`",
					"risk_assessment_id": "`+risk.Assessment.ID+`"
				}`),
			)
			if turnResponse.Code != http.StatusCreated {
				t.Fatalf("expected turn status 201, got %d: %s", turnResponse.Code, turnResponse.Body.String())
			}

			var turn struct {
				AgentResponse string   `json:"agent_response"`
				EvidenceRefs  []string `json:"evidence_refs"`
				Intent        string   `json:"intent"`
				Domain        string   `json:"domain"`
				Entities      map[string]struct {
					FullName string `json:"full_name"`
				} `json:"entities"`
				Capabilities []string `json:"capabilities"`
			}
			if err := json.NewDecoder(turnResponse.Body).Decode(&turn); err != nil {
				t.Fatalf("decode turn response: %v", err)
			}
			if turn.Intent != testCase.expectedIntent {
				t.Fatalf("expected %s intent, got %q", testCase.expectedIntent, turn.Intent)
			}
			if turn.Domain != "github" {
				t.Fatalf("expected github domain, got %q", turn.Domain)
			}
			if len(turn.Capabilities) != 1 || turn.Capabilities[0] != testCase.expectedCapability {
				t.Fatalf("expected capability %q, got %#v", testCase.expectedCapability, turn.Capabilities)
			}
			if testCase.expectedRepositoryFullName != "" &&
				turn.Entities["repository"].FullName != testCase.expectedRepositoryFullName {
				t.Fatalf("expected repository entity %q, got %#v", testCase.expectedRepositoryFullName, turn.Entities)
			}
			for _, expected := range testCase.expectedResponse {
				if !strings.Contains(turn.AgentResponse, expected) {
					t.Fatalf("expected response to contain %q, got %q", expected, turn.AgentResponse)
				}
			}
			if strings.Contains(turn.AgentResponse, "评估当前风险") {
				t.Fatalf("expected github answer instead of risk clarification, got %q", turn.AgentResponse)
			}
		})
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

func TestAgentConversationTurnReportsProjectStatusWithoutRuntime(t *testing.T) {
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
			"message": "介绍当前状态",
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
	if turn.Intent != "project_status" {
		t.Fatalf("expected project_status intent, got %q", turn.Intent)
	}
	if !strings.Contains(turn.AgentResponse, "当前项目") ||
		!strings.Contains(turn.AgentResponse, "高风险") {
		t.Fatalf("expected project status response, got %q", turn.AgentResponse)
	}
	if len(turn.EvidenceRefs) != 1 || turn.EvidenceRefs[0] != "event_check-run-conversation-1" {
		t.Fatalf("expected evidence refs from risk signal, got %#v", turn.EvidenceRefs)
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
		ProjectID        string          `json:"project_id"`
		RiskAssessmentID string          `json:"risk_assessment_id"`
		Message          string          `json:"message"`
		EvidenceBundle   json.RawMessage `json:"evidence_bundle"`
	}
	sessionTurnCalls := 0
	agentRuntime := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		if !strings.HasPrefix(request.URL.Path, "/agent/sessions/") ||
			!strings.HasSuffix(request.URL.Path, "/turns") {
			t.Fatalf("expected conversation runtime path, got %s", request.URL.Path)
		}
		sessionTurnCalls++
		if err := json.NewDecoder(request.Body).Decode(&runtimeRequest); err != nil {
			t.Fatalf("decode runtime request: %v", err)
		}
		_, _ = response.Write([]byte(`{
			"session_id": "conversation_project_repo_1001",
			"conversation_id": "conversation_project_repo_1001",
			"user_message": "给我下一步行动计划",
			"agent_response": "Agent Runtime 已识别为行动规划请求。",
			"intent": "action_plan",
			"domain": "github",
			"entities": {"repository":{"id":"repo_1002","name":"dev-time-agent","full_name":"henry-insomniac/dev-time-agent"}},
			"capabilities": ["github.pull_requests.list"],
			"confidence": 0.9,
			"evidence_refs": ["event_check-run-conversation-1"],
			"current_node": "planner",
			"trace_events": [{"node":"planner","title":"生成行动计划"}],
			"tool_calls": [],
			"approval_request": null
		}`))
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
		Domain        string   `json:"domain"`
		Entities      map[string]struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			FullName string `json:"full_name"`
		} `json:"entities"`
		Capabilities []string `json:"capabilities"`
	}
	if err := json.NewDecoder(turnResponse.Body).Decode(&turn); err != nil {
		t.Fatalf("decode turn response: %v", err)
	}
	if runtimeRequest.ConversationID != conversation.ID {
		t.Fatalf("expected runtime conversation id %q, got %q", conversation.ID, runtimeRequest.ConversationID)
	}
	if sessionTurnCalls != 1 {
		t.Fatalf("expected one session turn call when runtime returns evidence refs, got %d", sessionTurnCalls)
	}
	if runtimeRequest.RiskAssessmentID != assessmentID {
		t.Fatalf("expected runtime risk assessment id %q, got %q", assessmentID, runtimeRequest.RiskAssessmentID)
	}
	if runtimeRequest.ProjectID != projectID {
		t.Fatalf("expected runtime project id %q, got %q", projectID, runtimeRequest.ProjectID)
	}
	if runtimeRequest.Message != "给我下一步行动计划" {
		t.Fatalf("expected runtime message, got %q", runtimeRequest.Message)
	}
	if len(runtimeRequest.EvidenceBundle) != 0 {
		t.Fatalf("expected no server-provided evidence bundle after runtime tool response, got %s", string(runtimeRequest.EvidenceBundle))
	}
	if turn.AgentResponse != "Agent Runtime 已识别为行动规划请求。" {
		t.Fatalf("expected runtime response, got %q", turn.AgentResponse)
	}
	if turn.Intent != "action_plan" {
		t.Fatalf("expected action_plan intent, got %q", turn.Intent)
	}
	if turn.Domain != "github" {
		t.Fatalf("expected runtime domain, got %q", turn.Domain)
	}
	if turn.Entities["repository"].FullName != "henry-insomniac/dev-time-agent" {
		t.Fatalf("expected runtime entities, got %#v", turn.Entities)
	}
	if len(turn.Capabilities) != 1 || turn.Capabilities[0] != "github.pull_requests.list" {
		t.Fatalf("expected runtime capabilities, got %#v", turn.Capabilities)
	}
	if len(turn.EvidenceRefs) != 1 || turn.EvidenceRefs[0] != "event_check-run-conversation-1" {
		t.Fatalf("expected runtime evidence refs, got %#v", turn.EvidenceRefs)
	}
}

func TestAgentConversationTurnReturnsRuntimeReasoningTrace(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	agentRuntime := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{
			"session_id": "conversation_project_repo_1001",
			"conversation_id": "conversation_project_repo_1001",
			"user_message": "基于当前风险生成 PR 评论草稿",
			"agent_response": "已生成 PR 评论草稿，请确认后发布。",
			"intent": "draft_pr_comment",
			"confidence": 0.91,
			"evidence_refs": ["event_check-run-conversation-1"],
			"current_node": "response_verifier",
			"trace_events": [{"node":"approval_gate","title":"等待用户确认写操作"}],
			"tool_calls": [
				{
					"name": "risk_evidence.read",
					"status": "succeeded",
					"input": {"risk_assessment_id": "risk_123"},
					"evidence_refs": ["event_check-run-conversation-1"]
				}
			],
			"approval_request": {
				"status": "pending",
				"reason": "LLM 生成了需要用户确认的写操作。",
				"actions": [
					{
						"action_type": "pr_comment",
						"target_ref": "pull_request:18",
						"draft_body": "go test 失败阻塞交付，请先修复后再合并。",
						"evidence_refs": ["event_check-run-conversation-1"],
						"required_permission": "pull_request:write"
					}
				]
			},
			"reasoning_trace": [
				{
					"stage": "planning",
					"title": "识别用户意图",
					"summary": "用户要求生成 PR 评论草稿。",
					"status": "completed",
					"confidence": 0.91,
					"evidence_refs": [],
					"tool_call": null
				},
				{
					"stage": "approval",
					"title": "等待用户确认写操作",
					"summary": "写操作不会自动执行。",
					"status": "completed",
					"confidence": null,
					"evidence_refs": ["event_check-run-conversation-1"],
					"tool_call": null
				}
			]
		}`))
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
			"message": "基于当前风险生成 PR 评论草稿",
			"risk_assessment_id": "`+assessmentID+`"
		}`),
	)
	if turnResponse.Code != http.StatusCreated {
		t.Fatalf("expected turn status 201, got %d: %s", turnResponse.Code, turnResponse.Body.String())
	}

	var turn struct {
		ReasoningTrace []struct {
			Stage        string   `json:"stage"`
			Title        string   `json:"title"`
			Summary      string   `json:"summary"`
			EvidenceRefs []string `json:"evidence_refs"`
		} `json:"reasoning_trace"`
		ToolCalls []struct {
			Name         string   `json:"name"`
			Status       string   `json:"status"`
			EvidenceRefs []string `json:"evidence_refs"`
		} `json:"tool_calls"`
		ApprovalRequest struct {
			Status  string `json:"status"`
			Actions []struct {
				ActionType string `json:"action_type"`
				TargetRef  string `json:"target_ref"`
			} `json:"actions"`
		} `json:"approval_request"`
	}
	if err := json.NewDecoder(turnResponse.Body).Decode(&turn); err != nil {
		t.Fatalf("decode turn response: %v", err)
	}
	if len(turn.ReasoningTrace) != 2 {
		t.Fatalf("expected runtime reasoning trace, got %#v", turn.ReasoningTrace)
	}
	if turn.ReasoningTrace[0].Stage != "planning" ||
		turn.ReasoningTrace[0].Title != "识别用户意图" {
		t.Fatalf("expected planning reasoning step, got %#v", turn.ReasoningTrace[0])
	}
	if len(turn.ToolCalls) != 1 || turn.ToolCalls[0].Name != "risk_evidence.read" {
		t.Fatalf("expected runtime tool call, got %#v", turn.ToolCalls)
	}
	if turn.ApprovalRequest.Status != "pending" ||
		len(turn.ApprovalRequest.Actions) != 1 ||
		turn.ApprovalRequest.Actions[0].TargetRef != "pull_request:18" {
		t.Fatalf("expected pending approval request, got %#v", turn.ApprovalRequest)
	}
}

func TestAgentConversationTurnUsesRuntimeIntentBeforeEvidence(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	sessionTurnCalls := 0
	agentRuntime := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		if !strings.HasPrefix(request.URL.Path, "/agent/sessions/") ||
			!strings.HasSuffix(request.URL.Path, "/turns") {
			t.Fatalf("expected conversation runtime path, got %s", request.URL.Path)
		}
		sessionTurnCalls++
		_, _ = response.Write([]byte(`{
				"session_id": "conversation_project_repo_1001",
				"conversation_id": "conversation_project_repo_1001",
				"user_message": "你怎么看",
				"agent_response": "你想让我评估当前风险、解释证据，还是生成下一步行动计划？",
				"intent": "clarify",
				"confidence": 0.35,
				"evidence_refs": [],
				"current_node": "clarify_responder",
				"trace_events": [{"node":"clarify_responder","title":"生成澄清问题"}],
				"tool_calls": [],
				"approval_request": null
			}`))
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
	if sessionTurnCalls != 1 {
		t.Fatalf("expected one session turn call, got %d", sessionTurnCalls)
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

func createGitHubDomainFixture(t *testing.T, router http.Handler) {
	t.Helper()

	webhookResponse := performWebhookRequest(
		router,
		"domain-check-run-1",
		"check_run",
		[]byte(`{
			"repository": {
				"id": 1002,
				"name": "dev-time-agent",
				"full_name": "henry-insomniac/dev-time-agent",
				"owner": { "login": "henry-insomniac" }
			},
			"check_run": {
				"id": 421,
				"name": "test",
				"status": "completed",
				"conclusion": "failure",
				"html_url": "https://github.test/henry-insomniac/dev-time-agent/actions/runs/421"
			}
		}`),
	)
	if webhookResponse.Code != http.StatusAccepted {
		t.Fatalf("expected check_run webhook 202, got %d: %s", webhookResponse.Code, webhookResponse.Body.String())
	}

	webhookResponse = performWebhookRequest(
		router,
		"domain-pull-request-1",
		"pull_request",
		[]byte(`{
			"repository": {
				"id": 1002,
				"name": "dev-time-agent",
				"full_name": "henry-insomniac/dev-time-agent",
				"owner": { "login": "henry-insomniac" }
			},
			"pull_request": {
				"id": 18,
				"number": 18,
				"title": "Add GitHub tool layer",
				"state": "open",
				"html_url": "https://github.test/henry-insomniac/dev-time-agent/pull/18"
			}
		}`),
	)
	if webhookResponse.Code != http.StatusAccepted {
		t.Fatalf("expected pull_request webhook 202, got %d: %s", webhookResponse.Code, webhookResponse.Body.String())
	}

	webhookResponse = performWebhookRequest(
		router,
		"domain-issue-1",
		"issues",
		[]byte(`{
			"repository": {
				"id": 1002,
				"name": "dev-time-agent",
				"full_name": "henry-insomniac/dev-time-agent",
				"owner": { "login": "henry-insomniac" }
			},
			"issue": {
				"id": 42,
				"number": 42,
				"title": "Add issue reader",
				"state": "open",
				"html_url": "https://github.test/henry-insomniac/dev-time-agent/issues/42"
			}
		}`),
	)
	if webhookResponse.Code != http.StatusAccepted {
		t.Fatalf("expected issues webhook 202, got %d: %s", webhookResponse.Code, webhookResponse.Body.String())
	}
}
