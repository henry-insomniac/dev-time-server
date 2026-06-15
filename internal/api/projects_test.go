package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/henry-insomniac/dev-time-server/internal/api"
	"github.com/henry-insomniac/dev-time-server/internal/testsupport"
)

func TestProjectsAreReturnedByRiskPriority(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	store := testsupport.NewMigratedStore(t, ctx)
	router := api.NewRouter(api.Dependencies{Store: store})

	importProject(t, router, 1001, "dev-time")
	importProject(t, router, 1002, "dev-time-agent")

	webhookResponse := performWebhookRequest(
		router,
		"agent-check-run-1",
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
				"conclusion": "failure"
			}
		}`),
	)
	if webhookResponse.Code != http.StatusAccepted {
		t.Fatalf("expected webhook status 202, got %d: %s", webhookResponse.Code, webhookResponse.Body.String())
	}

	response := performJSONRequest(router, http.MethodGet, "/api/projects", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("expected projects status 200, got %d: %s", response.Code, response.Body.String())
	}

	var body struct {
		Projects []struct {
			ID    string `json:"id"`
			Name  string `json:"name"`
			Risk  int    `json:"risk_score"`
			Level string `json:"risk_level"`
		} `json:"projects"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode projects response: %v", err)
	}

	if len(body.Projects) != 2 {
		t.Fatalf("expected two projects, got %d", len(body.Projects))
	}
	if body.Projects[0].Name != "dev-time-agent" {
		t.Fatalf("expected riskiest project first, got %q", body.Projects[0].Name)
	}
	if body.Projects[0].Risk < body.Projects[1].Risk {
		t.Fatalf("expected descending risk order, got %d before %d", body.Projects[0].Risk, body.Projects[1].Risk)
	}
	if body.Projects[0].Level != "high" {
		t.Fatalf("expected high risk first project, got %q", body.Projects[0].Level)
	}
}

func TestProjectsOnlyIncludeRepositoriesEnabledForAnalysis(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	store := testsupport.NewMigratedStore(t, ctx)
	router := api.NewRouter(api.Dependencies{Store: store})

	importProject(t, router, 1001, "dev-time")
	importProject(t, router, 1002, "dev-time-agent")

	disableResponse := performJSONRequest(
		router,
		http.MethodPatch,
		"/api/settings/github/repositories/repo_1002/analysis",
		[]byte(`{"analysis_enabled": false}`),
	)
	if disableResponse.Code != http.StatusOK {
		t.Fatalf("expected disable repository analysis 200, got %d: %s", disableResponse.Code, disableResponse.Body.String())
	}

	response := performJSONRequest(router, http.MethodGet, "/api/projects", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("expected projects status 200, got %d: %s", response.Code, response.Body.String())
	}

	var body struct {
		Projects []struct {
			Name string `json:"name"`
		} `json:"projects"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode projects response: %v", err)
	}

	if len(body.Projects) != 1 || body.Projects[0].Name != "dev-time" {
		t.Fatalf("expected only enabled analysis repositories in projects, got %#v", body.Projects)
	}
}

func TestProjectsOnlyIncludeLoadedGitHubRepositories(t *testing.T) {
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

	loadResponse := performJSONRequest(
		router,
		http.MethodPost,
		"/api/settings/github/repositories/repo_1002/load-project",
		nil,
	)
	if loadResponse.Code != http.StatusCreated {
		t.Fatalf("expected load project 201, got %d: %s", loadResponse.Code, loadResponse.Body.String())
	}

	projectsResponse := performJSONRequest(router, http.MethodGet, "/api/projects", nil)
	if projectsResponse.Code != http.StatusOK {
		t.Fatalf("expected projects 200, got %d: %s", projectsResponse.Code, projectsResponse.Body.String())
	}

	var body struct {
		Projects []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"projects"`
	}
	if err := json.NewDecoder(projectsResponse.Body).Decode(&body); err != nil {
		t.Fatalf("decode projects response: %v", err)
	}
	if len(body.Projects) != 1 ||
		body.Projects[0].ID != "project_repo_1002" ||
		body.Projects[0].Name != "dev-time-agent" {
		t.Fatalf("expected only loaded repository in projects, got %#v", body.Projects)
	}
}

func importProject(t *testing.T, router http.Handler, githubID int64, name string) string {
	t.Helper()

	response := performJSONRequest(
		router,
		http.MethodPost,
		"/api/github/repositories/import",
		[]byte(`{
			"github_id": `+jsonNumber(githubID)+`,
			"owner": "henry-insomniac",
			"name": "`+name+`",
			"full_name": "henry-insomniac/`+name+`"
		}`),
	)
	if response.Code != http.StatusCreated {
		t.Fatalf("expected import status 201, got %d: %s", response.Code, response.Body.String())
	}

	return decodeProjectID(t, response)
}

func jsonNumber(value int64) string {
	return strconv.FormatInt(value, 10)
}
