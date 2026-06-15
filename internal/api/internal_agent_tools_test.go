package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/henry-insomniac/dev-time-server/internal/api"
	"github.com/henry-insomniac/dev-time-server/internal/testsupport"
)

func TestInternalAgentToolsExposeProjectCIPRAndDraftSuggestion(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	store := testsupport.NewMigratedStore(t, ctx)
	router := api.NewRouter(api.Dependencies{Store: store})
	projectID, assessmentID := createProjectRisk(t, router)
	performWebhookRequest(
		router,
		"pull-request-agent-tool-1",
		"pull_request",
		[]byte(`{
			"repository": {
				"id": 1001,
				"name": "dev-time",
				"full_name": "henry-insomniac/dev-time",
				"owner": { "login": "henry-insomniac" }
			},
			"pull_request": {
				"number": 18,
				"title": "Add agent tool layer",
				"state": "open",
				"html_url": "https://github.com/henry-insomniac/dev-time/pull/18"
			}
		}`),
	)

	statusResponse := performJSONRequest(
		router,
		http.MethodGet,
		"/internal/risk-assessments/"+assessmentID+"/project-status",
		nil,
	)
	if statusResponse.Code != http.StatusOK {
		t.Fatalf("expected project status 200, got %d: %s", statusResponse.Code, statusResponse.Body.String())
	}
	var statusBody struct {
		Project struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"project"`
		Assessment struct {
			ID    string `json:"id"`
			Level string `json:"level"`
			Score int    `json:"score"`
		} `json:"assessment"`
		TopRiskReason string   `json:"top_risk_reason"`
		EvidenceRefs  []string `json:"evidence_refs"`
	}
	if err := json.NewDecoder(statusResponse.Body).Decode(&statusBody); err != nil {
		t.Fatalf("decode project status: %v", err)
	}
	if statusBody.Project.ID != projectID || statusBody.Assessment.ID != assessmentID {
		t.Fatalf("expected project and assessment identity, got %#v", statusBody)
	}
	if statusBody.Assessment.Level != "high" || statusBody.TopRiskReason == "" {
		t.Fatalf("expected high risk project status, got %#v", statusBody)
	}

	ciResponse := performJSONRequest(
		router,
		http.MethodGet,
		"/internal/risk-assessments/"+assessmentID+"/ci-checks",
		nil,
	)
	if ciResponse.Code != http.StatusOK {
		t.Fatalf("expected ci checks 200, got %d: %s", ciResponse.Code, ciResponse.Body.String())
	}
	var ciBody struct {
		Checks []struct {
			EvidenceRef string `json:"evidence_ref"`
			Name        string `json:"name"`
			Conclusion  string `json:"conclusion"`
		} `json:"checks"`
	}
	if err := json.NewDecoder(ciResponse.Body).Decode(&ciBody); err != nil {
		t.Fatalf("decode ci checks: %v", err)
	}
	if len(ciBody.Checks) != 1 ||
		ciBody.Checks[0].EvidenceRef != "event_check-run-conversation-1" ||
		ciBody.Checks[0].Conclusion != "failure" {
		t.Fatalf("expected failed check run, got %#v", ciBody.Checks)
	}

	prResponse := performJSONRequest(
		router,
		http.MethodGet,
		"/internal/risk-assessments/"+assessmentID+"/pull-requests",
		nil,
	)
	if prResponse.Code != http.StatusOK {
		t.Fatalf("expected pull requests 200, got %d: %s", prResponse.Code, prResponse.Body.String())
	}
	var prBody struct {
		PullRequests []struct {
			EvidenceRef string `json:"evidence_ref"`
			Number      int    `json:"number"`
			Title       string `json:"title"`
		} `json:"pull_requests"`
	}
	if err := json.NewDecoder(prResponse.Body).Decode(&prBody); err != nil {
		t.Fatalf("decode pull requests: %v", err)
	}
	if len(prBody.PullRequests) != 1 || prBody.PullRequests[0].Number != 18 {
		t.Fatalf("expected related PR #18, got %#v", prBody.PullRequests)
	}

	draftResponse := performJSONRequest(
		router,
		http.MethodPost,
		"/internal/action-suggestions",
		[]byte(`{
			"project_id": "`+projectID+`",
			"action_type": "pr_comment",
			"target_ref": "pull_request:18",
			"draft_body": "go test 失败阻塞交付，请先修复后再继续合并。",
			"evidence_refs": ["event_check-run-conversation-1"]
		}`),
	)
	if draftResponse.Code != http.StatusCreated {
		t.Fatalf("expected action suggestion draft 201, got %d: %s", draftResponse.Code, draftResponse.Body.String())
	}
	var draftBody struct {
		ID        string `json:"id"`
		Status    string `json:"status"`
		TargetRef string `json:"target_ref"`
	}
	if err := json.NewDecoder(draftResponse.Body).Decode(&draftBody); err != nil {
		t.Fatalf("decode draft action suggestion: %v", err)
	}
	if draftBody.ID == "" || draftBody.Status != "pending_user_confirmation" ||
		draftBody.TargetRef != "pull_request:18" {
		t.Fatalf("expected pending action suggestion draft, got %#v", draftBody)
	}
}

func TestInternalGitHubAuthStatusReportsImportedRepositoryAccess(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	store := testsupport.NewMigratedStore(t, ctx)
	router := api.NewRouter(api.Dependencies{Store: store})
	projectID := importProject(t, router, 1001, "dev-time-server")

	response := performJSONRequest(
		router,
		http.MethodGet,
		"/internal/github/auth-status",
		nil,
	)
	if response.Code != http.StatusOK {
		t.Fatalf("expected github auth status 200, got %d: %s", response.Code, response.Body.String())
	}

	var body struct {
		Connected    bool `json:"connected"`
		Repositories []struct {
			ID        string `json:"id"`
			ProjectID string `json:"project_id"`
			Name      string `json:"name"`
			FullName  string `json:"full_name"`
		} `json:"repositories"`
		Permissions []string `json:"permissions"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode github auth status: %v", err)
	}
	if !body.Connected {
		t.Fatalf("expected imported GitHub repository access to be connected, got %#v", body)
	}
	if len(body.Repositories) != 1 ||
		body.Repositories[0].ProjectID != projectID ||
		body.Repositories[0].Name != "dev-time-server" {
		t.Fatalf("expected imported repository in github status, got %#v", body.Repositories)
	}
	if len(body.Permissions) == 0 {
		t.Fatalf("expected explicit github permissions, got %#v", body.Permissions)
	}

	reposResponse := performJSONRequest(
		router,
		http.MethodGet,
		"/internal/github/repositories",
		nil,
	)
	if reposResponse.Code != http.StatusOK {
		t.Fatalf("expected github repositories 200, got %d: %s", reposResponse.Code, reposResponse.Body.String())
	}

	var reposBody struct {
		Repositories []struct {
			ID        string `json:"id"`
			ProjectID string `json:"project_id"`
			FullName  string `json:"full_name"`
		} `json:"repositories"`
	}
	if err := json.NewDecoder(reposResponse.Body).Decode(&reposBody); err != nil {
		t.Fatalf("decode github repositories: %v", err)
	}
	if len(reposBody.Repositories) != 1 ||
		reposBody.Repositories[0].ProjectID != projectID ||
		reposBody.Repositories[0].FullName != "henry-insomniac/dev-time-server" {
		t.Fatalf("expected imported github repository list, got %#v", reposBody.Repositories)
	}
}

func TestInternalGitHubRepositoryPullRequestsListFromEvents(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	store := testsupport.NewMigratedStore(t, ctx)
	router := api.NewRouter(api.Dependencies{Store: store})
	importProject(t, router, 1001, "dev-time-agent")
	performWebhookRequest(
		router,
		"pull-request-18",
		"pull_request",
		[]byte(`{
			"action": "opened",
			"repository": {
				"id": 1001,
				"name": "dev-time-agent",
				"full_name": "henry-insomniac/dev-time-agent",
				"owner": { "login": "henry-insomniac" }
			},
			"pull_request": {
				"number": 18,
				"title": "Add GitHub tool layer",
				"state": "open",
				"html_url": "https://github.com/henry-insomniac/dev-time-agent/pull/18"
			}
		}`),
	)

	response := performJSONRequest(
		router,
		http.MethodGet,
		"/internal/github/repositories/repo_1001/pull-requests",
		nil,
	)
	if response.Code != http.StatusOK {
		t.Fatalf("expected github pull requests 200, got %d: %s", response.Code, response.Body.String())
	}

	var body struct {
		PullRequests []struct {
			EvidenceRef string `json:"evidence_ref"`
			Number      int    `json:"number"`
			Title       string `json:"title"`
			State       string `json:"state"`
			URL         string `json:"url"`
		} `json:"pull_requests"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode github pull requests: %v", err)
	}
	if len(body.PullRequests) != 1 ||
		body.PullRequests[0].EvidenceRef != "event_pull-request-18" ||
		body.PullRequests[0].Number != 18 ||
		body.PullRequests[0].Title != "Add GitHub tool layer" ||
		body.PullRequests[0].State != "open" {
		t.Fatalf("expected pull request from github event store, got %#v", body.PullRequests)
	}
}

func TestInternalGitHubRepositoryPullRequestsFallBackToLiveGitHub(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	privateKeyPath := writeTestGitHubAppPrivateKey(t)
	var installationRequested bool
	var tokenRequested bool
	var pullRequestsRequested bool
	githubServer := httptest.NewServer(http.HandlerFunc(func(
		response http.ResponseWriter,
		request *http.Request,
	) {
		switch {
		case request.Method == http.MethodGet &&
			request.URL.Path == "/repos/henry-insomniac/dev-time-agent/installation":
			installationRequested = true
			if !strings.HasPrefix(request.Header.Get("Authorization"), "Bearer ") {
				t.Fatalf("expected app JWT authorization header, got %q", request.Header.Get("Authorization"))
			}
			writeTestJSON(response, map[string]any{"id": 123})
		case request.Method == http.MethodPost &&
			request.URL.Path == "/app/installations/123/access_tokens":
			tokenRequested = true
			writeTestJSON(response, map[string]any{"token": "installation-token"})
		case request.Method == http.MethodGet &&
			request.URL.Path == "/repos/henry-insomniac/dev-time-agent/pulls":
			pullRequestsRequested = true
			if request.Header.Get("Authorization") != "Bearer installation-token" {
				t.Fatalf("expected installation token authorization, got %q", request.Header.Get("Authorization"))
			}
			writeTestJSON(response, []map[string]any{
				{
					"number":   18,
					"title":    "Add GitHub tool layer",
					"state":    "open",
					"html_url": "https://github.com/henry-insomniac/dev-time-agent/pull/18",
				},
			})
		default:
			t.Fatalf("unexpected github request %s %s", request.Method, request.URL.Path)
		}
	}))
	defer githubServer.Close()

	store := testsupport.NewMigratedStore(t, ctx)
	router := api.NewRouter(api.Dependencies{
		Store: store,
		GitHubApp: api.GitHubAppConfig{
			AppID:            "12345",
			AppSlug:          "dev-time-test",
			PrivateKeyPath:   privateKeyPath,
			SetupStateSecret: "state-secret",
			APIBaseURL:       githubServer.URL,
		},
	})
	importProject(t, router, 1001, "dev-time-agent")

	response := performJSONRequest(
		router,
		http.MethodGet,
		"/internal/github/repositories/repo_1001/pull-requests",
		nil,
	)
	if response.Code != http.StatusOK {
		t.Fatalf("expected github pull requests 200, got %d: %s", response.Code, response.Body.String())
	}

	var body struct {
		PullRequests []struct {
			EvidenceRef string `json:"evidence_ref"`
			Number      int    `json:"number"`
			Title       string `json:"title"`
			State       string `json:"state"`
			URL         string `json:"url"`
		} `json:"pull_requests"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode github pull requests: %v", err)
	}
	if len(body.PullRequests) != 1 ||
		body.PullRequests[0].EvidenceRef != "github_live_pull_request_18" ||
		body.PullRequests[0].Number != 18 ||
		body.PullRequests[0].Title != "Add GitHub tool layer" ||
		body.PullRequests[0].State != "open" {
		t.Fatalf("expected pull request from live github, got %#v", body.PullRequests)
	}
	if !installationRequested || !tokenRequested || !pullRequestsRequested {
		t.Fatalf(
			"expected installation, token, and pull requests requests, installation=%v token=%v pullRequests=%v",
			installationRequested,
			tokenRequested,
			pullRequestsRequested,
		)
	}
}

func TestInternalGitHubRepositoryIssuesListFromEvents(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	store := testsupport.NewMigratedStore(t, ctx)
	router := api.NewRouter(api.Dependencies{Store: store})
	importProject(t, router, 1001, "dev-time-agent")
	performWebhookRequest(
		router,
		"issue-42",
		"issues",
		[]byte(`{
			"action": "opened",
			"repository": {
				"id": 1001,
				"name": "dev-time-agent",
				"full_name": "henry-insomniac/dev-time-agent",
				"owner": { "login": "henry-insomniac" }
			},
			"issue": {
				"number": 42,
				"title": "Add issue reader",
				"state": "open",
				"html_url": "https://github.com/henry-insomniac/dev-time-agent/issues/42"
			}
		}`),
	)

	response := performJSONRequest(
		router,
		http.MethodGet,
		"/internal/github/repositories/repo_1001/issues",
		nil,
	)
	if response.Code != http.StatusOK {
		t.Fatalf("expected github issues 200, got %d: %s", response.Code, response.Body.String())
	}

	var body struct {
		Issues []struct {
			EvidenceRef string `json:"evidence_ref"`
			Number      int    `json:"number"`
			Title       string `json:"title"`
			State       string `json:"state"`
			URL         string `json:"url"`
		} `json:"issues"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode github issues: %v", err)
	}
	if len(body.Issues) != 1 ||
		body.Issues[0].EvidenceRef != "event_issue-42" ||
		body.Issues[0].Number != 42 ||
		body.Issues[0].Title != "Add issue reader" ||
		body.Issues[0].State != "open" {
		t.Fatalf("expected issue from github event store, got %#v", body.Issues)
	}
}

func TestInternalGitHubRepositoryChecksListFromEvents(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	store := testsupport.NewMigratedStore(t, ctx)
	router := api.NewRouter(api.Dependencies{Store: store})
	importProject(t, router, 1001, "dev-time-agent")
	performWebhookRequest(
		router,
		"check-run-421",
		"check_run",
		[]byte(`{
			"repository": {
				"id": 1001,
				"name": "dev-time-agent",
				"full_name": "henry-insomniac/dev-time-agent",
				"owner": { "login": "henry-insomniac" }
			},
			"check_run": {
				"id": 421,
				"name": "test",
				"status": "completed",
				"conclusion": "failure",
				"html_url": "https://github.com/henry-insomniac/dev-time-agent/actions/runs/421"
			}
		}`),
	)

	response := performJSONRequest(
		router,
		http.MethodGet,
		"/internal/github/repositories/repo_1001/checks",
		nil,
	)
	if response.Code != http.StatusOK {
		t.Fatalf("expected github checks 200, got %d: %s", response.Code, response.Body.String())
	}

	var body struct {
		Checks []struct {
			EvidenceRef string `json:"evidence_ref"`
			Name        string `json:"name"`
			Status      string `json:"status"`
			Conclusion  string `json:"conclusion"`
			URL         string `json:"url"`
		} `json:"checks"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode github checks: %v", err)
	}
	if len(body.Checks) != 1 ||
		body.Checks[0].EvidenceRef != "event_check-run-421" ||
		body.Checks[0].Name != "test" ||
		body.Checks[0].Status != "completed" ||
		body.Checks[0].Conclusion != "failure" {
		t.Fatalf("expected check from github event store, got %#v", body.Checks)
	}
}

func TestGitHubSettingsExposeConnectionStatus(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	store := testsupport.NewMigratedStore(t, ctx)
	router := api.NewRouter(api.Dependencies{Store: store})
	importProject(t, router, 1001, "dev-time-server")

	response := performJSONRequest(
		router,
		http.MethodGet,
		"/api/settings/github",
		nil,
	)
	if response.Code != http.StatusOK {
		t.Fatalf("expected github settings 200, got %d: %s", response.Code, response.Body.String())
	}

	var body struct {
		Connected    bool `json:"connected"`
		Repositories []struct {
			FullName string `json:"full_name"`
		} `json:"repositories"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode github settings: %v", err)
	}
	if !body.Connected ||
		len(body.Repositories) != 1 ||
		body.Repositories[0].FullName != "henry-insomniac/dev-time-server" {
		t.Fatalf("expected visible github connection settings, got %#v", body)
	}
}

func TestGitHubSettingsListDiscoveredRepositoriesBeforeProjectLoad(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	store := testsupport.NewMigratedStore(t, ctx)
	router := api.NewRouter(api.Dependencies{Store: store})

	discoverResponse := performJSONRequest(
		router,
		http.MethodPost,
		"/api/settings/github/repositories/discover",
		[]byte(`{
			"repositories": [
				{
					"github_id": 1001,
					"owner": "henry-insomniac",
					"name": "dev-time-server",
					"full_name": "henry-insomniac/dev-time-server"
				},
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

	settingsResponse := performJSONRequest(
		router,
		http.MethodGet,
		"/api/settings/github",
		nil,
	)
	if settingsResponse.Code != http.StatusOK {
		t.Fatalf("expected github settings 200, got %d: %s", settingsResponse.Code, settingsResponse.Body.String())
	}

	var body struct {
		Connected    bool `json:"connected"`
		Repositories []struct {
			FullName  string  `json:"full_name"`
			ProjectID *string `json:"project_id"`
		} `json:"repositories"`
	}
	if err := json.NewDecoder(settingsResponse.Body).Decode(&body); err != nil {
		t.Fatalf("decode github settings: %v", err)
	}
	if !body.Connected {
		t.Fatalf("expected discovered repositories to mark github connected, got %#v", body)
	}
	if len(body.Repositories) != 2 {
		t.Fatalf("expected all discovered repositories in settings, got %#v", body.Repositories)
	}
	if body.Repositories[0].ProjectID != nil || body.Repositories[1].ProjectID != nil {
		t.Fatalf("expected discovered repositories to be unloaded projects, got %#v", body.Repositories)
	}
}

func TestGitHubSettingsCanLoadDiscoveredRepositoryAsProject(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	store := testsupport.NewMigratedStore(t, ctx)
	router := api.NewRouter(api.Dependencies{Store: store})

	discoverResponse := performJSONRequest(
		router,
		http.MethodPost,
		"/api/settings/github/repositories/discover",
		[]byte(`{
			"repositories": [
				{
					"github_id": 1001,
					"owner": "henry-insomniac",
					"name": "dev-time-server",
					"full_name": "henry-insomniac/dev-time-server"
				}
			]
		}`),
	)
	if discoverResponse.Code != http.StatusOK {
		t.Fatalf("expected discover repositories 200, got %d: %s", discoverResponse.Code, discoverResponse.Body.String())
	}

	loadResponse := performJSONRequest(
		router,
		http.MethodPost,
		"/api/settings/github/repositories/repo_1001/load-project",
		nil,
	)
	if loadResponse.Code != http.StatusCreated {
		t.Fatalf("expected load project 201, got %d: %s", loadResponse.Code, loadResponse.Body.String())
	}

	var loadBody struct {
		Project struct {
			ID           string `json:"id"`
			RepositoryID string `json:"repository_id"`
			Name         string `json:"name"`
		} `json:"project"`
		Repository struct {
			ID        string  `json:"id"`
			ProjectID *string `json:"project_id"`
		} `json:"repository"`
	}
	if err := json.NewDecoder(loadResponse.Body).Decode(&loadBody); err != nil {
		t.Fatalf("decode load project response: %v", err)
	}
	if loadBody.Project.ID != "project_repo_1001" ||
		loadBody.Project.RepositoryID != "repo_1001" ||
		loadBody.Project.Name != "dev-time-server" {
		t.Fatalf("expected loaded project for repository, got %#v", loadBody.Project)
	}
	if loadBody.Repository.ProjectID == nil || *loadBody.Repository.ProjectID != loadBody.Project.ID {
		t.Fatalf("expected repository to include loaded project id, got %#v", loadBody.Repository)
	}

	settingsResponse := performJSONRequest(
		router,
		http.MethodGet,
		"/api/settings/github",
		nil,
	)
	if settingsResponse.Code != http.StatusOK {
		t.Fatalf("expected github settings 200, got %d: %s", settingsResponse.Code, settingsResponse.Body.String())
	}

	var settingsBody struct {
		Repositories []struct {
			ID        string  `json:"id"`
			ProjectID *string `json:"project_id"`
		} `json:"repositories"`
	}
	if err := json.NewDecoder(settingsResponse.Body).Decode(&settingsBody); err != nil {
		t.Fatalf("decode github settings: %v", err)
	}
	if len(settingsBody.Repositories) != 1 ||
		settingsBody.Repositories[0].ID != "repo_1001" ||
		settingsBody.Repositories[0].ProjectID == nil ||
		*settingsBody.Repositories[0].ProjectID != "project_repo_1001" {
		t.Fatalf("expected loaded project in github settings, got %#v", settingsBody.Repositories)
	}
}

func TestGitHubSettingsCanToggleRepositoryAnalysis(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	store := testsupport.NewMigratedStore(t, ctx)
	router := api.NewRouter(api.Dependencies{Store: store})
	importProject(t, router, 1001, "dev-time-server")

	response := performJSONRequest(
		router,
		http.MethodPatch,
		"/api/settings/github/repositories/repo_1001/analysis",
		[]byte(`{"analysis_enabled": false}`),
	)
	if response.Code != http.StatusOK {
		t.Fatalf("expected toggle github repository analysis 200, got %d: %s", response.Code, response.Body.String())
	}

	settingsResponse := performJSONRequest(
		router,
		http.MethodGet,
		"/api/settings/github",
		nil,
	)
	if settingsResponse.Code != http.StatusOK {
		t.Fatalf("expected github settings 200, got %d: %s", settingsResponse.Code, settingsResponse.Body.String())
	}

	var body struct {
		Repositories []struct {
			ID              string `json:"id"`
			AnalysisEnabled bool   `json:"analysis_enabled"`
		} `json:"repositories"`
	}
	if err := json.NewDecoder(settingsResponse.Body).Decode(&body); err != nil {
		t.Fatalf("decode github settings: %v", err)
	}
	if len(body.Repositories) != 1 ||
		body.Repositories[0].ID != "repo_1001" ||
		body.Repositories[0].AnalysisEnabled {
		t.Fatalf("expected repository analysis disabled in settings, got %#v", body.Repositories)
	}
}

func TestInternalGitHubRepositoriesIncludeDisabledAnalysisRepository(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	store := testsupport.NewMigratedStore(t, ctx)
	router := api.NewRouter(api.Dependencies{Store: store})
	importProject(t, router, 1001, "dev-time-server")

	toggleResponse := performJSONRequest(
		router,
		http.MethodPatch,
		"/api/settings/github/repositories/repo_1001/analysis",
		[]byte(`{"analysis_enabled": false}`),
	)
	if toggleResponse.Code != http.StatusOK {
		t.Fatalf("expected toggle github repository analysis 200, got %d: %s", toggleResponse.Code, toggleResponse.Body.String())
	}

	reposResponse := performJSONRequest(
		router,
		http.MethodGet,
		"/internal/github/repositories",
		nil,
	)
	if reposResponse.Code != http.StatusOK {
		t.Fatalf("expected github repositories 200, got %d: %s", reposResponse.Code, reposResponse.Body.String())
	}

	var reposBody struct {
		Repositories []struct {
			ID              string `json:"id"`
			AnalysisEnabled bool   `json:"analysis_enabled"`
		} `json:"repositories"`
	}
	if err := json.NewDecoder(reposResponse.Body).Decode(&reposBody); err != nil {
		t.Fatalf("decode github repositories: %v", err)
	}
	if len(reposBody.Repositories) != 1 ||
		reposBody.Repositories[0].ID != "repo_1001" ||
		reposBody.Repositories[0].AnalysisEnabled {
		t.Fatalf("expected disabled analysis repository to remain visible, got %#v", reposBody.Repositories)
	}

	statusResponse := performJSONRequest(
		router,
		http.MethodGet,
		"/internal/github/auth-status",
		nil,
	)
	if statusResponse.Code != http.StatusOK {
		t.Fatalf("expected github auth status 200, got %d: %s", statusResponse.Code, statusResponse.Body.String())
	}

	var statusBody struct {
		Connected bool `json:"connected"`
	}
	if err := json.NewDecoder(statusResponse.Body).Decode(&statusBody); err != nil {
		t.Fatalf("decode github auth status: %v", err)
	}
	if !statusBody.Connected {
		t.Fatalf("expected internal github auth status connected when repository access exists, got %#v", statusBody)
	}
}

func TestGitHubSettingsExposeRepositorySyncStatus(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	store := testsupport.NewMigratedStore(t, ctx)
	router := api.NewRouter(api.Dependencies{Store: store})
	importProject(t, router, 1001, "dev-time-server")

	response := performJSONRequest(
		router,
		http.MethodGet,
		"/api/settings/github",
		nil,
	)
	if response.Code != http.StatusOK {
		t.Fatalf("expected github settings 200, got %d: %s", response.Code, response.Body.String())
	}

	var body struct {
		Repositories []struct {
			ID           string  `json:"id"`
			SyncStatus   string  `json:"sync_status"`
			LastSyncedAt *string `json:"last_synced_at"`
		} `json:"repositories"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode github settings: %v", err)
	}
	if len(body.Repositories) != 1 ||
		body.Repositories[0].ID != "repo_1001" ||
		body.Repositories[0].SyncStatus != "not_synced" {
		t.Fatalf("expected repository not_synced status in settings, got %#v", body.Repositories)
	}
}

func TestGitHubSettingsCanTriggerRepositorySync(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	store := testsupport.NewMigratedStore(t, ctx)
	router := api.NewRouter(api.Dependencies{Store: store})
	importProject(t, router, 1001, "dev-time-server")

	syncResponse := performJSONRequest(
		router,
		http.MethodPost,
		"/api/settings/github/repositories/repo_1001/sync",
		nil,
	)
	if syncResponse.Code != http.StatusAccepted {
		t.Fatalf("expected trigger repository sync 202, got %d: %s", syncResponse.Code, syncResponse.Body.String())
	}

	settingsResponse := performJSONRequest(
		router,
		http.MethodGet,
		"/api/settings/github",
		nil,
	)
	if settingsResponse.Code != http.StatusOK {
		t.Fatalf("expected github settings 200, got %d: %s", settingsResponse.Code, settingsResponse.Body.String())
	}

	var body struct {
		Repositories []struct {
			ID           string  `json:"id"`
			SyncStatus   string  `json:"sync_status"`
			LastSyncedAt *string `json:"last_synced_at"`
		} `json:"repositories"`
	}
	if err := json.NewDecoder(settingsResponse.Body).Decode(&body); err != nil {
		t.Fatalf("decode github settings: %v", err)
	}
	if len(body.Repositories) != 1 ||
		body.Repositories[0].ID != "repo_1001" ||
		body.Repositories[0].SyncStatus != "succeeded" ||
		body.Repositories[0].LastSyncedAt == nil {
		t.Fatalf("expected repository sync succeeded status in settings, got %#v", body.Repositories)
	}
}
