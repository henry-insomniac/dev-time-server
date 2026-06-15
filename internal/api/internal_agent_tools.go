package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/henry-insomniac/dev-time-server/internal/db"
)

func (server server) handleInternalProjectStatus(response http.ResponseWriter, request *http.Request) {
	bundle, ok := server.loadInternalEvidenceBundle(response, request)
	if !ok {
		return
	}

	topRiskReason := "暂无活跃风险信号"
	evidenceRefs := []string{}
	if len(bundle.Signals) > 0 {
		topRiskReason = bundle.Signals[0].Reason
		evidenceRefs = bundle.Signals[0].EvidenceRefs
	}
	writeJSON(response, http.StatusOK, struct {
		Project       db.ProjectSummary `json:"project"`
		Assessment    db.RiskAssessment `json:"assessment"`
		TopRiskReason string            `json:"top_risk_reason"`
		EvidenceRefs  []string          `json:"evidence_refs"`
	}{
		Project:       bundle.Project,
		Assessment:    bundle.Assessment,
		TopRiskReason: topRiskReason,
		EvidenceRefs:  evidenceRefs,
	})
}

func (server server) handleInternalCIChecks(response http.ResponseWriter, request *http.Request) {
	bundle, ok := server.loadInternalEvidenceBundle(response, request)
	if !ok {
		return
	}

	checks := []internalCheckRun{}
	for _, event := range bundle.Events {
		if event.EventType != "check_run" {
			continue
		}
		var payload struct {
			CheckRun struct {
				Name       string `json:"name"`
				Status     string `json:"status"`
				Conclusion string `json:"conclusion"`
				HTMLURL    string `json:"html_url"`
			} `json:"check_run"`
		}
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			writeJSON(response, http.StatusInternalServerError, map[string]string{
				"error": "decode check_run event failed",
			})
			return
		}
		checks = append(checks, internalCheckRun{
			EvidenceRef: event.ID,
			Name:        payload.CheckRun.Name,
			Status:      payload.CheckRun.Status,
			Conclusion:  payload.CheckRun.Conclusion,
			URL:         payload.CheckRun.HTMLURL,
		})
	}

	writeJSON(response, http.StatusOK, struct {
		Checks []internalCheckRun `json:"checks"`
	}{
		Checks: checks,
	})
}

func (server server) handleInternalPullRequests(response http.ResponseWriter, request *http.Request) {
	bundle, ok := server.loadInternalEvidenceBundle(response, request)
	if !ok {
		return
	}

	pullRequests := []internalPullRequest{}
	for _, event := range bundle.Events {
		if event.EventType != "pull_request" {
			continue
		}
		var payload struct {
			PullRequest struct {
				Number  int    `json:"number"`
				Title   string `json:"title"`
				State   string `json:"state"`
				HTMLURL string `json:"html_url"`
			} `json:"pull_request"`
		}
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			writeJSON(response, http.StatusInternalServerError, map[string]string{
				"error": "decode pull_request event failed",
			})
			return
		}
		pullRequests = append(pullRequests, internalPullRequest{
			EvidenceRef: event.ID,
			Number:      payload.PullRequest.Number,
			Title:       payload.PullRequest.Title,
			State:       payload.PullRequest.State,
			URL:         payload.PullRequest.HTMLURL,
		})
	}

	writeJSON(response, http.StatusOK, struct {
		PullRequests []internalPullRequest `json:"pull_requests"`
	}{
		PullRequests: pullRequests,
	})
}

func (server server) handleInternalGitHubAuthStatus(response http.ResponseWriter, request *http.Request) {
	repositories, ok := server.listInternalGitHubRepositories(response, request)
	if !ok {
		return
	}

	writeGitHubAccessStatus(response, repositories)
}

func (server server) handleGitHubSettings(response http.ResponseWriter, request *http.Request) {
	if server.store == nil {
		writeGitHubAccessStatus(response, []db.GitHubRepositoryAccess{}, "unavailable")
		return
	}

	repositories, err := server.store.ListGitHubRepositoryAccess(request.Context())
	if err != nil {
		writeJSON(response, http.StatusInternalServerError, map[string]string{
			"error": "list github repository access failed",
		})
		return
	}

	writeGitHubAccessStatus(response, repositories)
}

func (server server) handleDiscoverGitHubRepositories(response http.ResponseWriter, request *http.Request) {
	if server.store == nil {
		writeJSON(response, http.StatusServiceUnavailable, map[string]string{
			"error": "repository store is not configured",
		})
		return
	}

	var input struct {
		Repositories []struct {
			GitHubID int64  `json:"github_id"`
			Owner    string `json:"owner"`
			Name     string `json:"name"`
			FullName string `json:"full_name"`
		} `json:"repositories"`
	}
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{
			"error": "invalid JSON request body",
		})
		return
	}
	for _, repository := range input.Repositories {
		if repository.GitHubID == 0 ||
			repository.Owner == "" ||
			repository.Name == "" ||
			repository.FullName == "" {
			writeJSON(response, http.StatusBadRequest, map[string]string{
				"error": "github_id, owner, name, and full_name are required",
			})
			return
		}
		if _, err := server.store.ImportRepository(request.Context(), db.RepositoryInput{
			GitHubID: repository.GitHubID,
			Owner:    repository.Owner,
			Name:     repository.Name,
			FullName: repository.FullName,
		}); err != nil {
			writeJSON(response, http.StatusInternalServerError, map[string]string{
				"error": "discover github repositories failed",
			})
			return
		}
	}

	repositories, err := server.store.ListGitHubRepositoryAccess(request.Context())
	if err != nil {
		writeJSON(response, http.StatusInternalServerError, map[string]string{
			"error": "list github repository access failed",
		})
		return
	}
	writeGitHubAccessStatus(response, repositories)
}

func (server server) handleSetGitHubRepositoryAnalysis(response http.ResponseWriter, request *http.Request) {
	if server.store == nil {
		writeJSON(response, http.StatusServiceUnavailable, map[string]string{
			"error": "repository store is not configured",
		})
		return
	}

	repositoryID := chi.URLParam(request, "repositoryID")
	if repositoryID == "" {
		writeJSON(response, http.StatusBadRequest, map[string]string{
			"error": "repository id is required",
		})
		return
	}

	var input struct {
		AnalysisEnabled *bool `json:"analysis_enabled"`
	}
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{
			"error": "invalid JSON request body",
		})
		return
	}
	if input.AnalysisEnabled == nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{
			"error": "analysis_enabled is required",
		})
		return
	}

	repository, err := server.store.SetRepositoryAnalysisEnabled(
		request.Context(),
		repositoryID,
		*input.AnalysisEnabled,
	)
	if err != nil {
		status := http.StatusInternalServerError
		message := "set github repository analysis failed"
		if err == db.ErrNotFound {
			status = http.StatusNotFound
			message = "github repository not found"
		}
		writeJSON(response, status, map[string]string{"error": message})
		return
	}

	writeJSON(response, http.StatusOK, struct {
		Repository db.GitHubRepositoryAccess `json:"repository"`
	}{
		Repository: repository,
	})
}

func (server server) handleLoadGitHubRepositoryProject(response http.ResponseWriter, request *http.Request) {
	if server.store == nil {
		writeJSON(response, http.StatusServiceUnavailable, map[string]string{
			"error": "repository store is not configured",
		})
		return
	}

	repositoryID := chi.URLParam(request, "repositoryID")
	if repositoryID == "" {
		writeJSON(response, http.StatusBadRequest, map[string]string{
			"error": "repository id is required",
		})
		return
	}

	repository, err := server.store.GetGitHubRepositoryAccess(request.Context(), repositoryID)
	if err != nil {
		status := http.StatusInternalServerError
		message := "load github repository failed"
		if err == db.ErrNotFound {
			status = http.StatusNotFound
			message = "github repository not found"
		}
		writeJSON(response, status, map[string]string{"error": message})
		return
	}

	project, err := server.store.EnsureProjectForRepository(
		request.Context(),
		repository.ID,
		repository.Name,
	)
	if err != nil {
		writeJSON(response, http.StatusInternalServerError, map[string]string{
			"error": "load github repository project failed",
		})
		return
	}

	loadedRepository, err := server.store.GetGitHubRepositoryAccess(request.Context(), repositoryID)
	if err != nil {
		writeJSON(response, http.StatusInternalServerError, map[string]string{
			"error": "load github repository access failed",
		})
		return
	}

	writeJSON(response, http.StatusCreated, struct {
		Project    db.Project                `json:"project"`
		Repository db.GitHubRepositoryAccess `json:"repository"`
	}{
		Project:    project,
		Repository: loadedRepository,
	})
}

func (server server) handleTriggerGitHubRepositorySync(response http.ResponseWriter, request *http.Request) {
	if server.store == nil {
		writeJSON(response, http.StatusServiceUnavailable, map[string]string{
			"error": "repository store is not configured",
		})
		return
	}

	repositoryID := chi.URLParam(request, "repositoryID")
	if repositoryID == "" {
		writeJSON(response, http.StatusBadRequest, map[string]string{
			"error": "repository id is required",
		})
		return
	}

	if err := server.store.MarkRepositorySyncSucceeded(
		request.Context(),
		repositoryID,
		time.Now().UTC(),
	); err != nil {
		status := http.StatusInternalServerError
		message := "trigger github repository sync failed"
		if err == db.ErrNotFound {
			status = http.StatusNotFound
			message = "github repository not found"
		}
		writeJSON(response, status, map[string]string{"error": message})
		return
	}
	repository, err := server.store.GetGitHubRepositoryAccess(request.Context(), repositoryID)
	if err != nil {
		writeJSON(response, http.StatusInternalServerError, map[string]string{
			"error": "load synced github repository failed",
		})
		return
	}

	writeJSON(response, http.StatusAccepted, struct {
		Repository db.GitHubRepositoryAccess `json:"repository"`
	}{
		Repository: repository,
	})
}

func (server server) handleInternalGitHubRepositories(
	response http.ResponseWriter,
	request *http.Request,
) {
	repositories, ok := server.listInternalGitHubRepositories(response, request)
	if !ok {
		return
	}

	writeJSON(response, http.StatusOK, struct {
		Repositories []db.GitHubRepositoryAccess `json:"repositories"`
	}{
		Repositories: repositories,
	})
}

func (server server) handleInternalGitHubRepositoryPullRequests(
	response http.ResponseWriter,
	request *http.Request,
) {
	events, ok := server.loadInternalGitHubRepositoryEvents(
		response,
		request,
		"pull_request",
	)
	if !ok {
		return
	}

	pullRequests := []internalPullRequest{}
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
			writeJSON(response, http.StatusInternalServerError, map[string]string{
				"error": "decode pull_request event failed",
			})
			return
		}
		pullRequests = append(pullRequests, internalPullRequest{
			EvidenceRef: event.ID,
			Number:      payload.PullRequest.Number,
			Title:       payload.PullRequest.Title,
			State:       payload.PullRequest.State,
			URL:         payload.PullRequest.HTMLURL,
		})
	}

	writeJSON(response, http.StatusOK, struct {
		PullRequests []internalPullRequest `json:"pull_requests"`
	}{
		PullRequests: pullRequests,
	})
}

func (server server) handleInternalGitHubRepositoryIssues(
	response http.ResponseWriter,
	request *http.Request,
) {
	events, ok := server.loadInternalGitHubRepositoryEvents(
		response,
		request,
		"issues",
	)
	if !ok {
		return
	}

	issues := []internalIssue{}
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
			writeJSON(response, http.StatusInternalServerError, map[string]string{
				"error": "decode issue event failed",
			})
			return
		}
		issues = append(issues, internalIssue{
			EvidenceRef: event.ID,
			Number:      payload.Issue.Number,
			Title:       payload.Issue.Title,
			State:       payload.Issue.State,
			URL:         payload.Issue.HTMLURL,
		})
	}

	writeJSON(response, http.StatusOK, struct {
		Issues []internalIssue `json:"issues"`
	}{
		Issues: issues,
	})
}

func (server server) handleInternalGitHubRepositoryChecks(
	response http.ResponseWriter,
	request *http.Request,
) {
	events, ok := server.loadInternalGitHubRepositoryEvents(
		response,
		request,
		"check_run",
	)
	if !ok {
		return
	}

	checks := []internalCheckRun{}
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
			writeJSON(response, http.StatusInternalServerError, map[string]string{
				"error": "decode check_run event failed",
			})
			return
		}
		checks = append(checks, internalCheckRun{
			EvidenceRef: event.ID,
			Name:        payload.CheckRun.Name,
			Status:      payload.CheckRun.Status,
			Conclusion:  payload.CheckRun.Conclusion,
			URL:         payload.CheckRun.HTMLURL,
		})
	}

	writeJSON(response, http.StatusOK, struct {
		Checks []internalCheckRun `json:"checks"`
	}{
		Checks: checks,
	})
}

func (server server) loadInternalGitHubRepositoryEvents(
	response http.ResponseWriter,
	request *http.Request,
	eventType string,
) ([]db.GitHubRepositoryEvent, bool) {
	if server.store == nil {
		writeJSON(response, http.StatusServiceUnavailable, map[string]string{
			"error": "repository store is not configured",
		})
		return nil, false
	}

	repositoryID := chi.URLParam(request, "repositoryID")
	if repositoryID == "" {
		writeJSON(response, http.StatusBadRequest, map[string]string{
			"error": "repository id is required",
		})
		return nil, false
	}

	events, err := server.store.ListGitHubRepositoryEvents(
		request.Context(),
		repositoryID,
		eventType,
	)
	if err != nil {
		writeJSON(response, http.StatusInternalServerError, map[string]string{
			"error": "list github repository events failed",
		})
		return nil, false
	}

	return events, true
}

func writeGitHubAccessStatus(
	response http.ResponseWriter,
	repositories []db.GitHubRepositoryAccess,
	storageStatuses ...string,
) {
	storageStatus := "ready"
	if len(storageStatuses) > 0 {
		storageStatus = storageStatuses[0]
	}

	writeJSON(response, http.StatusOK, struct {
		Connected     bool                        `json:"connected"`
		Provider      string                      `json:"provider"`
		Repositories  []db.GitHubRepositoryAccess `json:"repositories"`
		Permissions   []string                    `json:"permissions"`
		StorageStatus string                      `json:"storage_status"`
	}{
		Connected:     len(repositories) > 0,
		Provider:      "github_app",
		Repositories:  repositories,
		StorageStatus: storageStatus,
		Permissions: []string{
			"metadata:read",
			"contents:read",
			"pull_requests:read",
			"checks:read",
			"issues:read",
		},
	})
}

func (server server) listInternalGitHubRepositories(
	response http.ResponseWriter,
	request *http.Request,
) ([]db.GitHubRepositoryAccess, bool) {
	if server.store == nil {
		writeJSON(response, http.StatusServiceUnavailable, map[string]string{
			"error": "repository store is not configured",
		})
		return nil, false
	}

	repositories, err := server.store.ListGitHubRepositoryAccess(request.Context())
	if err != nil {
		writeJSON(response, http.StatusInternalServerError, map[string]string{
			"error": "list github repository access failed",
		})
		return nil, false
	}

	return repositories, true
}

func (server server) handleInternalCreateActionSuggestion(
	response http.ResponseWriter,
	request *http.Request,
) {
	if server.store == nil {
		writeJSON(response, http.StatusServiceUnavailable, map[string]string{
			"error": "repository store is not configured",
		})
		return
	}

	var input struct {
		ProjectID    string   `json:"project_id"`
		ActionType   string   `json:"action_type"`
		TargetRef    string   `json:"target_ref"`
		DraftBody    string   `json:"draft_body"`
		EvidenceRefs []string `json:"evidence_refs"`
	}
	if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{
			"error": "invalid JSON request body",
		})
		return
	}
	if input.ProjectID == "" || input.ActionType == "" ||
		input.TargetRef == "" || input.DraftBody == "" {
		writeJSON(response, http.StatusBadRequest, map[string]string{
			"error": "project_id, action_type, target_ref, and draft_body are required",
		})
		return
	}

	suggestion, err := server.store.CreateActionSuggestion(
		request.Context(),
		db.ActionSuggestionInput{
			ProjectID:    input.ProjectID,
			ActionType:   input.ActionType,
			TargetRef:    input.TargetRef,
			DraftBody:    input.DraftBody,
			EvidenceRefs: input.EvidenceRefs,
		},
	)
	if err != nil {
		writeJSON(response, http.StatusInternalServerError, map[string]string{
			"error": "create action suggestion failed",
		})
		return
	}

	writeJSON(response, http.StatusCreated, suggestion)
}

func (server server) loadInternalEvidenceBundle(
	response http.ResponseWriter,
	request *http.Request,
) (db.EvidenceBundle, bool) {
	if server.store == nil {
		writeJSON(response, http.StatusServiceUnavailable, map[string]string{
			"error": "repository store is not configured",
		})
		return db.EvidenceBundle{}, false
	}

	assessmentID := chi.URLParam(request, "assessmentID")
	if assessmentID == "" {
		writeJSON(response, http.StatusBadRequest, map[string]string{
			"error": "assessment id is required",
		})
		return db.EvidenceBundle{}, false
	}

	bundle, err := server.store.GetEvidenceBundle(request.Context(), assessmentID)
	if err != nil {
		writeJSON(response, http.StatusNotFound, map[string]string{
			"error": "evidence bundle not found",
		})
		return db.EvidenceBundle{}, false
	}

	return bundle, true
}

type internalCheckRun struct {
	EvidenceRef string `json:"evidence_ref"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	Conclusion  string `json:"conclusion"`
	URL         string `json:"url"`
}

type internalPullRequest struct {
	EvidenceRef string `json:"evidence_ref"`
	Number      int    `json:"number"`
	Title       string `json:"title"`
	State       string `json:"state"`
	URL         string `json:"url"`
}

type internalIssue struct {
	EvidenceRef string `json:"evidence_ref"`
	Number      int    `json:"number"`
	Title       string `json:"title"`
	State       string `json:"state"`
	URL         string `json:"url"`
}
