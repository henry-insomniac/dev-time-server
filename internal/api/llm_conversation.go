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
	ClarifyingQuestion string  `json:"clarifying_question"`
}

func (server server) buildAgentConversationReply(
	ctx context.Context,
	conversationID string,
	riskAssessmentID string,
	userMessage string,
) (string, []string, string, error) {
	if strings.TrimSpace(server.agentRuntimeBaseURL) != "" {
		reply, evidenceRefs, intent, err := requestAgentRuntimeSessionTurn(
			ctx,
			server.agentRuntimeBaseURL,
			conversationID,
			riskAssessmentID,
			userMessage,
			nil,
		)
		if err == nil {
			if !intentRequiresEvidence(intent) {
				return reply, evidenceRefs, intent, nil
			}
			bundle, bundleErr := server.store.GetEvidenceBundle(ctx, riskAssessmentID)
			if bundleErr != nil {
				return "", nil, "", bundleErr
			}
			return requestAgentRuntimeSessionTurn(
				ctx,
				server.agentRuntimeBaseURL,
				conversationID,
				riskAssessmentID,
				userMessage,
				&bundle,
			)
		}
	}

	classification := classifyConversationIntent(userMessage)

	if !classification.RequiresEvidence {
		return replyWithoutEvidence(classification), nil, classification.Intent, nil
	}

	bundle, err := server.store.GetEvidenceBundle(ctx, riskAssessmentID)
	if err != nil {
		return "", nil, "", err
	}
	evidenceRefs := evidenceRefsFromSignals(bundle.Signals)

	if classification.Intent == "project_status" {
		return fallbackProjectStatusReply(bundle), evidenceRefs, "project_status", nil
	}
	if classification.Intent == "action_plan" {
		return fallbackActionPlanReply(bundle), evidenceRefs, "action_plan", nil
	}

	config, err := server.store.GetActiveLLMProviderConfig(ctx)
	if errors.Is(err, db.ErrNotFound) {
		return fallbackAgentConversationReply(bundle), evidenceRefs, "risk_explain", nil
	}
	if err != nil {
		return "", nil, "", err
	}

	reply, err := requestLLMConversationReply(ctx, config, bundle, userMessage)
	if err != nil {
		return "", nil, "", err
	}
	return reply, evidenceRefs, "risk_explain", nil
}

func requestAgentRuntimeSessionTurn(
	ctx context.Context,
	baseURL string,
	conversationID string,
	riskAssessmentID string,
	userMessage string,
	bundle *db.EvidenceBundle,
) (string, []string, string, error) {
	payload := map[string]any{
		"conversation_id":    conversationID,
		"project_id":         "",
		"risk_assessment_id": riskAssessmentID,
		"message":            userMessage,
	}
	if bundle != nil {
		payload["project_id"] = bundle.Assessment.ProjectID
		payload["evidence_bundle"] = bundle
	}
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return "", nil, "", fmt.Errorf("marshal agent runtime session payload: %w", err)
	}

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		strings.TrimRight(baseURL, "/")+"/agent/sessions/"+conversationID+"/turns",
		bytes.NewReader(rawPayload),
	)
	if err != nil {
		return "", nil, "", fmt.Errorf("create agent runtime session request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")

	response, err := llmHTTPClient.Do(request)
	if err != nil {
		return "", nil, "", fmt.Errorf("call agent runtime session: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", nil, "", fmt.Errorf("agent runtime session returned status %d", response.StatusCode)
	}

	var body struct {
		AgentResponse string   `json:"agent_response"`
		Intent        string   `json:"intent"`
		EvidenceRefs  []string `json:"evidence_refs"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		return "", nil, "", fmt.Errorf("decode agent runtime session response: %w", err)
	}
	if strings.TrimSpace(body.AgentResponse) == "" || strings.TrimSpace(body.Intent) == "" {
		return "", nil, "", fmt.Errorf("agent runtime session response is incomplete")
	}

	return strings.TrimSpace(body.AgentResponse), body.EvidenceRefs, strings.TrimSpace(body.Intent), nil
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
