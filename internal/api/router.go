package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/henry-insomniac/dev-time-server/internal/buildinfo"
	"github.com/henry-insomniac/dev-time-server/internal/db"
)

type Dependencies struct {
	Store               *db.Store
	AgentRuntimeBaseURL string
}

type server struct {
	store               *db.Store
	agentRuntimeBaseURL string
}

type llmProviderPreset struct {
	Provider string
	BaseURL  string
	Model    string
}

var supportedLLMProviderPresets = []llmProviderPreset{
	{
		Provider: "openai",
		BaseURL:  "https://api.openai.com/v1",
		Model:    "gpt-4.1",
	},
	{
		Provider: "deepseek",
		BaseURL:  "https://api.deepseek.com/v1",
		Model:    "deepseek-chat",
	},
}

func NewRouter(dependencies ...Dependencies) http.Handler {
	loaded := Dependencies{}
	if len(dependencies) > 0 {
		loaded = dependencies[0]
	}

	server := server{
		store:               loaded.Store,
		agentRuntimeBaseURL: loaded.AgentRuntimeBaseURL,
	}
	router := chi.NewRouter()
	router.Use(localDevCORS)
	router.Get("/healthz", handleHealthz)
	router.Post("/api/github/repositories/import", server.handleImportRepository)
	router.Post("/api/github/webhook", server.handleGitHubWebhook)
	router.Get("/api/projects", server.handleProjects)
	router.Get("/api/projects/{projectID}/risk", server.handleProjectRisk)
	router.Get("/api/projects/{projectID}/action-suggestions", server.handleProjectActionSuggestions)
	router.Get("/api/projects/{projectID}/agent-runs", server.handleProjectAgentRuns)
	router.Get("/api/settings/llm-providers", server.handleListLLMProviders)
	router.Post("/api/settings/llm-providers", server.handleSaveLLMProvider)
	router.Get(
		"/internal/risk-assessments/{assessmentID}/evidence-bundle",
		server.handleEvidenceBundle,
	)
	router.Get("/api/projects/{projectID}/agent-conversation", server.handleAgentConversation)
	router.Post("/api/agent-conversations/{conversationID}/turns", server.handleAgentConversationTurn)
	router.Post("/api/action-suggestions/{suggestionID}/confirm", server.handleConfirmActionSuggestion)
	router.Post("/api/risk-assessments/{assessmentID}/refresh-agent", server.handleRefreshAgent)
	router.Get("/internal/llm-provider-config", server.handleInternalLLMProviderConfig)
	router.Post("/internal/agent-jobs/claim", server.handleClaimAgentJob)
	router.Post("/internal/agent-jobs/{jobID}/complete", server.handleCompleteAgentJob)
	return router
}

func localDevCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		origin := request.Header.Get("Origin")
		if origin == "http://localhost:5173" || origin == "http://127.0.0.1:5173" {
			response.Header().Set("Access-Control-Allow-Origin", origin)
			response.Header().Set("Vary", "Origin")
			response.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			response.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		}
		if request.Method == http.MethodOptions {
			response.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(response, request)
	})
}

func handleHealthz(response http.ResponseWriter, _ *http.Request) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(http.StatusOK)

	_ = json.NewEncoder(response).Encode(struct {
		Status  string `json:"status"`
		Service string `json:"service"`
	}{
		Status:  "ok",
		Service: buildinfo.ServiceName(),
	})
}

func (server server) handleImportRepository(response http.ResponseWriter, request *http.Request) {
	if server.store == nil {
		writeJSON(response, http.StatusServiceUnavailable, map[string]string{
			"error": "repository store is not configured",
		})
		return
	}

	var input struct {
		GitHubID int64  `json:"github_id"`
		Owner    string `json:"owner"`
		Name     string `json:"name"`
		FullName string `json:"full_name"`
	}
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{
			"error": "invalid JSON request body",
		})
		return
	}
	if input.GitHubID == 0 || input.Owner == "" || input.Name == "" || input.FullName == "" {
		writeJSON(response, http.StatusBadRequest, map[string]string{
			"error": "github_id, owner, name, and full_name are required",
		})
		return
	}

	imported, err := server.store.ImportRepository(request.Context(), db.RepositoryInput{
		GitHubID: input.GitHubID,
		Owner:    input.Owner,
		Name:     input.Name,
		FullName: input.FullName,
	})
	if err != nil {
		writeJSON(response, http.StatusInternalServerError, map[string]string{
			"error": "import repository failed",
		})
		return
	}

	project, err := server.store.EnsureProjectForRepository(
		request.Context(),
		imported.Repository.ID,
		input.Name,
	)
	if err != nil {
		writeJSON(response, http.StatusInternalServerError, map[string]string{
			"error": "ensure project failed",
		})
		return
	}

	status := http.StatusOK
	if imported.Created {
		status = http.StatusCreated
	}

	writeJSON(response, status, struct {
		Repository db.Repository `json:"repository"`
		Project    db.Project    `json:"project"`
	}{
		Repository: imported.Repository,
		Project:    project,
	})
}

func writeJSON(response http.ResponseWriter, status int, body any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(body)
}

func (server server) handleGitHubWebhook(response http.ResponseWriter, request *http.Request) {
	if server.store == nil {
		writeJSON(response, http.StatusServiceUnavailable, map[string]string{
			"error": "repository store is not configured",
		})
		return
	}

	deliveryID := strings.TrimSpace(request.Header.Get("X-GitHub-Delivery"))
	eventType := strings.TrimSpace(request.Header.Get("X-GitHub-Event"))
	if deliveryID == "" || eventType == "" {
		writeJSON(response, http.StatusBadRequest, map[string]string{
			"error": "X-GitHub-Delivery and X-GitHub-Event are required",
		})
		return
	}

	rawPayload, err := io.ReadAll(request.Body)
	if err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{
			"error": "read webhook body failed",
		})
		return
	}

	var payload struct {
		Repository struct {
			GitHubID int64  `json:"id"`
			Name     string `json:"name"`
			FullName string `json:"full_name"`
			Owner    struct {
				Login string `json:"login"`
			} `json:"owner"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{
			"error": "invalid JSON request body",
		})
		return
	}
	if payload.Repository.GitHubID == 0 ||
		payload.Repository.Owner.Login == "" ||
		payload.Repository.Name == "" ||
		payload.Repository.FullName == "" {
		writeJSON(response, http.StatusBadRequest, map[string]string{
			"error": "repository payload is required",
		})
		return
	}

	imported, err := server.store.ImportRepository(request.Context(), db.RepositoryInput{
		GitHubID: payload.Repository.GitHubID,
		Owner:    payload.Repository.Owner.Login,
		Name:     payload.Repository.Name,
		FullName: payload.Repository.FullName,
	})
	if err != nil {
		writeJSON(response, http.StatusInternalServerError, map[string]string{
			"error": "import repository failed",
		})
		return
	}
	if _, err := server.store.EnsureProjectForRepository(
		request.Context(),
		imported.Repository.ID,
		payload.Repository.Name,
	); err != nil {
		writeJSON(response, http.StatusInternalServerError, map[string]string{
			"error": "ensure project failed",
		})
		return
	}

	event, err := server.store.RecordGitHubEvent(request.Context(), db.GitHubEventInput{
		RepositoryID: imported.Repository.ID,
		DeliveryID:   deliveryID,
		EventType:    eventType,
		Payload:      rawPayload,
		OccurredAt:   time.Now().UTC(),
	})
	if err != nil {
		writeJSON(response, http.StatusInternalServerError, map[string]string{
			"error": "record github event failed",
		})
		return
	}

	statusCode := http.StatusAccepted
	status := "recorded"
	if event.Duplicate {
		statusCode = http.StatusOK
		status = "duplicate"
	} else if !isSupportedGitHubEvent(eventType) {
		status = "ignored"
	}

	writeJSON(response, statusCode, struct {
		EventID string `json:"event_id"`
		Status  string `json:"status"`
	}{
		EventID: event.ID,
		Status:  status,
	})
}

func isSupportedGitHubEvent(eventType string) bool {
	switch eventType {
	case "issues", "pull_request", "check_run", "push", "milestone", "release":
		return true
	default:
		return false
	}
}

func (server server) handleProjectRisk(response http.ResponseWriter, request *http.Request) {
	if server.store == nil {
		writeJSON(response, http.StatusServiceUnavailable, map[string]string{
			"error": "repository store is not configured",
		})
		return
	}

	projectID := chi.URLParam(request, "projectID")
	if projectID == "" {
		writeJSON(response, http.StatusBadRequest, map[string]string{
			"error": "project id is required",
		})
		return
	}

	projectRisk, err := server.store.AssessProjectRisk(request.Context(), projectID)
	if err != nil {
		writeJSON(response, http.StatusNotFound, map[string]string{
			"error": "project risk not found",
		})
		return
	}

	writeJSON(response, http.StatusOK, projectRisk)
}

func (server server) handleProjects(response http.ResponseWriter, request *http.Request) {
	if server.store == nil {
		writeJSON(response, http.StatusServiceUnavailable, map[string]string{
			"error": "repository store is not configured",
		})
		return
	}

	projects, err := server.store.ListProjectsByRisk(request.Context())
	if err != nil {
		writeJSON(response, http.StatusInternalServerError, map[string]string{
			"error": "list projects failed",
		})
		return
	}

	writeJSON(response, http.StatusOK, struct {
		Projects []db.ProjectSummary `json:"projects"`
	}{
		Projects: projects,
	})
}

func (server server) handleSaveLLMProvider(response http.ResponseWriter, request *http.Request) {
	if server.store == nil {
		writeJSON(response, http.StatusServiceUnavailable, map[string]string{
			"error": "repository store is not configured",
		})
		return
	}

	var input struct {
		Provider string `json:"provider"`
		BaseURL  string `json:"base_url"`
		Model    string `json:"model"`
		APIKey   string `json:"api_key"`
	}
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{
			"error": "invalid JSON request body",
		})
		return
	}
	input.Provider = strings.ToLower(strings.TrimSpace(input.Provider))
	input.BaseURL = strings.TrimSpace(input.BaseURL)
	input.Model = strings.TrimSpace(input.Model)
	input.APIKey = strings.TrimSpace(input.APIKey)
	if !isSupportedLLMProvider(input.Provider) {
		writeJSON(response, http.StatusBadRequest, map[string]string{
			"error": "provider must be openai or deepseek",
		})
		return
	}
	if input.Provider == "" || input.BaseURL == "" || input.Model == "" || input.APIKey == "" {
		writeJSON(response, http.StatusBadRequest, map[string]string{
			"error": "provider, base_url, model, and api_key are required",
		})
		return
	}

	config, err := server.store.SaveLLMProviderConfig(request.Context(), db.LLMProviderConfigInput{
		Provider: input.Provider,
		BaseURL:  input.BaseURL,
		Model:    input.Model,
		APIKey:   input.APIKey,
	})
	if err != nil {
		writeJSON(response, http.StatusInternalServerError, map[string]string{
			"error": "save llm provider failed",
		})
		return
	}

	writeJSON(response, http.StatusCreated, config)
}

func (server server) handleListLLMProviders(response http.ResponseWriter, request *http.Request) {
	if server.store == nil {
		writeJSON(response, http.StatusServiceUnavailable, map[string]string{
			"error": "repository store is not configured",
		})
		return
	}

	configs, err := server.store.ListLLMProviderConfigs(request.Context())
	if err != nil {
		writeJSON(response, http.StatusInternalServerError, map[string]string{
			"error": "list llm providers failed",
		})
		return
	}

	writeJSON(response, http.StatusOK, struct {
		Providers []db.LLMProviderConfig `json:"providers"`
	}{
		Providers: mergeSupportedLLMProviderConfigs(configs),
	})
}

func isSupportedLLMProvider(provider string) bool {
	for _, preset := range supportedLLMProviderPresets {
		if provider == preset.Provider {
			return true
		}
	}
	return false
}

func mergeSupportedLLMProviderConfigs(configs []db.LLMProviderConfig) []db.LLMProviderConfig {
	configsByProvider := map[string]db.LLMProviderConfig{}
	for _, config := range configs {
		configsByProvider[config.Provider] = config
	}

	merged := make([]db.LLMProviderConfig, 0, len(supportedLLMProviderPresets))
	for _, preset := range supportedLLMProviderPresets {
		if config, exists := configsByProvider[preset.Provider]; exists {
			merged = append(merged, config)
			continue
		}
		merged = append(merged, db.LLMProviderConfig{
			ID:         "llm_provider_" + preset.Provider,
			Provider:   preset.Provider,
			BaseURL:    preset.BaseURL,
			Model:      preset.Model,
			Configured: false,
			Enabled:    false,
		})
	}

	return merged
}

func (server server) handleInternalLLMProviderConfig(response http.ResponseWriter, request *http.Request) {
	if server.store == nil {
		writeJSON(response, http.StatusServiceUnavailable, map[string]string{
			"error": "repository store is not configured",
		})
		return
	}

	config, err := server.store.GetActiveLLMProviderConfig(request.Context())
	if err != nil {
		writeJSON(response, http.StatusNotFound, map[string]string{
			"error": "active llm provider is not configured",
		})
		return
	}

	writeJSON(response, http.StatusOK, config)
}

func (server server) handleEvidenceBundle(response http.ResponseWriter, request *http.Request) {
	if server.store == nil {
		writeJSON(response, http.StatusServiceUnavailable, map[string]string{
			"error": "repository store is not configured",
		})
		return
	}

	assessmentID := chi.URLParam(request, "assessmentID")
	if assessmentID == "" {
		writeJSON(response, http.StatusBadRequest, map[string]string{
			"error": "assessment id is required",
		})
		return
	}

	bundle, err := server.store.GetEvidenceBundle(request.Context(), assessmentID)
	if err != nil {
		writeJSON(response, http.StatusNotFound, map[string]string{
			"error": "evidence bundle not found",
		})
		return
	}

	writeJSON(response, http.StatusOK, bundle)
}

func (server server) handleAgentConversation(response http.ResponseWriter, request *http.Request) {
	if server.store == nil {
		writeJSON(response, http.StatusServiceUnavailable, map[string]string{
			"error": "repository store is not configured",
		})
		return
	}

	projectID := chi.URLParam(request, "projectID")
	riskAssessmentID := request.URL.Query().Get("risk_assessment_id")
	if projectID == "" || riskAssessmentID == "" {
		writeJSON(response, http.StatusBadRequest, map[string]string{
			"error": "project id and risk_assessment_id are required",
		})
		return
	}

	conversation, err := server.store.GetOrCreateAgentConversation(
		request.Context(),
		projectID,
		riskAssessmentID,
	)
	if err != nil {
		writeJSON(response, http.StatusInternalServerError, map[string]string{
			"error": "agent conversation failed",
		})
		return
	}

	writeJSON(response, http.StatusOK, conversation)
}

func (server server) handleAgentConversationTurn(response http.ResponseWriter, request *http.Request) {
	if server.store == nil {
		writeJSON(response, http.StatusServiceUnavailable, map[string]string{
			"error": "repository store is not configured",
		})
		return
	}

	conversationID := chi.URLParam(request, "conversationID")
	var input struct {
		Message          string `json:"message"`
		RiskAssessmentID string `json:"risk_assessment_id"`
	}
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{
			"error": "invalid JSON request body",
		})
		return
	}
	if conversationID == "" || input.Message == "" || input.RiskAssessmentID == "" {
		writeJSON(response, http.StatusBadRequest, map[string]string{
			"error": "conversation id, message, and risk_assessment_id are required",
		})
		return
	}

	agentReply, err := server.buildAgentConversationReply(
		request.Context(),
		conversationID,
		input.RiskAssessmentID,
		input.Message,
	)
	if err != nil {
		writeJSON(response, http.StatusBadGateway, map[string]string{
			"error": "agent conversation llm response failed",
		})
		return
	}

	turn, err := server.store.AddAgentConversationTurn(
		request.Context(),
		conversationID,
		input.Message,
		agentReply.AgentResponse,
		agentReply.EvidenceRefs,
		agentReply.Intent,
		agentReply.ToolCalls,
		agentReply.ApprovalRequest,
		agentReply.ReasoningTrace,
	)
	if err != nil {
		writeJSON(response, http.StatusInternalServerError, map[string]string{
			"error": "agent conversation turn failed",
		})
		return
	}

	writeJSON(response, http.StatusCreated, turn)
}

func (server server) handleConfirmActionSuggestion(response http.ResponseWriter, request *http.Request) {
	if server.store == nil {
		writeJSON(response, http.StatusServiceUnavailable, map[string]string{
			"error": "repository store is not configured",
		})
		return
	}

	suggestionID := chi.URLParam(request, "suggestionID")
	if suggestionID == "" {
		writeJSON(response, http.StatusBadRequest, map[string]string{
			"error": "suggestion id is required",
		})
		return
	}

	suggestion, err := server.store.ConfirmActionSuggestion(request.Context(), suggestionID)
	if err != nil {
		writeJSON(response, http.StatusNotFound, map[string]string{
			"error": "action suggestion not found",
		})
		return
	}

	writeJSON(response, http.StatusOK, suggestion)
}

func (server server) handleProjectActionSuggestions(response http.ResponseWriter, request *http.Request) {
	if server.store == nil {
		writeJSON(response, http.StatusServiceUnavailable, map[string]string{
			"error": "repository store is not configured",
		})
		return
	}

	projectID := chi.URLParam(request, "projectID")
	if projectID == "" {
		writeJSON(response, http.StatusBadRequest, map[string]string{
			"error": "project id is required",
		})
		return
	}

	suggestions, err := server.store.ListActionSuggestionsByProject(request.Context(), projectID)
	if err != nil {
		writeJSON(response, http.StatusInternalServerError, map[string]string{
			"error": "list action suggestions failed",
		})
		return
	}

	writeJSON(response, http.StatusOK, struct {
		ActionSuggestions []db.ActionSuggestion `json:"action_suggestions"`
	}{
		ActionSuggestions: suggestions,
	})
}

func (server server) handleProjectAgentRuns(response http.ResponseWriter, request *http.Request) {
	if server.store == nil {
		writeJSON(response, http.StatusServiceUnavailable, map[string]string{
			"error": "repository store is not configured",
		})
		return
	}

	projectID := chi.URLParam(request, "projectID")
	if projectID == "" {
		writeJSON(response, http.StatusBadRequest, map[string]string{
			"error": "project id is required",
		})
		return
	}

	runs, err := server.store.ListAgentRunsByProject(request.Context(), projectID)
	if err != nil {
		writeJSON(response, http.StatusInternalServerError, map[string]string{
			"error": "list agent runs failed",
		})
		return
	}

	writeJSON(response, http.StatusOK, struct {
		AgentRuns []db.AgentRun `json:"agent_runs"`
	}{
		AgentRuns: runs,
	})
}

func (server server) handleRefreshAgent(response http.ResponseWriter, request *http.Request) {
	if server.store == nil {
		writeJSON(response, http.StatusServiceUnavailable, map[string]string{
			"error": "repository store is not configured",
		})
		return
	}

	assessmentID := chi.URLParam(request, "assessmentID")
	var input struct {
		AgentType string `json:"agent_type"`
	}
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{
			"error": "invalid JSON request body",
		})
		return
	}
	if assessmentID == "" || input.AgentType == "" {
		writeJSON(response, http.StatusBadRequest, map[string]string{
			"error": "assessment id and agent_type are required",
		})
		return
	}

	job, err := server.store.CreateAgentJob(
		request.Context(),
		assessmentID,
		input.AgentType,
		"manual_refresh",
	)
	if err != nil {
		writeJSON(response, http.StatusNotFound, map[string]string{
			"error": "risk assessment not found",
		})
		return
	}

	writeJSON(response, http.StatusCreated, job)
}

func (server server) handleClaimAgentJob(response http.ResponseWriter, request *http.Request) {
	if server.store == nil {
		writeJSON(response, http.StatusServiceUnavailable, map[string]string{
			"error": "repository store is not configured",
		})
		return
	}

	job, found, err := server.store.ClaimNextAgentJob(request.Context())
	if err != nil {
		writeJSON(response, http.StatusInternalServerError, map[string]string{
			"error": "claim agent job failed",
		})
		return
	}
	if !found {
		response.WriteHeader(http.StatusNoContent)
		return
	}

	writeJSON(response, http.StatusOK, job)
}

func (server server) handleCompleteAgentJob(response http.ResponseWriter, request *http.Request) {
	if server.store == nil {
		writeJSON(response, http.StatusServiceUnavailable, map[string]string{
			"error": "repository store is not configured",
		})
		return
	}

	jobID := chi.URLParam(request, "jobID")
	var input struct {
		Summary           string   `json:"summary"`
		EvidenceRefs      []string `json:"evidence_refs"`
		Model             string   `json:"model"`
		PromptVersion     string   `json:"prompt_version"`
		ActionSuggestions []struct {
			ActionType   string   `json:"action_type"`
			TargetRef    string   `json:"target_ref"`
			DraftBody    string   `json:"draft_body"`
			EvidenceRefs []string `json:"evidence_refs"`
		} `json:"action_suggestions"`
	}
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{
			"error": "invalid JSON request body",
		})
		return
	}
	if jobID == "" || input.Summary == "" {
		writeJSON(response, http.StatusBadRequest, map[string]string{
			"error": "job id and summary are required",
		})
		return
	}

	actionSuggestions := make([]db.ActionSuggestionInput, 0, len(input.ActionSuggestions))
	for _, suggestion := range input.ActionSuggestions {
		if suggestion.ActionType == "" || suggestion.TargetRef == "" || suggestion.DraftBody == "" {
			writeJSON(response, http.StatusBadRequest, map[string]string{
				"error": "action suggestions require action_type, target_ref, and draft_body",
			})
			return
		}
		actionSuggestions = append(actionSuggestions, db.ActionSuggestionInput{
			ActionType:   suggestion.ActionType,
			TargetRef:    suggestion.TargetRef,
			DraftBody:    suggestion.DraftBody,
			EvidenceRefs: suggestion.EvidenceRefs,
		})
	}

	job, err := server.store.CompleteAgentJob(request.Context(), jobID, db.AgentArtifactInput{
		Summary:           input.Summary,
		EvidenceRefs:      input.EvidenceRefs,
		Model:             input.Model,
		PromptVersion:     input.PromptVersion,
		ActionSuggestions: actionSuggestions,
	})
	if err != nil {
		writeJSON(response, http.StatusNotFound, map[string]string{
			"error": "agent job not found",
		})
		return
	}

	writeJSON(response, http.StatusOK, job)
}
