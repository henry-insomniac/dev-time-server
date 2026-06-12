package db

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

var ErrNotFound = errors.New("not found")

type RepositoryInput struct {
	GitHubID int64
	Owner    string
	Name     string
	FullName string
}

type Repository struct {
	ID       string `json:"id"`
	GitHubID int64  `json:"github_id"`
	Owner    string `json:"owner"`
	Name     string `json:"name"`
	FullName string `json:"full_name"`
}

type Project struct {
	ID           string `json:"id"`
	RepositoryID string `json:"repository_id"`
	Name         string `json:"name"`
}

type RepositoryImport struct {
	Repository Repository
	Created    bool
}

type GitHubEventInput struct {
	RepositoryID string
	DeliveryID   string
	EventType    string
	Payload      json.RawMessage
	OccurredAt   time.Time
}

type GitHubEvent struct {
	ID           string          `json:"id"`
	RepositoryID string          `json:"repository_id"`
	DeliveryID   string          `json:"delivery_id"`
	EventType    string          `json:"event_type"`
	Payload      json.RawMessage `json:"payload"`
	Duplicate    bool            `json:"duplicate"`
}

type RiskAssessment struct {
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`
	Score     int    `json:"score"`
	Level     string `json:"level"`
	Trend     string `json:"trend"`
}

type RiskSignal struct {
	ID           string   `json:"id"`
	ProjectID    string   `json:"project_id"`
	Category     string   `json:"category"`
	Severity     int      `json:"severity"`
	Reason       string   `json:"reason"`
	EvidenceRefs []string `json:"evidence_refs"`
}

type ProjectRisk struct {
	Assessment RiskAssessment `json:"assessment"`
	Signals    []RiskSignal   `json:"signals"`
}

type ProjectSummary struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	RiskScore int    `json:"risk_score"`
	RiskLevel string `json:"risk_level"`
}

type LLMProviderConfigInput struct {
	Provider string
	BaseURL  string
	Model    string
	APIKey   string
}

type LLMProviderConfig struct {
	ID          string `json:"id"`
	Provider    string `json:"provider"`
	BaseURL     string `json:"base_url"`
	Model       string `json:"model"`
	Configured  bool   `json:"configured"`
	KeyLastFour string `json:"key_last_four"`
	Enabled     bool   `json:"enabled"`
}

type ActiveLLMProviderConfig struct {
	Provider string `json:"provider"`
	BaseURL  string `json:"base_url"`
	Model    string `json:"model"`
	APIKey   string `json:"api_key"`
}

type EvidenceBundle struct {
	Project        ProjectSummary  `json:"project"`
	Assessment     RiskAssessment  `json:"assessment"`
	Signals        []RiskSignal    `json:"signals"`
	Events         []EvidenceEvent `json:"events"`
	AllowedActions []string        `json:"allowed_actions"`
}

type EvidenceEvent struct {
	ID        string          `json:"id"`
	EventType string          `json:"event_type"`
	Payload   json.RawMessage `json:"payload"`
}

type AgentConversation struct {
	ID                     string `json:"id"`
	ProjectID              string `json:"project_id"`
	LatestRiskAssessmentID string `json:"latest_risk_assessment_id"`
	Status                 string `json:"status"`
}

type AgentConversationTurn struct {
	ID             string            `json:"id"`
	ConversationID string            `json:"conversation_id"`
	UserMessage    string            `json:"user_message"`
	AgentResponse  string            `json:"agent_response"`
	EvidenceRefs   []string          `json:"evidence_refs"`
	Intent         string            `json:"intent"`
	TraceEvents    []AgentTraceEvent `json:"trace_events"`
}

type AgentTraceEvent struct {
	ID             string   `json:"id"`
	ConversationID string   `json:"conversation_id"`
	TurnID         string   `json:"turn_id"`
	EventType      string   `json:"event_type"`
	Title          string   `json:"title"`
	Body           string   `json:"body"`
	Intent         string   `json:"intent"`
	EvidenceRefs   []string `json:"evidence_refs"`
}

type ActionSuggestionInput struct {
	ProjectID       string
	AgentArtifactID string
	ActionType      string
	TargetRef       string
	DraftBody       string
	EvidenceRefs    []string
}

type ActionSuggestion struct {
	ID           string   `json:"id"`
	ProjectID    string   `json:"project_id"`
	ActionType   string   `json:"action_type"`
	Status       string   `json:"status"`
	TargetRef    string   `json:"target_ref"`
	DraftBody    string   `json:"draft_body"`
	EvidenceRefs []string `json:"evidence_refs"`
}

type AgentJob struct {
	JobID               string   `json:"job_id"`
	ProjectID           string   `json:"project_id"`
	RiskAssessmentID    string   `json:"risk_assessment_id"`
	AgentType           string   `json:"agent_type"`
	Trigger             string   `json:"trigger"`
	Status              string   `json:"status"`
	ActionSuggestionIDs []string `json:"action_suggestion_ids,omitempty"`
}

type AgentRun struct {
	ID               string      `json:"id"`
	AgentJobID       string      `json:"agent_job_id"`
	ProjectID        string      `json:"project_id"`
	RiskAssessmentID string      `json:"risk_assessment_id"`
	AgentType        string      `json:"agent_type"`
	Status           string      `json:"status"`
	Summary          string      `json:"summary"`
	Steps            []AgentStep `json:"steps"`
}

type AgentStep struct {
	ID           string   `json:"id"`
	AgentRunID   string   `json:"agent_run_id"`
	StepType     string   `json:"step_type"`
	Status       string   `json:"status"`
	Title        string   `json:"title"`
	Body         string   `json:"body"`
	EvidenceRefs []string `json:"evidence_refs"`
}

type AgentArtifactInput struct {
	Summary           string
	EvidenceRefs      []string
	Model             string
	PromptVersion     string
	ActionSuggestions []ActionSuggestionInput
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (store *Store) UpsertRepository(ctx context.Context, input RepositoryInput) (Repository, error) {
	imported, err := store.ImportRepository(ctx, input)
	if err != nil {
		return Repository{}, err
	}

	return imported.Repository, nil
}

func (store *Store) ImportRepository(
	ctx context.Context,
	input RepositoryInput,
) (RepositoryImport, error) {
	repositoryID := fmt.Sprintf("repo_%d", input.GitHubID)

	var repository Repository
	var created bool
	err := store.pool.QueryRow(
		ctx,
		`
		INSERT INTO repositories (id, github_id, owner, name, full_name)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (github_id) DO UPDATE
		SET owner = EXCLUDED.owner,
		    name = EXCLUDED.name,
		    full_name = EXCLUDED.full_name
		RETURNING id, github_id, owner, name, full_name, (xmax = 0) AS created
		`,
		repositoryID,
		input.GitHubID,
		input.Owner,
		input.Name,
		input.FullName,
	).Scan(
		&repository.ID,
		&repository.GitHubID,
		&repository.Owner,
		&repository.Name,
		&repository.FullName,
		&created,
	)
	if err != nil {
		return RepositoryImport{}, fmt.Errorf("import repository: %w", err)
	}

	return RepositoryImport{Repository: repository, Created: created}, nil
}

func (store *Store) EnsureProjectForRepository(
	ctx context.Context,
	repositoryID string,
	name string,
) (Project, error) {
	projectID := "project_" + repositoryID

	var project Project
	err := store.pool.QueryRow(
		ctx,
		`
		INSERT INTO projects (id, repository_id, name)
		VALUES ($1, $2, $3)
		ON CONFLICT (repository_id) DO UPDATE
		SET name = EXCLUDED.name
		RETURNING id, repository_id, name
		`,
		projectID,
		repositoryID,
		name,
	).Scan(&project.ID, &project.RepositoryID, &project.Name)
	if err != nil {
		return Project{}, fmt.Errorf("ensure project: %w", err)
	}

	return project, nil
}

func (store *Store) RecordGitHubEvent(
	ctx context.Context,
	input GitHubEventInput,
) (GitHubEvent, error) {
	eventID := "event_" + input.DeliveryID
	occurredAt := input.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}

	var event GitHubEvent
	var inserted bool
	err := store.pool.QueryRow(
		ctx,
		`
		INSERT INTO github_events (
			id,
			repository_id,
			delivery_id,
			event_type,
			payload,
			occurred_at
		)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (delivery_id) DO UPDATE
		SET delivery_id = github_events.delivery_id
		RETURNING id, repository_id, delivery_id, event_type, payload, (xmax = 0) AS inserted
		`,
		eventID,
		input.RepositoryID,
		input.DeliveryID,
		input.EventType,
		input.Payload,
		occurredAt,
	).Scan(
		&event.ID,
		&event.RepositoryID,
		&event.DeliveryID,
		&event.EventType,
		&event.Payload,
		&inserted,
	)
	if err != nil {
		return GitHubEvent{}, fmt.Errorf("record github event: %w", err)
	}

	event.Duplicate = !inserted
	return event, nil
}

func (store *Store) AssessProjectRisk(ctx context.Context, projectID string) (ProjectRisk, error) {
	repositoryID, err := store.repositoryIDForProject(ctx, projectID)
	if err != nil {
		return ProjectRisk{}, err
	}

	signals, err := store.buildRiskSignals(ctx, projectID, repositoryID)
	if err != nil {
		return ProjectRisk{}, err
	}

	assessment := RiskAssessment{
		ID:        "risk_" + projectID,
		ProjectID: projectID,
		Score:     riskScore(signals),
		Level:     riskLevel(riskScore(signals)),
		Trend:     "new",
	}

	if err := store.replaceRiskSnapshot(ctx, assessment, signals); err != nil {
		return ProjectRisk{}, err
	}

	return ProjectRisk{
		Assessment: assessment,
		Signals:    signals,
	}, nil
}

func (store *Store) ListProjectsByRisk(ctx context.Context) ([]ProjectSummary, error) {
	rows, err := store.pool.Query(
		ctx,
		`
		SELECT id, name
		FROM projects
		ORDER BY created_at ASC, id ASC
		`,
	)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	defer rows.Close()

	var projects []ProjectSummary
	for rows.Next() {
		var project ProjectSummary
		if err := rows.Scan(&project.ID, &project.Name); err != nil {
			return nil, fmt.Errorf("scan project: %w", err)
		}

		projectRisk, err := store.AssessProjectRisk(ctx, project.ID)
		if err != nil {
			return nil, err
		}
		project.RiskScore = projectRisk.Assessment.Score
		project.RiskLevel = projectRisk.Assessment.Level
		projects = append(projects, project)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate projects: %w", err)
	}

	sort.SliceStable(projects, func(left, right int) bool {
		return projects[left].RiskScore > projects[right].RiskScore
	})

	return projects, nil
}

func (store *Store) repositoryIDForProject(ctx context.Context, projectID string) (string, error) {
	var repositoryID string
	if err := store.pool.QueryRow(
		ctx,
		`SELECT repository_id FROM projects WHERE id = $1`,
		projectID,
	).Scan(&repositoryID); err != nil {
		return "", fmt.Errorf("load project repository: %w", err)
	}

	return repositoryID, nil
}

func (store *Store) buildRiskSignals(
	ctx context.Context,
	projectID string,
	repositoryID string,
) ([]RiskSignal, error) {
	rows, err := store.pool.Query(
		ctx,
		`
		SELECT id, payload #>> '{check_run,name}'
		FROM github_events
		WHERE repository_id = $1
		  AND event_type = 'check_run'
		  AND payload #>> '{check_run,conclusion}' = 'failure'
		ORDER BY occurred_at DESC, id DESC
		`,
		repositoryID,
	)
	if err != nil {
		return nil, fmt.Errorf("query failed check runs: %w", err)
	}
	defer rows.Close()

	var signals []RiskSignal
	for rows.Next() {
		var eventID string
		var checkName string
		if err := rows.Scan(&eventID, &checkName); err != nil {
			return nil, fmt.Errorf("scan failed check run: %w", err)
		}

		if checkName == "" {
			checkName = "GitHub check"
		}

		signals = append(signals, RiskSignal{
			ID:           "signal_" + eventID,
			ProjectID:    projectID,
			Category:     "blocked",
			Severity:     70,
			Reason:       checkName + " failed and is blocking progress.",
			EvidenceRefs: []string{eventID},
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate failed check runs: %w", err)
	}

	return signals, nil
}

func (store *Store) replaceRiskSnapshot(
	ctx context.Context,
	assessment RiskAssessment,
	signals []RiskSignal,
) error {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin risk transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	if _, err := tx.Exec(ctx, `DELETE FROM risk_signals WHERE project_id = $1`, assessment.ProjectID); err != nil {
		return fmt.Errorf("delete risk signals: %w", err)
	}

	for _, signal := range signals {
		evidenceRefs, err := json.Marshal(signal.EvidenceRefs)
		if err != nil {
			return fmt.Errorf("marshal evidence refs: %w", err)
		}
		if _, err := tx.Exec(
			ctx,
			`
			INSERT INTO risk_signals (
				id,
				project_id,
				category,
				severity,
				reason,
				evidence_refs
			)
			VALUES ($1, $2, $3, $4, $5, $6)
			`,
			signal.ID,
			signal.ProjectID,
			signal.Category,
			signal.Severity,
			signal.Reason,
			evidenceRefs,
		); err != nil {
			return fmt.Errorf("insert risk signal: %w", err)
		}
	}

	if _, err := tx.Exec(
		ctx,
		`
		INSERT INTO risk_assessments (id, project_id, score, level, trend)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (id) DO UPDATE
		SET score = EXCLUDED.score,
		    level = EXCLUDED.level,
		    trend = EXCLUDED.trend,
		    created_at = now()
		`,
		assessment.ID,
		assessment.ProjectID,
		assessment.Score,
		assessment.Level,
		assessment.Trend,
	); err != nil {
		return fmt.Errorf("upsert risk assessment: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit risk transaction: %w", err)
	}

	return nil
}

func riskScore(signals []RiskSignal) int {
	score := 0
	for _, signal := range signals {
		if signal.Severity > score {
			score = signal.Severity
		}
	}
	return score
}

func riskLevel(score int) string {
	switch {
	case score >= 76:
		return "urgent"
	case score >= 56:
		return "high"
	case score >= 31:
		return "watch"
	default:
		return "stable"
	}
}

func (store *Store) SaveLLMProviderConfig(
	ctx context.Context,
	input LLMProviderConfigInput,
) (LLMProviderConfig, error) {
	configID := "llm_provider_" + input.Provider
	ciphertext, err := encryptAPIKey(input.APIKey)
	if err != nil {
		return LLMProviderConfig{}, err
	}
	keyLastFour := lastFour(input.APIKey)

	var config LLMProviderConfig
	err = store.pool.QueryRow(
		ctx,
		`
		INSERT INTO llm_provider_configs (
			id,
			provider,
			base_url,
			model,
			api_key_ciphertext,
			key_last_four,
			enabled
		)
		VALUES ($1, $2, $3, $4, $5, $6, true)
		ON CONFLICT (provider) DO UPDATE
		SET base_url = EXCLUDED.base_url,
		    model = EXCLUDED.model,
		    api_key_ciphertext = EXCLUDED.api_key_ciphertext,
		    key_last_four = EXCLUDED.key_last_four,
		    enabled = true,
		    updated_at = now()
		RETURNING id, provider, base_url, model, key_last_four, enabled
		`,
		configID,
		input.Provider,
		input.BaseURL,
		input.Model,
		ciphertext,
		keyLastFour,
	).Scan(
		&config.ID,
		&config.Provider,
		&config.BaseURL,
		&config.Model,
		&config.KeyLastFour,
		&config.Enabled,
	)
	if err != nil {
		return LLMProviderConfig{}, fmt.Errorf("save llm provider config: %w", err)
	}

	config.Configured = true
	return config, nil
}

func (store *Store) ListLLMProviderConfigs(ctx context.Context) ([]LLMProviderConfig, error) {
	rows, err := store.pool.Query(
		ctx,
		`
		SELECT id, provider, base_url, model, key_last_four, enabled
		FROM llm_provider_configs
		ORDER BY provider ASC
		`,
	)
	if err != nil {
		return nil, fmt.Errorf("list llm provider configs: %w", err)
	}
	defer rows.Close()

	var configs []LLMProviderConfig
	for rows.Next() {
		var config LLMProviderConfig
		if err := rows.Scan(
			&config.ID,
			&config.Provider,
			&config.BaseURL,
			&config.Model,
			&config.KeyLastFour,
			&config.Enabled,
		); err != nil {
			return nil, fmt.Errorf("scan llm provider config: %w", err)
		}
		config.Configured = true
		configs = append(configs, config)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate llm provider configs: %w", err)
	}

	return configs, nil
}

func (store *Store) GetActiveLLMProviderConfig(
	ctx context.Context,
) (ActiveLLMProviderConfig, error) {
	var config ActiveLLMProviderConfig
	var ciphertext string
	err := store.pool.QueryRow(
		ctx,
		`
		SELECT provider, base_url, model, api_key_ciphertext
		FROM llm_provider_configs
		WHERE enabled = true
		  AND provider IN ('openai', 'deepseek')
		ORDER BY CASE provider
		  WHEN 'openai' THEN 1
		  WHEN 'deepseek' THEN 2
		  ELSE 3
		END
		LIMIT 1
		`,
	).Scan(
		&config.Provider,
		&config.BaseURL,
		&config.Model,
		&ciphertext,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ActiveLLMProviderConfig{}, ErrNotFound
	}
	if err != nil {
		return ActiveLLMProviderConfig{}, fmt.Errorf("get active llm provider config: %w", err)
	}

	apiKey, err := decryptAPIKey(ciphertext)
	if err != nil {
		return ActiveLLMProviderConfig{}, err
	}
	config.APIKey = apiKey
	return config, nil
}

func (store *Store) GetEvidenceBundle(
	ctx context.Context,
	assessmentID string,
) (EvidenceBundle, error) {
	var bundle EvidenceBundle
	if err := store.pool.QueryRow(
		ctx,
		`
		SELECT
			p.id,
			p.name,
			ra.id,
			ra.project_id,
			ra.score,
			ra.level,
			ra.trend
		FROM risk_assessments ra
		JOIN projects p ON p.id = ra.project_id
		WHERE ra.id = $1
		`,
		assessmentID,
	).Scan(
		&bundle.Project.ID,
		&bundle.Project.Name,
		&bundle.Assessment.ID,
		&bundle.Assessment.ProjectID,
		&bundle.Assessment.Score,
		&bundle.Assessment.Level,
		&bundle.Assessment.Trend,
	); err != nil {
		return EvidenceBundle{}, fmt.Errorf("load risk assessment: %w", err)
	}
	bundle.Project.RiskScore = bundle.Assessment.Score
	bundle.Project.RiskLevel = bundle.Assessment.Level

	signals, evidenceRefs, err := store.riskSignalsForProject(ctx, bundle.Assessment.ProjectID)
	if err != nil {
		return EvidenceBundle{}, err
	}
	bundle.Signals = signals

	repositoryID, err := store.repositoryIDForProject(ctx, bundle.Assessment.ProjectID)
	if err != nil {
		return EvidenceBundle{}, err
	}

	events, err := store.evidenceEvents(ctx, evidenceRefs, repositoryID)
	if err != nil {
		return EvidenceBundle{}, err
	}
	bundle.Events = events
	bundle.AllowedActions = []string{"create_issue", "create_pr_comment", "suggest_label"}

	return bundle, nil
}

func (store *Store) riskSignalsForProject(
	ctx context.Context,
	projectID string,
) ([]RiskSignal, []string, error) {
	rows, err := store.pool.Query(
		ctx,
		`
		SELECT id, project_id, category, severity, reason, evidence_refs
		FROM risk_signals
		WHERE project_id = $1
		ORDER BY severity DESC, id ASC
		`,
		projectID,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("query risk signals: %w", err)
	}
	defer rows.Close()

	var signals []RiskSignal
	var evidenceRefs []string
	for rows.Next() {
		var signal RiskSignal
		var rawEvidenceRefs []byte
		if err := rows.Scan(
			&signal.ID,
			&signal.ProjectID,
			&signal.Category,
			&signal.Severity,
			&signal.Reason,
			&rawEvidenceRefs,
		); err != nil {
			return nil, nil, fmt.Errorf("scan risk signal: %w", err)
		}
		if err := json.Unmarshal(rawEvidenceRefs, &signal.EvidenceRefs); err != nil {
			return nil, nil, fmt.Errorf("decode evidence refs: %w", err)
		}
		evidenceRefs = append(evidenceRefs, signal.EvidenceRefs...)
		signals = append(signals, signal)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate risk signals: %w", err)
	}

	return signals, evidenceRefs, nil
}

func (store *Store) evidenceEvents(
	ctx context.Context,
	evidenceRefs []string,
	repositoryID string,
) ([]EvidenceEvent, error) {
	rows, err := store.pool.Query(
		ctx,
		`
		SELECT id, event_type, payload
		FROM github_events
		WHERE id = ANY($1::text[])
		   OR (
		       repository_id = $2
		       AND event_type = 'pull_request'
		   )
		ORDER BY occurred_at DESC, id DESC
		`,
		evidenceRefs,
		repositoryID,
	)
	if err != nil {
		return nil, fmt.Errorf("query evidence events: %w", err)
	}
	defer rows.Close()

	var events []EvidenceEvent
	for rows.Next() {
		var event EvidenceEvent
		if err := rows.Scan(&event.ID, &event.EventType, &event.Payload); err != nil {
			return nil, fmt.Errorf("scan evidence event: %w", err)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate evidence events: %w", err)
	}

	return events, nil
}

func (store *Store) GetOrCreateAgentConversation(
	ctx context.Context,
	projectID string,
	riskAssessmentID string,
) (AgentConversation, error) {
	conversationID := "conversation_" + projectID

	var conversation AgentConversation
	err := store.pool.QueryRow(
		ctx,
		`
		INSERT INTO agent_conversations (
			id,
			project_id,
			latest_risk_assessment_id,
			status
		)
		VALUES ($1, $2, $3, 'active')
		ON CONFLICT (id) DO UPDATE
		SET latest_risk_assessment_id = EXCLUDED.latest_risk_assessment_id,
		    status = 'active',
		    updated_at = now()
		RETURNING id, project_id, latest_risk_assessment_id, status
		`,
		conversationID,
		projectID,
		riskAssessmentID,
	).Scan(
		&conversation.ID,
		&conversation.ProjectID,
		&conversation.LatestRiskAssessmentID,
		&conversation.Status,
	)
	if err != nil {
		return AgentConversation{}, fmt.Errorf("get or create agent conversation: %w", err)
	}

	return conversation, nil
}

func (store *Store) AddAgentConversationTurn(
	ctx context.Context,
	conversationID string,
	userMessage string,
	agentResponse string,
	evidenceRefs []string,
	intent string,
) (AgentConversationTurn, error) {
	if evidenceRefs == nil {
		evidenceRefs = []string{}
	}
	rawEvidenceRefs, err := json.Marshal(evidenceRefs)
	if err != nil {
		return AgentConversationTurn{}, fmt.Errorf("marshal turn evidence refs: %w", err)
	}

	turn := AgentConversationTurn{
		ID:             fmt.Sprintf("turn_%d", time.Now().UTC().UnixNano()),
		ConversationID: conversationID,
		UserMessage:    userMessage,
		AgentResponse:  agentResponse,
		EvidenceRefs:   evidenceRefs,
		Intent:         intent,
	}
	traceEvent := AgentTraceEvent{
		ID:             "trace_" + turn.ID,
		ConversationID: conversationID,
		TurnID:         turn.ID,
		EventType:      "intent_routed",
		Title:          "完成意图识别",
		Body:           "Agent 已根据用户输入选择处理路径。",
		Intent:         intent,
		EvidenceRefs:   evidenceRefs,
	}

	if _, err := store.pool.Exec(
		ctx,
		`
		INSERT INTO agent_conversation_turns (
			id,
			conversation_id,
			role,
			user_message,
			agent_response,
			evidence_refs,
			intent
		)
		VALUES ($1, $2, 'agent', $3, $4, $5, $6)
		`,
		turn.ID,
		turn.ConversationID,
		turn.UserMessage,
		turn.AgentResponse,
		rawEvidenceRefs,
		turn.Intent,
	); err != nil {
		return AgentConversationTurn{}, fmt.Errorf("insert agent conversation turn: %w", err)
	}
	rawTraceEvidenceRefs, err := json.Marshal(traceEvent.EvidenceRefs)
	if err != nil {
		return AgentConversationTurn{}, fmt.Errorf("marshal trace evidence refs: %w", err)
	}
	if _, err := store.pool.Exec(
		ctx,
		`
		INSERT INTO agent_trace_events (
			id,
			conversation_id,
			turn_id,
			event_type,
			title,
			body,
			intent,
			evidence_refs
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		`,
		traceEvent.ID,
		traceEvent.ConversationID,
		traceEvent.TurnID,
		traceEvent.EventType,
		traceEvent.Title,
		traceEvent.Body,
		traceEvent.Intent,
		rawTraceEvidenceRefs,
	); err != nil {
		return AgentConversationTurn{}, fmt.Errorf("insert agent trace event: %w", err)
	}

	turn.TraceEvents = []AgentTraceEvent{traceEvent}
	return turn, nil
}

func conversationEvidenceRefs(signals []RiskSignal) []string {
	refs := []string{}
	for _, signal := range signals {
		refs = append(refs, signal.EvidenceRefs...)
	}
	return listUnique(refs)
}

func listUnique(values []string) []string {
	seen := map[string]struct{}{}
	unique := []string{}
	for _, value := range values {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}
	return unique
}

func (store *Store) CreateActionSuggestion(
	ctx context.Context,
	input ActionSuggestionInput,
) (ActionSuggestion, error) {
	evidenceRefs, err := json.Marshal(input.EvidenceRefs)
	if err != nil {
		return ActionSuggestion{}, fmt.Errorf("marshal action evidence refs: %w", err)
	}

	suggestionID := fmt.Sprintf("action_%d", time.Now().UTC().UnixNano())
	_, err = store.pool.Exec(
		ctx,
		`
		INSERT INTO action_suggestions (
			id,
			project_id,
			agent_artifact_id,
			action_type,
			status,
			target_ref,
			draft_body,
			evidence_refs
		)
		VALUES ($1, $2, NULLIF($3, ''), $4, 'pending_user_confirmation', $5, $6, $7)
		`,
		suggestionID,
		input.ProjectID,
		input.AgentArtifactID,
		input.ActionType,
		input.TargetRef,
		input.DraftBody,
		evidenceRefs,
	)
	if err != nil {
		return ActionSuggestion{}, fmt.Errorf("create action suggestion: %w", err)
	}

	return store.ActionSuggestion(ctx, suggestionID)
}

func (store *Store) ConfirmActionSuggestion(
	ctx context.Context,
	suggestionID string,
) (ActionSuggestion, error) {
	if _, err := store.pool.Exec(
		ctx,
		`
		UPDATE action_suggestions
		SET status = 'succeeded',
		    updated_at = now()
		WHERE id = $1
		  AND status IN ('drafted', 'pending_user_confirmation', 'failed')
		`,
		suggestionID,
	); err != nil {
		return ActionSuggestion{}, fmt.Errorf("confirm action suggestion: %w", err)
	}

	return store.ActionSuggestion(ctx, suggestionID)
}

func (store *Store) ListActionSuggestionsByProject(
	ctx context.Context,
	projectID string,
) ([]ActionSuggestion, error) {
	rows, err := store.pool.Query(
		ctx,
		`
		SELECT
			id,
			project_id,
			action_type,
			status,
			target_ref,
			draft_body,
			evidence_refs
		FROM action_suggestions
		WHERE project_id = $1
		ORDER BY created_at DESC, id DESC
		`,
		projectID,
	)
	if err != nil {
		return nil, fmt.Errorf("list action suggestions: %w", err)
	}
	defer rows.Close()

	suggestions := []ActionSuggestion{}
	for rows.Next() {
		var suggestion ActionSuggestion
		var rawEvidenceRefs []byte
		if err := rows.Scan(
			&suggestion.ID,
			&suggestion.ProjectID,
			&suggestion.ActionType,
			&suggestion.Status,
			&suggestion.TargetRef,
			&suggestion.DraftBody,
			&rawEvidenceRefs,
		); err != nil {
			return nil, fmt.Errorf("scan action suggestion: %w", err)
		}
		if err := json.Unmarshal(rawEvidenceRefs, &suggestion.EvidenceRefs); err != nil {
			return nil, fmt.Errorf("decode action evidence refs: %w", err)
		}
		suggestions = append(suggestions, suggestion)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate action suggestions: %w", err)
	}

	return suggestions, nil
}

func (store *Store) ActionSuggestion(
	ctx context.Context,
	suggestionID string,
) (ActionSuggestion, error) {
	var suggestion ActionSuggestion
	var rawEvidenceRefs []byte
	if err := store.pool.QueryRow(
		ctx,
		`
		SELECT
			id,
			project_id,
			action_type,
			status,
			target_ref,
			draft_body,
			evidence_refs
		FROM action_suggestions
		WHERE id = $1
		`,
		suggestionID,
	).Scan(
		&suggestion.ID,
		&suggestion.ProjectID,
		&suggestion.ActionType,
		&suggestion.Status,
		&suggestion.TargetRef,
		&suggestion.DraftBody,
		&rawEvidenceRefs,
	); err != nil {
		return ActionSuggestion{}, fmt.Errorf("load action suggestion: %w", err)
	}

	if err := json.Unmarshal(rawEvidenceRefs, &suggestion.EvidenceRefs); err != nil {
		return ActionSuggestion{}, fmt.Errorf("decode action evidence refs: %w", err)
	}

	return suggestion, nil
}

func (store *Store) CreateAgentJob(
	ctx context.Context,
	riskAssessmentID string,
	agentType string,
	trigger string,
) (AgentJob, error) {
	var projectID string
	if err := store.pool.QueryRow(
		ctx,
		`SELECT project_id FROM risk_assessments WHERE id = $1`,
		riskAssessmentID,
	).Scan(&projectID); err != nil {
		return AgentJob{}, fmt.Errorf("load risk assessment for agent job: %w", err)
	}

	jobID := fmt.Sprintf("job_%d", time.Now().UTC().UnixNano())
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return AgentJob{}, fmt.Errorf("begin create agent job: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	if _, err := tx.Exec(
		ctx,
		`
		INSERT INTO agent_jobs (
			id,
			project_id,
			risk_assessment_id,
			agent_type,
			status,
			trigger
		)
		VALUES ($1, $2, $3, $4, 'queued', $5)
		`,
		jobID,
		projectID,
		riskAssessmentID,
		agentType,
		trigger,
	); err != nil {
		return AgentJob{}, fmt.Errorf("create agent job: %w", err)
	}

	runID := "run_" + jobID
	if _, err := tx.Exec(
		ctx,
		`
		INSERT INTO agent_runs (
			id,
			agent_job_id,
			project_id,
			risk_assessment_id,
			agent_type,
			status
		)
		VALUES ($1, $2, $3, $4, $5, 'queued')
		`,
		runID,
		jobID,
		projectID,
		riskAssessmentID,
		agentType,
	); err != nil {
		return AgentJob{}, fmt.Errorf("create agent run: %w", err)
	}
	if err := insertAgentStep(
		ctx,
		tx,
		runID,
		"queued",
		"queued",
		"Agent 已进入调查队列",
		"系统已根据风险评估创建 Agent 调查任务。",
		nil,
	); err != nil {
		return AgentJob{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AgentJob{}, fmt.Errorf("commit create agent job: %w", err)
	}

	return AgentJob{
		JobID:            jobID,
		ProjectID:        projectID,
		RiskAssessmentID: riskAssessmentID,
		AgentType:        agentType,
		Trigger:          trigger,
		Status:           "queued",
	}, nil
}

func (store *Store) ClaimNextAgentJob(ctx context.Context) (AgentJob, bool, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return AgentJob{}, false, fmt.Errorf("begin claim agent job: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	var job AgentJob
	err = tx.QueryRow(
		ctx,
		`
		UPDATE agent_jobs
		SET status = 'running',
		    updated_at = now()
		WHERE id = (
			SELECT id
			FROM agent_jobs
			WHERE status = 'queued'
			ORDER BY created_at ASC
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING id, project_id, risk_assessment_id, agent_type, trigger, status
		`,
	).Scan(
		&job.JobID,
		&job.ProjectID,
		&job.RiskAssessmentID,
		&job.AgentType,
		&job.Trigger,
		&job.Status,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return AgentJob{}, false, nil
		}
		return AgentJob{}, false, fmt.Errorf("claim agent job: %w", err)
	}

	runID := "run_" + job.JobID
	if _, err := tx.Exec(
		ctx,
		`
		UPDATE agent_runs
		SET status = 'running',
		    updated_at = now()
		WHERE agent_job_id = $1
		`,
		job.JobID,
	); err != nil {
		return AgentJob{}, false, fmt.Errorf("mark agent run running: %w", err)
	}
	if err := insertAgentStep(
		ctx,
		tx,
		runID,
		"running",
		"running",
		"Agent 开始调查风险",
		"Agent 已领取任务，正在读取证据包并形成判断。",
		nil,
	); err != nil {
		return AgentJob{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AgentJob{}, false, fmt.Errorf("commit claim agent job: %w", err)
	}

	return job, true, nil
}

func (store *Store) CompleteAgentJob(
	ctx context.Context,
	jobID string,
	input AgentArtifactInput,
) (AgentJob, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return AgentJob{}, fmt.Errorf("begin complete agent job: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	var job AgentJob
	if err := tx.QueryRow(
		ctx,
		`
		UPDATE agent_jobs
		SET status = 'succeeded',
		    updated_at = now()
		WHERE id = $1
		RETURNING id, project_id, risk_assessment_id, agent_type, trigger, status
		`,
		jobID,
	).Scan(
		&job.JobID,
		&job.ProjectID,
		&job.RiskAssessmentID,
		&job.AgentType,
		&job.Trigger,
		&job.Status,
	); err != nil {
		return AgentJob{}, fmt.Errorf("mark agent job succeeded: %w", err)
	}

	artifactPayload, err := json.Marshal(map[string]string{
		"summary": input.Summary,
	})
	if err != nil {
		return AgentJob{}, fmt.Errorf("marshal agent artifact: %w", err)
	}
	evidenceRefs, err := json.Marshal(input.EvidenceRefs)
	if err != nil {
		return AgentJob{}, fmt.Errorf("marshal artifact evidence refs: %w", err)
	}

	artifactID := "artifact_" + jobID
	if _, err := tx.Exec(
		ctx,
		`
		INSERT INTO agent_artifacts (
			id,
			agent_job_id,
			project_id,
			artifact,
			evidence_refs,
			model,
			prompt_version,
			token_usage
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, '{}'::jsonb)
		`,
		artifactID,
		job.JobID,
		job.ProjectID,
		artifactPayload,
		evidenceRefs,
		input.Model,
		input.PromptVersion,
	); err != nil {
		return AgentJob{}, fmt.Errorf("insert agent artifact: %w", err)
	}

	for index, suggestion := range input.ActionSuggestions {
		suggestionEvidenceRefs, err := json.Marshal(suggestion.EvidenceRefs)
		if err != nil {
			return AgentJob{}, fmt.Errorf("marshal action evidence refs: %w", err)
		}

		suggestionID := fmt.Sprintf("action_%s_%d", jobID, index+1)
		if _, err := tx.Exec(
			ctx,
			`
			INSERT INTO action_suggestions (
				id,
				project_id,
				agent_artifact_id,
				action_type,
				status,
				target_ref,
				draft_body,
				evidence_refs
			)
			VALUES ($1, $2, $3, $4, 'pending_user_confirmation', $5, $6, $7)
			`,
			suggestionID,
			job.ProjectID,
			artifactID,
			suggestion.ActionType,
			suggestion.TargetRef,
			suggestion.DraftBody,
			suggestionEvidenceRefs,
		); err != nil {
			return AgentJob{}, fmt.Errorf("insert action suggestion: %w", err)
		}

		job.ActionSuggestionIDs = append(job.ActionSuggestionIDs, suggestionID)
	}

	runID := "run_" + job.JobID
	if _, err := tx.Exec(
		ctx,
		`
		UPDATE agent_runs
		SET status = 'succeeded',
		    summary = $2,
		    updated_at = now()
		WHERE agent_job_id = $1
		`,
		job.JobID,
		input.Summary,
	); err != nil {
		return AgentJob{}, fmt.Errorf("mark agent run succeeded: %w", err)
	}
	if err := insertAgentStep(
		ctx,
		tx,
		runID,
		"completed",
		"succeeded",
		"Agent 完成风险判断",
		input.Summary,
		input.EvidenceRefs,
	); err != nil {
		return AgentJob{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return AgentJob{}, fmt.Errorf("commit complete agent job: %w", err)
	}

	return job, nil
}

func insertAgentStep(
	ctx context.Context,
	tx pgx.Tx,
	agentRunID string,
	stepType string,
	status string,
	title string,
	body string,
	evidenceRefs []string,
) error {
	if evidenceRefs == nil {
		evidenceRefs = []string{}
	}
	rawEvidenceRefs, err := json.Marshal(evidenceRefs)
	if err != nil {
		return fmt.Errorf("marshal agent step evidence refs: %w", err)
	}
	stepID := fmt.Sprintf("step_%s_%d", agentRunID, time.Now().UTC().UnixNano())
	if _, err := tx.Exec(
		ctx,
		`
		INSERT INTO agent_steps (
			id,
			agent_run_id,
			step_type,
			status,
			title,
			body,
			evidence_refs
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		`,
		stepID,
		agentRunID,
		stepType,
		status,
		title,
		body,
		rawEvidenceRefs,
	); err != nil {
		return fmt.Errorf("insert agent step: %w", err)
	}
	return nil
}

func (store *Store) ListAgentRunsByProject(ctx context.Context, projectID string) ([]AgentRun, error) {
	rows, err := store.pool.Query(
		ctx,
		`
		SELECT
			id,
			agent_job_id,
			project_id,
			risk_assessment_id,
			agent_type,
			status,
			summary
		FROM agent_runs
		WHERE project_id = $1
		ORDER BY created_at DESC, id DESC
		`,
		projectID,
	)
	if err != nil {
		return nil, fmt.Errorf("list agent runs: %w", err)
	}
	defer rows.Close()

	runs := []AgentRun{}
	for rows.Next() {
		var run AgentRun
		if err := rows.Scan(
			&run.ID,
			&run.AgentJobID,
			&run.ProjectID,
			&run.RiskAssessmentID,
			&run.AgentType,
			&run.Status,
			&run.Summary,
		); err != nil {
			return nil, fmt.Errorf("scan agent run: %w", err)
		}
		steps, err := store.listAgentSteps(ctx, run.ID)
		if err != nil {
			return nil, err
		}
		run.Steps = steps
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate agent runs: %w", err)
	}

	return runs, nil
}

func (store *Store) listAgentSteps(ctx context.Context, agentRunID string) ([]AgentStep, error) {
	rows, err := store.pool.Query(
		ctx,
		`
		SELECT
			id,
			agent_run_id,
			step_type,
			status,
			title,
			body,
			evidence_refs
		FROM agent_steps
		WHERE agent_run_id = $1
		ORDER BY created_at ASC, id ASC
		`,
		agentRunID,
	)
	if err != nil {
		return nil, fmt.Errorf("list agent steps: %w", err)
	}
	defer rows.Close()

	steps := []AgentStep{}
	for rows.Next() {
		var step AgentStep
		var rawEvidenceRefs []byte
		if err := rows.Scan(
			&step.ID,
			&step.AgentRunID,
			&step.StepType,
			&step.Status,
			&step.Title,
			&step.Body,
			&rawEvidenceRefs,
		); err != nil {
			return nil, fmt.Errorf("scan agent step: %w", err)
		}
		if err := json.Unmarshal(rawEvidenceRefs, &step.EvidenceRefs); err != nil {
			return nil, fmt.Errorf("decode agent step evidence refs: %w", err)
		}
		steps = append(steps, step)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate agent steps: %w", err)
	}

	return steps, nil
}

func encryptAPIKey(apiKey string) (string, error) {
	gcm, err := apiKeyCipher()
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("create api key nonce: %w", err)
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(apiKey), nil)
	return "aes-gcm:" + base64.StdEncoding.EncodeToString(ciphertext), nil
}

func decryptAPIKey(ciphertext string) (string, error) {
	const prefix = "aes-gcm:"
	if len(ciphertext) <= len(prefix) || ciphertext[:len(prefix)] != prefix {
		return "", fmt.Errorf("unsupported api key ciphertext format")
	}

	rawCiphertext, err := base64.StdEncoding.DecodeString(ciphertext[len(prefix):])
	if err != nil {
		return "", fmt.Errorf("decode api key ciphertext: %w", err)
	}

	gcm, err := apiKeyCipher()
	if err != nil {
		return "", err
	}
	if len(rawCiphertext) < gcm.NonceSize() {
		return "", fmt.Errorf("api key ciphertext is too short")
	}

	nonce := rawCiphertext[:gcm.NonceSize()]
	encryptedAPIKey := rawCiphertext[gcm.NonceSize():]
	apiKey, err := gcm.Open(nil, nonce, encryptedAPIKey, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt api key: %w", err)
	}
	return string(apiKey), nil
}

func apiKeyCipher() (cipher.AEAD, error) {
	secret := os.Getenv("DEV_TIME_LLM_KEY_SECRET")
	if secret == "" {
		secret = "dev-time-local-development-key"
	}

	key := sha256.Sum256([]byte(secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("create api key cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create api key gcm: %w", err)
	}
	return gcm, nil
}

func lastFour(value string) string {
	if len(value) <= 4 {
		return value
	}
	return value[len(value)-4:]
}
