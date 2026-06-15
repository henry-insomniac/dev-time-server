package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/henry-insomniac/dev-time-server/internal/db"
)

var llmHTTPClient = &http.Client{Timeout: 30 * time.Second}

type conversationIntentClassification struct {
	Intent             string  `json:"intent"`
	Confidence         float64 `json:"confidence"`
	RequiresEvidence   bool    `json:"requires_evidence"`
	RequiresTool       bool    `json:"requires_tool"`
	RequiresApproval   bool    `json:"requires_approval"`
	RepositoryQuery    string  `json:"repository_query"`
	ClarifyingQuestion string  `json:"clarifying_question"`
}

type agentConversationReply struct {
	AgentResponse   string
	EvidenceRefs    []string
	Intent          string
	Domain          string
	Entities        map[string]any
	Capabilities    []string
	ToolCalls       []map[string]any
	ApprovalRequest map[string]any
	ReasoningTrace  []db.ReasoningTraceStep
}

func (server server) buildAgentConversationReply(
	ctx context.Context,
	conversationID string,
	riskAssessmentID string,
	userMessage string,
) (agentConversationReply, error) {
	if reply, handled, err := server.githubDomainConversationReply(ctx, userMessage); handled || err != nil {
		return reply, err
	}

	if strings.TrimSpace(server.agentRuntimeBaseURL) != "" {
		projectID, _ := server.store.ProjectIDForRiskAssessment(
			ctx,
			riskAssessmentID,
		)
		reply, err := requestAgentRuntimeSessionTurn(
			ctx,
			server.agentRuntimeBaseURL,
			conversationID,
			projectID,
			riskAssessmentID,
			userMessage,
			nil,
		)
		if err == nil {
			if !intentRequiresEvidence(reply.Intent) || len(reply.EvidenceRefs) > 0 {
				return reply, nil
			}
			bundle, bundleErr := server.store.GetEvidenceBundle(ctx, riskAssessmentID)
			if bundleErr != nil {
				return agentConversationReply{}, bundleErr
			}
			return requestAgentRuntimeSessionTurn(
				ctx,
				server.agentRuntimeBaseURL,
				conversationID,
				bundle.Assessment.ProjectID,
				riskAssessmentID,
				userMessage,
				&bundle,
			)
		}
	}

	classification := classifyConversationIntent(userMessage)

	if classification.Intent == "github_repository_list" {
		return server.githubRepositoryAccessConversationReply(ctx, userMessage)
	}
	if classification.Intent == "github_repository_detail" {
		return server.githubRepositoryDetailConversationReply(ctx, userMessage)
	}

	if !classification.RequiresEvidence {
		return agentConversationReply{
			AgentResponse: replyWithoutEvidence(classification),
			Intent:        classification.Intent,
		}, nil
	}

	bundle, err := server.store.GetEvidenceBundle(ctx, riskAssessmentID)
	if err != nil {
		return agentConversationReply{}, err
	}
	evidenceRefs := evidenceRefsFromSignals(bundle.Signals)

	if classification.Intent == "project_status" {
		return agentConversationReply{
			AgentResponse: fallbackProjectStatusReply(bundle),
			EvidenceRefs:  evidenceRefs,
			Intent:        "project_status",
		}, nil
	}
	if classification.Intent == "action_plan" {
		return agentConversationReply{
			AgentResponse: fallbackActionPlanReply(bundle),
			EvidenceRefs:  evidenceRefs,
			Intent:        "action_plan",
		}, nil
	}

	config, err := server.store.GetActiveLLMProviderConfig(ctx)
	if errors.Is(err, db.ErrNotFound) {
		return agentConversationReply{
			AgentResponse: fallbackAgentConversationReply(bundle),
			EvidenceRefs:  evidenceRefs,
			Intent:        "risk_explain",
		}, nil
	}
	if err != nil {
		return agentConversationReply{}, err
	}

	reply, err := requestLLMConversationReply(ctx, config, bundle, userMessage)
	if err != nil {
		return agentConversationReply{}, err
	}
	return agentConversationReply{
		AgentResponse: reply,
		EvidenceRefs:  evidenceRefs,
		Intent:        "risk_explain",
	}, nil
}

func requestAgentRuntimeSessionTurn(
	ctx context.Context,
	baseURL string,
	conversationID string,
	projectID string,
	riskAssessmentID string,
	userMessage string,
	bundle *db.EvidenceBundle,
) (agentConversationReply, error) {
	payload := map[string]any{
		"conversation_id":    conversationID,
		"project_id":         projectID,
		"risk_assessment_id": riskAssessmentID,
		"message":            userMessage,
	}
	if bundle != nil {
		payload["evidence_bundle"] = bundle
	}
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return agentConversationReply{}, fmt.Errorf("marshal agent runtime session payload: %w", err)
	}

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		strings.TrimRight(baseURL, "/")+"/agent/sessions/"+conversationID+"/turns",
		bytes.NewReader(rawPayload),
	)
	if err != nil {
		return agentConversationReply{}, fmt.Errorf("create agent runtime session request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")

	response, err := llmHTTPClient.Do(request)
	if err != nil {
		return agentConversationReply{}, fmt.Errorf("call agent runtime session: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return agentConversationReply{}, fmt.Errorf("agent runtime session returned status %d", response.StatusCode)
	}

	var body struct {
		AgentResponse   string                  `json:"agent_response"`
		Intent          string                  `json:"intent"`
		Domain          string                  `json:"domain"`
		Entities        map[string]any          `json:"entities"`
		Capabilities    []string                `json:"capabilities"`
		EvidenceRefs    []string                `json:"evidence_refs"`
		ToolCalls       []map[string]any        `json:"tool_calls"`
		ApprovalRequest map[string]any          `json:"approval_request"`
		ReasoningTrace  []db.ReasoningTraceStep `json:"reasoning_trace"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		return agentConversationReply{}, fmt.Errorf("decode agent runtime session response: %w", err)
	}
	if strings.TrimSpace(body.AgentResponse) == "" || strings.TrimSpace(body.Intent) == "" {
		return agentConversationReply{}, fmt.Errorf("agent runtime session response is incomplete")
	}

	return agentConversationReply{
		AgentResponse:   strings.TrimSpace(body.AgentResponse),
		EvidenceRefs:    body.EvidenceRefs,
		Intent:          strings.TrimSpace(body.Intent),
		Domain:          strings.TrimSpace(body.Domain),
		Entities:        body.Entities,
		Capabilities:    body.Capabilities,
		ToolCalls:       body.ToolCalls,
		ApprovalRequest: body.ApprovalRequest,
		ReasoningTrace:  body.ReasoningTrace,
	}, nil
}

func requestLLMConversationReply(
	ctx context.Context,
	config db.ActiveLLMProviderConfig,
	bundle db.EvidenceBundle,
	userMessage string,
) (string, error) {
	payload := map[string]any{
		"model": config.Model,
		"messages": []map[string]string{
			{
				"role":    "system",
				"content": "你是 Dev Time 的项目风险 Agent。只能基于证据包回答，使用中文，结论要短，并引用证据含义。",
			},
			{
				"role":    "user",
				"content": conversationPrompt(bundle, userMessage),
			},
		},
		"temperature": 0.2,
	}
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal llm conversation payload: %w", err)
	}

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		strings.TrimRight(config.BaseURL, "/")+"/chat/completions",
		bytes.NewReader(rawPayload),
	)
	if err != nil {
		return "", fmt.Errorf("create llm conversation request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+config.APIKey)
	request.Header.Set("Content-Type", "application/json")

	response, err := llmHTTPClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("call llm conversation provider: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("llm conversation provider returned status %d", response.StatusCode)
	}

	var body struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("decode llm conversation response: %w", err)
	}
	if len(body.Choices) == 0 || strings.TrimSpace(body.Choices[0].Message.Content) == "" {
		return "", fmt.Errorf("llm conversation response is empty")
	}

	return strings.TrimSpace(body.Choices[0].Message.Content), nil
}

func conversationPrompt(bundle db.EvidenceBundle, userMessage string) string {
	rawBundle, err := json.Marshal(bundle)
	if err != nil {
		rawBundle = []byte("{}")
	}
	return fmt.Sprintf(
		"用户问题：%s\n\n证据包 JSON：%s",
		userMessage,
		string(rawBundle),
	)
}

func fallbackAgentConversationReply(bundle db.EvidenceBundle) string {
	if len(bundle.Signals) == 0 {
		return "暂无活跃风险信号。"
	}
	return "当前风险原因：" + bundle.Signals[0].Reason
}

func fallbackActionPlanReply(bundle db.EvidenceBundle) string {
	reason := "暂无活跃风险信号"
	if len(bundle.Signals) > 0 {
		reason = bundle.Signals[0].Reason
	}
	return "行动计划：先确认阻塞证据，再定位失败检查，随后修复并重新运行测试。当前依据：" + reason
}

func fallbackProjectStatusReply(bundle db.EvidenceBundle) string {
	reason := "暂无活跃风险信号"
	if len(bundle.Signals) > 0 {
		reason = bundle.Signals[0].Reason
	}
	return fmt.Sprintf(
		"当前项目 %s 处于%s状态，风险分 %d。主要阻塞：%s",
		bundle.Project.Name,
		formatRiskLevel(bundle.Assessment.Level),
		bundle.Assessment.Score,
		reason,
	)
}

func (server server) githubDomainConversationReply(
	ctx context.Context,
	userMessage string,
) (agentConversationReply, bool, error) {
	if server.store == nil {
		return agentConversationReply{}, false, nil
	}

	normalized := strings.ToLower(strings.TrimSpace(userMessage))
	if !isPotentialGitHubDomainQuestion(normalized) {
		return agentConversationReply{}, false, nil
	}

	repositories, err := server.store.ListGitHubRepositoryAccess(ctx)
	if err != nil {
		return agentConversationReply{}, true, err
	}

	if isGitHubAuthStatusQuestion(normalized) {
		return server.githubAuthStatusConversationReply(repositories), true, nil
	}
	if isGitHubPullRequestListQuestion(normalized) {
		reply, err := server.githubPullRequestsConversationReply(ctx, repositories, userMessage)
		return reply, true, err
	}
	if isGitHubIssueListQuestion(normalized) {
		reply, err := server.githubIssuesConversationReply(ctx, repositories, userMessage)
		return reply, true, err
	}
	if isGitHubCheckListQuestion(normalized) {
		reply, err := server.githubChecksConversationReply(ctx, repositories, userMessage)
		return reply, true, err
	}
	if repository, ok := selectGitHubRepositoryForMessage(repositories, userMessage); ok &&
		isSpecificGitHubRepositoryViewQuestion(userMessage) {
		return githubRepositoryDetailReply(repository), true, nil
	}
	if isGitHubRepositoryAccessQuestion(normalized) {
		return githubRepositoryListReply(repositories), true, nil
	}

	return agentConversationReply{}, false, nil
}

func (server server) githubAuthStatusConversationReply(
	repositories []db.GitHubRepositoryAccess,
) agentConversationReply {
	status := "GitHub 未连接"
	if len(repositories) > 0 {
		status = "GitHub 已连接"
	}
	appStatus := "GitHub App 未配置"
	if server.githubApp.isConfigured() {
		appStatus = "GitHub App 已配置"
	}
	return agentConversationReply{
		AgentResponse: fmt.Sprintf(
			"%s，当前可访问 %d 个仓库；%s；读取权限包含 metadata、contents、pull requests、checks、issues。",
			status,
			len(repositories),
			appStatus,
		),
		Intent:       "github_auth_status",
		Domain:       "github",
		Entities:     map[string]any{},
		Capabilities: []string{"github.auth.status"},
		ToolCalls: []map[string]any{
			{
				"name":          "github.auth.status",
				"status":        "succeeded",
				"evidence_refs": []string{},
			},
		},
	}
}

func (server server) githubPullRequestsConversationReply(
	ctx context.Context,
	repositories []db.GitHubRepositoryAccess,
	userMessage string,
) (agentConversationReply, error) {
	repository, ok := selectGitHubRepositoryForMessage(repositories, userMessage)
	if !ok {
		return missingGitHubRepositoryReply("github_pull_requests_list"), nil
	}

	events, err := server.store.ListGitHubRepositoryEvents(ctx, repository.ID, "pull_request")
	if err != nil {
		return agentConversationReply{}, err
	}
	pullRequests, evidenceRefs, err := pullRequestsFromGitHubEvents(events)
	if err != nil {
		return agentConversationReply{}, err
	}
	sourceLabel := "已记录的"
	emptySourceLabel := "已记录的"
	if len(events) == 0 && server.githubApp.isConfigured() {
		pullRequests, err = server.liveGitHubPullRequests(ctx, repository)
		if err != nil {
			return agentConversationReply{}, err
		}
		evidenceRefs = evidenceRefsFromPullRequests(pullRequests)
		sourceLabel = "从 GitHub 读取到的"
		emptySourceLabel = "从 GitHub 读取到"
	}

	response := fmt.Sprintf("%s 当前没有%s PR。", repository.FullName, emptySourceLabel)
	if len(pullRequests) > 0 {
		parts := make([]string, 0, len(pullRequests))
		for _, pullRequest := range pullRequests {
			parts = append(parts, formatPullRequestSummary(pullRequest))
		}
		response = fmt.Sprintf(
			"%s 当前%s PR：%s",
			repository.FullName,
			sourceLabel,
			strings.Join(parts, "；"),
		)
	}
	return githubRepositoryEventReply(
		"github_pull_requests_list",
		response,
		"github.pull_requests.list",
		repository,
		evidenceRefs,
	), nil
}

func (server server) githubIssuesConversationReply(
	ctx context.Context,
	repositories []db.GitHubRepositoryAccess,
	userMessage string,
) (agentConversationReply, error) {
	repository, ok := selectGitHubRepositoryForMessage(repositories, userMessage)
	if !ok {
		return missingGitHubRepositoryReply("github_issues_list"), nil
	}

	events, err := server.store.ListGitHubRepositoryEvents(ctx, repository.ID, "issues")
	if err != nil {
		return agentConversationReply{}, err
	}
	issues, evidenceRefs, err := issuesFromGitHubEvents(events)
	if err != nil {
		return agentConversationReply{}, err
	}
	sourceLabel := "已记录的"
	emptySourceLabel := "已记录的"
	if len(events) == 0 && server.githubApp.isConfigured() {
		issues, err = server.liveGitHubIssues(ctx, repository)
		if err != nil {
			return agentConversationReply{}, err
		}
		evidenceRefs = evidenceRefsFromIssues(issues)
		sourceLabel = "从 GitHub 读取到的"
		emptySourceLabel = "从 GitHub 读取到"
	}

	response := fmt.Sprintf("%s 当前没有%s Issue。", repository.FullName, emptySourceLabel)
	if len(issues) > 0 {
		parts := make([]string, 0, len(issues))
		for _, issue := range issues {
			parts = append(parts, formatIssueSummary(issue))
		}
		response = fmt.Sprintf(
			"%s 当前%s Issue：%s",
			repository.FullName,
			sourceLabel,
			strings.Join(parts, "；"),
		)
	}
	return githubRepositoryEventReply(
		"github_issues_list",
		response,
		"github.issues.list",
		repository,
		evidenceRefs,
	), nil
}

func (server server) githubChecksConversationReply(
	ctx context.Context,
	repositories []db.GitHubRepositoryAccess,
	userMessage string,
) (agentConversationReply, error) {
	repository, ok := selectGitHubRepositoryForMessage(repositories, userMessage)
	if !ok {
		return missingGitHubRepositoryReply("github_checks_list"), nil
	}

	events, err := server.store.ListGitHubRepositoryEvents(ctx, repository.ID, "check_run")
	if err != nil {
		return agentConversationReply{}, err
	}
	checks, evidenceRefs, err := checksFromGitHubEvents(events)
	if err != nil {
		return agentConversationReply{}, err
	}
	sourceLabel := "已记录的"
	emptySourceLabel := "已记录的"
	if len(events) == 0 && server.githubApp.isConfigured() {
		checks, err = server.liveGitHubChecks(ctx, repository)
		if err != nil {
			return agentConversationReply{}, err
		}
		evidenceRefs = evidenceRefsFromChecks(checks)
		sourceLabel = "从 GitHub 读取到的"
		emptySourceLabel = "从 GitHub 读取到"
	}

	response := fmt.Sprintf("%s 当前没有%s CI/Checks。", repository.FullName, emptySourceLabel)
	if len(checks) > 0 {
		parts := make([]string, 0, len(checks))
		for _, check := range checks {
			parts = append(parts, formatCheckSummary(check))
		}
		response = fmt.Sprintf(
			"%s 当前%s CI/Checks：%s",
			repository.FullName,
			sourceLabel,
			strings.Join(parts, "；"),
		)
	}
	return githubRepositoryEventReply(
		"github_checks_list",
		response,
		"github.checks.list",
		repository,
		evidenceRefs,
	), nil
}

func (server server) githubRepositoryListConversationReply(
	ctx context.Context,
) (agentConversationReply, error) {
	return server.githubRepositoryListConversationReplyFromRepositories(ctx)
}

func (server server) githubRepositoryAccessConversationReply(
	ctx context.Context,
	userMessage string,
) (agentConversationReply, error) {
	repositories, err := server.store.ListGitHubRepositoryAccess(ctx)
	if err != nil {
		return agentConversationReply{}, err
	}
	if repository, ok := selectGitHubRepositoryForMessage(repositories, userMessage); ok &&
		isSpecificGitHubRepositoryViewQuestion(userMessage) {
		return githubRepositoryDetailReply(repository), nil
	}
	return githubRepositoryListReply(repositories), nil
}

func (server server) githubRepositoryListConversationReplyFromRepositories(
	ctx context.Context,
) (agentConversationReply, error) {
	repositories, err := server.store.ListGitHubRepositoryAccess(ctx)
	if err != nil {
		return agentConversationReply{}, err
	}
	return githubRepositoryListReply(repositories), nil
}

func githubRepositoryListReply(
	repositories []db.GitHubRepositoryAccess,
) agentConversationReply {
	if len(repositories) == 0 {
		return agentConversationReply{
			AgentResponse: "当前还没有 GitHub 授权，或没有发现可访问的 GitHub 仓库。请先在 GitHub 设置里完成授权并同步仓库。",
			Intent:        "github_repository_list",
			Domain:        "github",
			Entities:      map[string]any{},
			Capabilities:  []string{"github.repos.list"},
		}
	}

	names := make([]string, 0, len(repositories))
	for _, repository := range repositories {
		names = append(names, repository.FullName)
	}
	return agentConversationReply{
		AgentResponse: "我当前能看到你授权给 Dev Time 的 GitHub 项目：" + strings.Join(names, "、"),
		Intent:        "github_repository_list",
		Domain:        "github",
		Entities:      map[string]any{},
		Capabilities:  []string{"github.repos.list"},
		ToolCalls: []map[string]any{
			{
				"name":          "github.repos.list",
				"status":        "succeeded",
				"evidence_refs": []string{},
			},
		},
	}
}

func (server server) githubRepositoryDetailConversationReply(
	ctx context.Context,
	userMessage string,
) (agentConversationReply, error) {
	repositories, err := server.store.ListGitHubRepositoryAccess(ctx)
	if err != nil {
		return agentConversationReply{}, err
	}
	if len(repositories) == 0 {
		return agentConversationReply{
			AgentResponse: "当前还没有 GitHub 授权，或没有发现可访问的 GitHub 仓库。请先在 GitHub 设置里完成授权并同步仓库。",
			Intent:        "github_repository_detail",
			Domain:        "github",
			Entities:      map[string]any{},
			Capabilities:  []string{"github.repo.detail"},
		}, nil
	}

	repository, ok := selectGitHubRepositoryForMessage(repositories, userMessage)
	if !ok {
		return agentConversationReply{
			AgentResponse: "我没有找到你提到的 GitHub 仓库，请确认仓库名称，或先在 GitHub 设置里同步仓库列表。",
			Intent:        "github_repository_detail",
			Domain:        "github",
			Entities:      map[string]any{},
			Capabilities:  []string{"github.repos.list"},
			ToolCalls: []map[string]any{
				{
					"name":          "github.repos.list",
					"status":        "succeeded",
					"evidence_refs": []string{},
				},
			},
		}, nil
	}

	return githubRepositoryDetailReply(repository), nil
}

func githubRepositoryDetailReply(
	repository db.GitHubRepositoryAccess,
) agentConversationReply {
	projectID := "未绑定"
	if repository.ProjectID != nil && strings.TrimSpace(*repository.ProjectID) != "" {
		projectID = *repository.ProjectID
	}
	return agentConversationReply{
		AgentResponse: fmt.Sprintf(
			"我能看到 GitHub 项目 %s。仓库 ID：%s；GitHub ID：%d；绑定项目：%s；分析状态：%s；同步状态：%s。",
			repository.FullName,
			repository.ID,
			repository.GitHubID,
			projectID,
			formatRepositoryAnalysisEnabled(repository.AnalysisEnabled),
			formatRepositorySyncStatus(repository.SyncStatus),
		),
		Intent:       "github_repository_detail",
		Domain:       "github",
		Entities:     githubRepositoryEntity(repository),
		Capabilities: []string{"github.repo.detail"},
		ToolCalls: []map[string]any{
			{
				"name":          "github.repo.detail",
				"status":        "succeeded",
				"input":         map[string]string{"repository_id": repository.ID},
				"evidence_refs": []string{},
			},
		},
	}
}

func selfIntroductionReply() string {
	return "我是 Dev Time Agent，定位是项目风险驱动助手。我会围绕项目、PR、测试、CI 和交付阻塞来识别风险、解释证据、生成行动计划，并在需要执行工具前请求确认。"
}

func replyWithoutEvidence(classification conversationIntentClassification) string {
	switch classification.Intent {
	case "smalltalk":
		return "你好，我是 Dev Time Agent。你可以让我解释当前风险、查看证据，或生成下一步行动计划。"
	case "self_intro":
		return selfIntroductionReply()
	case "clarify":
		if strings.TrimSpace(classification.ClarifyingQuestion) != "" {
			return classification.ClarifyingQuestion
		}
		return "你想让我评估当前风险、解释证据，还是生成下一步行动计划？"
	default:
		return "你想让我评估当前风险、解释证据，还是生成下一步行动计划？"
	}
}

func intentRequiresEvidence(intent string) bool {
	switch intent {
	case "project_status", "risk_explain", "evidence_query", "action_plan", "tool_request":
		return true
	default:
		return false
	}
}

func evidenceRefsFromSignals(signals []db.RiskSignal) []string {
	refs := []string{}
	seen := map[string]bool{}
	for _, signal := range signals {
		for _, ref := range signal.EvidenceRefs {
			if seen[ref] {
				continue
			}
			seen[ref] = true
			refs = append(refs, ref)
		}
	}
	return refs
}

func classifyConversationIntent(message string) conversationIntentClassification {
	normalized := strings.TrimSpace(strings.ToLower(message))
	switch normalized {
	case "你好", "您好", "hi", "hello", "hey":
		return conversationIntentClassification{
			Intent:           "smalltalk",
			Confidence:       1,
			RequiresEvidence: false,
		}
	}
	for _, keyword := range []string{"当前状态", "项目状态", "现在状态", "现在怎么样"} {
		if strings.Contains(normalized, keyword) {
			return conversationIntentClassification{
				Intent:           "project_status",
				Confidence:       0.9,
				RequiresEvidence: true,
			}
		}
	}
	if isGitHubRepositoryDetailQuestion(normalized) {
		return conversationIntentClassification{
			Intent:           "github_repository_detail",
			Confidence:       0.9,
			RequiresEvidence: false,
			RequiresTool:     true,
		}
	}
	if isGitHubRepositoryAccessQuestion(normalized) {
		return conversationIntentClassification{
			Intent:           "github_repository_list",
			Confidence:       0.9,
			RequiresEvidence: false,
			RequiresTool:     true,
		}
	}
	for _, keyword := range []string{"介绍", "你是谁", "你能做什么", "自我介绍"} {
		if strings.Contains(normalized, keyword) {
			return conversationIntentClassification{
				Intent:           "self_intro",
				Confidence:       0.95,
				RequiresEvidence: false,
			}
		}
	}
	for _, keyword := range []string{"行动", "计划", "下一步", "怎么做"} {
		if strings.Contains(normalized, keyword) {
			return conversationIntentClassification{
				Intent:           "action_plan",
				Confidence:       0.9,
				RequiresEvidence: true,
			}
		}
	}
	for _, keyword := range []string{"风险", "证据", "为什么", "高风险", "阻塞", "测试", "ci", "pr"} {
		if strings.Contains(normalized, keyword) {
			return conversationIntentClassification{
				Intent:           "risk_explain",
				Confidence:       0.9,
				RequiresEvidence: true,
			}
		}
	}
	return conversationIntentClassification{
		Intent:             "clarify",
		Confidence:         0.35,
		RequiresEvidence:   false,
		ClarifyingQuestion: "你想让我评估当前风险、解释证据，还是生成下一步行动计划？",
	}
}

func isGitHubRepositoryAccessQuestion(normalized string) bool {
	mentionsGitHub := strings.Contains(normalized, "github") ||
		strings.Contains(normalized, "git hub")
	mentionsRepository := false
	for _, keyword := range []string{"项目", "仓库", "repo", "repository", "代码库"} {
		if strings.Contains(normalized, keyword) {
			mentionsRepository = true
			break
		}
	}
	asksVisibility := false
	for _, keyword := range []string{"查看", "看到", "访问", "有哪些", "什么", "列表", "能看", "可见"} {
		if strings.Contains(normalized, keyword) {
			asksVisibility = true
			break
		}
	}
	return mentionsGitHub && mentionsRepository && asksVisibility
}

func isPotentialGitHubDomainQuestion(normalized string) bool {
	if strings.Contains(normalized, "github") || strings.Contains(normalized, "git hub") {
		return true
	}
	for _, keyword := range []string{
		"pull request",
		"pr",
		"issue",
		"issues",
		"ci",
		"check",
		"checks",
	} {
		if strings.Contains(normalized, keyword) {
			return true
		}
	}
	return hasRepositoryLikeToken(normalized)
}

func hasRepositoryLikeToken(normalized string) bool {
	fields := strings.FieldsFunc(normalized, func(r rune) bool {
		return r == ' ' ||
			r == '\t' ||
			r == '\n' ||
			r == '\r' ||
			r == '，' ||
			r == ',' ||
			r == '。' ||
			r == '；' ||
			r == ';' ||
			r == '：' ||
			r == ':' ||
			r == '的'
	})
	for _, field := range fields {
		field = strings.Trim(field, "（）()[]{}")
		if strings.Contains(field, "/") || strings.Contains(field, "-") ||
			strings.Contains(field, "_") || strings.Contains(field, ".") {
			return true
		}
	}
	return false
}

func isGitHubAuthStatusQuestion(normalized string) bool {
	mentionsGitHub := strings.Contains(normalized, "github") ||
		strings.Contains(normalized, "git hub")
	if !mentionsGitHub {
		return false
	}
	for _, keyword := range []string{"授权", "连接", "配置", "安装", "权限", "状态", "可访问", "能访问"} {
		if strings.Contains(normalized, keyword) {
			return true
		}
	}
	return false
}

func isGitHubPullRequestListQuestion(normalized string) bool {
	mentionsPullRequest := strings.Contains(normalized, "pull request") ||
		strings.Contains(normalized, "pr")
	return mentionsPullRequest && asksGitHubVisibility(normalized) && !mentionsRiskQuestion(normalized)
}

func isGitHubIssueListQuestion(normalized string) bool {
	mentionsIssue := strings.Contains(normalized, "issue") || strings.Contains(normalized, "issues")
	return mentionsIssue && asksGitHubVisibility(normalized) && !mentionsRiskQuestion(normalized)
}

func isGitHubCheckListQuestion(normalized string) bool {
	mentionsCheck := false
	for _, keyword := range []string{"ci", "check", "checks", "检查", "测试"} {
		if strings.Contains(normalized, keyword) {
			mentionsCheck = true
			break
		}
	}
	return mentionsCheck && asksGitHubVisibility(normalized) && !mentionsRiskQuestion(normalized)
}

func asksGitHubVisibility(normalized string) bool {
	for _, keyword := range []string{"查看", "看到", "有哪些", "列表", "列出", "打开", "open", "看下", "看一下"} {
		if strings.Contains(normalized, keyword) {
			return true
		}
	}
	return false
}

func mentionsRiskQuestion(normalized string) bool {
	for _, keyword := range []string{"风险", "为什么", "阻塞"} {
		if strings.Contains(normalized, keyword) {
			return true
		}
	}
	return false
}

func isGitHubRepositoryDetailQuestion(normalized string) bool {
	mentionsGitHub := strings.Contains(normalized, "github") ||
		strings.Contains(normalized, "git hub")
	mentionsRepository := false
	for _, keyword := range []string{"项目", "仓库", "repo", "repository", "代码库"} {
		if strings.Contains(normalized, keyword) {
			mentionsRepository = true
			break
		}
	}
	asksVisibility := false
	for _, keyword := range []string{"查看", "打开", "访问", "看下", "看一下"} {
		if strings.Contains(normalized, keyword) {
			asksVisibility = true
			break
		}
	}
	asksAll := false
	for _, keyword := range []string{"我的", "所有", "全部", "有哪些", "列表", "能看到", "可见", "什么"} {
		if strings.Contains(normalized, keyword) {
			asksAll = true
			break
		}
	}
	return mentionsGitHub && mentionsRepository && asksVisibility && !asksAll
}

func isSpecificGitHubRepositoryViewQuestion(message string) bool {
	normalized := strings.ToLower(strings.TrimSpace(message))
	asksVisibility := false
	for _, keyword := range []string{"查看", "打开", "访问", "看下", "看一下", "同步", "状态"} {
		if strings.Contains(normalized, keyword) {
			asksVisibility = true
			break
		}
	}
	if !asksVisibility {
		return false
	}
	for _, keyword := range []string{"所有", "全部", "有哪些", "列表", "能看到", "可见", "什么"} {
		if strings.Contains(normalized, keyword) {
			return false
		}
	}
	return true
}

func selectGitHubRepositoryForMessage(
	repositories []db.GitHubRepositoryAccess,
	message string,
) (db.GitHubRepositoryAccess, bool) {
	normalized := strings.ToLower(strings.TrimSpace(message))
	for _, repository := range repositories {
		fullName := strings.ToLower(strings.TrimSpace(repository.FullName))
		if fullName != "" && strings.Contains(normalized, fullName) {
			return repository, true
		}
	}
	matches := []db.GitHubRepositoryAccess{}
	longestMatchLength := 0
	for _, repository := range repositories {
		name := strings.ToLower(strings.TrimSpace(repository.Name))
		if name != "" && strings.Contains(normalized, name) {
			if len(name) > longestMatchLength {
				matches = []db.GitHubRepositoryAccess{repository}
				longestMatchLength = len(name)
				continue
			}
			if len(name) == longestMatchLength {
				matches = append(matches, repository)
			}
		}
	}
	if len(matches) == 1 {
		return matches[0], true
	}
	if len(repositories) == 1 {
		return repositories[0], true
	}
	return db.GitHubRepositoryAccess{}, false
}

func missingGitHubRepositoryReply(intent string) agentConversationReply {
	return agentConversationReply{
		AgentResponse: "我没有找到你提到的 GitHub 仓库，请确认仓库名称，或先在 GitHub 设置里同步仓库列表。",
		Intent:        intent,
		Domain:        "github",
		Entities:      map[string]any{},
		Capabilities:  []string{"github.repos.list"},
		ToolCalls: []map[string]any{
			{
				"name":          "github.repos.list",
				"status":        "succeeded",
				"evidence_refs": []string{},
			},
		},
	}
}

func githubRepositoryEventReply(
	intent string,
	response string,
	toolName string,
	repository db.GitHubRepositoryAccess,
	evidenceRefs []string,
) agentConversationReply {
	return agentConversationReply{
		AgentResponse: response,
		EvidenceRefs:  evidenceRefs,
		Intent:        intent,
		Domain:        "github",
		Entities:      githubRepositoryEntity(repository),
		Capabilities:  []string{toolName},
		ToolCalls: []map[string]any{
			{
				"name":          "github.repos.list",
				"status":        "succeeded",
				"evidence_refs": []string{},
			},
			{
				"name":   toolName,
				"status": "succeeded",
				"input": map[string]string{
					"repository_id": repository.ID,
				},
				"evidence_refs": evidenceRefs,
			},
		},
	}
}

func githubRepositoryEntity(repository db.GitHubRepositoryAccess) map[string]any {
	entity := map[string]any{
		"id":        repository.ID,
		"github_id": repository.GitHubID,
		"owner":     repository.Owner,
		"name":      repository.Name,
		"full_name": repository.FullName,
	}
	if repository.ProjectID != nil && strings.TrimSpace(*repository.ProjectID) != "" {
		entity["project_id"] = *repository.ProjectID
	}
	return map[string]any{"repository": entity}
}

func pullRequestsFromGitHubEvents(
	events []db.GitHubRepositoryEvent,
) ([]internalPullRequest, []string, error) {
	pullRequests := make([]internalPullRequest, 0, len(events))
	evidenceRefs := make([]string, 0, len(events))
	for _, event := range events {
		var payload struct {
			PullRequest struct {
				Number  int    `json:"number"`
				Title   string `json:"title"`
				State   string `json:"state"`
				HTMLURL string `json:"html_url"`
			} `json:"pull_request"`
		}
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return nil, nil, fmt.Errorf("decode pull_request event: %w", err)
		}
		pullRequests = append(pullRequests, internalPullRequest{
			EvidenceRef: event.ID,
			Number:      payload.PullRequest.Number,
			Title:       payload.PullRequest.Title,
			State:       payload.PullRequest.State,
			URL:         payload.PullRequest.HTMLURL,
		})
		evidenceRefs = append(evidenceRefs, event.ID)
	}
	return pullRequests, evidenceRefs, nil
}

func issuesFromGitHubEvents(
	events []db.GitHubRepositoryEvent,
) ([]internalIssue, []string, error) {
	issues := make([]internalIssue, 0, len(events))
	evidenceRefs := make([]string, 0, len(events))
	for _, event := range events {
		var payload struct {
			Issue struct {
				Number  int    `json:"number"`
				Title   string `json:"title"`
				State   string `json:"state"`
				HTMLURL string `json:"html_url"`
			} `json:"issue"`
		}
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return nil, nil, fmt.Errorf("decode issue event: %w", err)
		}
		issues = append(issues, internalIssue{
			EvidenceRef: event.ID,
			Number:      payload.Issue.Number,
			Title:       payload.Issue.Title,
			State:       payload.Issue.State,
			URL:         payload.Issue.HTMLURL,
		})
		evidenceRefs = append(evidenceRefs, event.ID)
	}
	return issues, evidenceRefs, nil
}

func checksFromGitHubEvents(
	events []db.GitHubRepositoryEvent,
) ([]internalCheckRun, []string, error) {
	checks := make([]internalCheckRun, 0, len(events))
	evidenceRefs := make([]string, 0, len(events))
	for _, event := range events {
		var payload struct {
			CheckRun struct {
				Name       string `json:"name"`
				Status     string `json:"status"`
				Conclusion string `json:"conclusion"`
				HTMLURL    string `json:"html_url"`
			} `json:"check_run"`
		}
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return nil, nil, fmt.Errorf("decode check_run event: %w", err)
		}
		checks = append(checks, internalCheckRun{
			EvidenceRef: event.ID,
			Name:        payload.CheckRun.Name,
			Status:      payload.CheckRun.Status,
			Conclusion:  payload.CheckRun.Conclusion,
			URL:         payload.CheckRun.HTMLURL,
		})
		evidenceRefs = append(evidenceRefs, event.ID)
	}
	return checks, evidenceRefs, nil
}

func evidenceRefsFromPullRequests(pullRequests []internalPullRequest) []string {
	refs := make([]string, 0, len(pullRequests))
	for _, pullRequest := range pullRequests {
		if strings.TrimSpace(pullRequest.EvidenceRef) != "" {
			refs = append(refs, pullRequest.EvidenceRef)
		}
	}
	return refs
}

func evidenceRefsFromIssues(issues []internalIssue) []string {
	refs := make([]string, 0, len(issues))
	for _, issue := range issues {
		if strings.TrimSpace(issue.EvidenceRef) != "" {
			refs = append(refs, issue.EvidenceRef)
		}
	}
	return refs
}

func evidenceRefsFromChecks(checks []internalCheckRun) []string {
	refs := make([]string, 0, len(checks))
	for _, check := range checks {
		if strings.TrimSpace(check.EvidenceRef) != "" {
			refs = append(refs, check.EvidenceRef)
		}
	}
	return refs
}

func formatPullRequestSummary(pullRequest internalPullRequest) string {
	title := pullRequest.Title
	if strings.TrimSpace(title) == "" {
		title = "Untitled"
	}
	state := pullRequest.State
	if strings.TrimSpace(state) == "" {
		state = "unknown"
	}
	return fmt.Sprintf("PR #%d %s（%s）", pullRequest.Number, title, state)
}

func formatIssueSummary(issue internalIssue) string {
	title := issue.Title
	if strings.TrimSpace(title) == "" {
		title = "Untitled"
	}
	state := issue.State
	if strings.TrimSpace(state) == "" {
		state = "unknown"
	}
	return fmt.Sprintf("Issue #%d %s（%s）", issue.Number, title, state)
}

func formatCheckSummary(check internalCheckRun) string {
	name := check.Name
	if strings.TrimSpace(name) == "" {
		name = "unknown"
	}
	status := check.Status
	if strings.TrimSpace(status) == "" {
		status = "unknown"
	}
	conclusion := check.Conclusion
	if strings.TrimSpace(conclusion) == "" {
		conclusion = "pending"
	}
	return fmt.Sprintf("%s %s（%s）", name, status, conclusion)
}

func formatRepositoryAnalysisEnabled(enabled bool) string {
	if enabled {
		return "已启用"
	}
	return "已关闭"
}

func formatRepositorySyncStatus(status string) string {
	switch status {
	case "succeeded":
		return "已同步"
	case "syncing":
		return "同步中"
	case "failed":
		return "同步失败"
	case "not_synced":
		return "未同步"
	default:
		if strings.TrimSpace(status) == "" {
			return "未知"
		}
		return status
	}
}

func formatRiskLevel(level string) string {
	labels := map[string]string{
		"high":   "高风险",
		"medium": "中风险",
		"low":    "低风险",
	}
	if label, ok := labels[level]; ok {
		return label
	}
	return level
}
