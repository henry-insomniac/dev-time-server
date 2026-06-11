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

func (server server) buildAgentConversationReply(
	ctx context.Context,
	riskAssessmentID string,
	userMessage string,
) (string, []string, error) {
	bundle, err := server.store.GetEvidenceBundle(ctx, riskAssessmentID)
	if err != nil {
		return "", nil, err
	}
	evidenceRefs := evidenceRefsFromSignals(bundle.Signals)

	config, err := server.store.GetActiveLLMProviderConfig(ctx)
	if errors.Is(err, db.ErrNotFound) {
		return fallbackAgentConversationReply(bundle), evidenceRefs, nil
	}
	if err != nil {
		return "", nil, err
	}

	reply, err := requestLLMConversationReply(ctx, config, bundle, userMessage)
	if err != nil {
		return "", nil, err
	}
	return reply, evidenceRefs, nil
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
