package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/henry-insomniac/dev-time-server/internal/db"
)

var errGitHubRateLimited = errors.New("github rate limited")

type githubRepositoryInstallation struct {
	ID int64 `json:"id"`
}

type githubRepositoryMetadata struct {
	DefaultBranch string `json:"default_branch"`
}

type githubLiveCheckRuns struct {
	CheckRuns []struct {
		ID         int64  `json:"id"`
		Name       string `json:"name"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
		HTMLURL    string `json:"html_url"`
	} `json:"check_runs"`
}

func (server server) liveGitHubPullRequests(
	ctx context.Context,
	repository db.GitHubRepositoryAccess,
) ([]internalPullRequest, error) {
	token, err := server.githubInstallationTokenForRepository(ctx, repository)
	if err != nil {
		return nil, err
	}

	var pullRequests []struct {
		Number  int    `json:"number"`
		Title   string `json:"title"`
		State   string `json:"state"`
		HTMLURL string `json:"html_url"`
	}
	if err := requestGitHubAPI(
		ctx,
		server.githubApp,
		token,
		"/repos/"+githubPath(repository.Owner)+"/"+githubPath(repository.Name)+"/pulls?state=all&per_page=10",
		&pullRequests,
	); err != nil {
		return nil, err
	}

	items := make([]internalPullRequest, 0, len(pullRequests))
	for _, pullRequest := range pullRequests {
		items = append(items, internalPullRequest{
			EvidenceRef: "github_live_pull_request_" + strconv.Itoa(pullRequest.Number),
			Number:      pullRequest.Number,
			Title:       pullRequest.Title,
			State:       pullRequest.State,
			URL:         pullRequest.HTMLURL,
		})
	}
	return items, nil
}

func (server server) liveGitHubIssues(
	ctx context.Context,
	repository db.GitHubRepositoryAccess,
) ([]internalIssue, error) {
	token, err := server.githubInstallationTokenForRepository(ctx, repository)
	if err != nil {
		return nil, err
	}

	var issues []struct {
		Number      int       `json:"number"`
		Title       string    `json:"title"`
		State       string    `json:"state"`
		HTMLURL     string    `json:"html_url"`
		PullRequest *struct{} `json:"pull_request"`
	}
	if err := requestGitHubAPI(
		ctx,
		server.githubApp,
		token,
		"/repos/"+githubPath(repository.Owner)+"/"+githubPath(repository.Name)+"/issues?state=all&per_page=10",
		&issues,
	); err != nil {
		return nil, err
	}

	items := make([]internalIssue, 0, len(issues))
	for _, issue := range issues {
		if issue.PullRequest != nil {
			continue
		}
		items = append(items, internalIssue{
			EvidenceRef: "github_live_issue_" + strconv.Itoa(issue.Number),
			Number:      issue.Number,
			Title:       issue.Title,
			State:       issue.State,
			URL:         issue.HTMLURL,
		})
	}
	return items, nil
}

func (server server) liveGitHubChecks(
	ctx context.Context,
	repository db.GitHubRepositoryAccess,
) ([]internalCheckRun, error) {
	token, err := server.githubInstallationTokenForRepository(ctx, repository)
	if err != nil {
		return nil, err
	}

	var metadata githubRepositoryMetadata
	if err := requestGitHubAPI(
		ctx,
		server.githubApp,
		token,
		"/repos/"+githubPath(repository.Owner)+"/"+githubPath(repository.Name),
		&metadata,
	); err != nil {
		return nil, err
	}
	ref := strings.TrimSpace(metadata.DefaultBranch)
	if ref == "" {
		ref = "HEAD"
	}

	var checkRuns githubLiveCheckRuns
	if err := requestGitHubAPI(
		ctx,
		server.githubApp,
		token,
		"/repos/"+githubPath(repository.Owner)+"/"+githubPath(repository.Name)+
			"/commits/"+githubPath(ref)+"/check-runs?per_page=10",
		&checkRuns,
	); err != nil {
		return nil, err
	}

	items := make([]internalCheckRun, 0, len(checkRuns.CheckRuns))
	for _, checkRun := range checkRuns.CheckRuns {
		evidenceRef := "github_live_check_run_" + strconv.FormatInt(checkRun.ID, 10)
		if checkRun.ID == 0 {
			evidenceRef = "github_live_check_run_" + strings.ReplaceAll(checkRun.Name, " ", "_")
		}
		items = append(items, internalCheckRun{
			EvidenceRef: evidenceRef,
			RunID:       checkRun.ID,
			Name:        checkRun.Name,
			Status:      checkRun.Status,
			Conclusion:  checkRun.Conclusion,
			URL:         checkRun.HTMLURL,
		})
	}
	return items, nil
}

func (server server) liveGitHubCheckLogs(
	ctx context.Context,
	repository db.GitHubRepositoryAccess,
	runID int64,
) (internalCheckLogExcerpt, error) {
	token, err := server.githubInstallationTokenForRepository(ctx, repository)
	if err != nil {
		return internalCheckLogExcerpt{}, err
	}
	rawLogs, err := requestGitHubAPIText(
		ctx,
		server.githubApp,
		token,
		"/repos/"+githubPath(repository.Owner)+"/"+githubPath(repository.Name)+
			"/check-runs/"+strconv.FormatInt(runID, 10)+"/logs",
	)
	if err != nil {
		return internalCheckLogExcerpt{}, err
	}
	return internalCheckLogExcerpt{
		RunID:        runID,
		CheckName:    "github check " + strconv.FormatInt(runID, 10),
		Conclusion:   "failure",
		LogExcerpt:   truncateGitHubLogExcerpt(rawLogs, 4000),
		EvidenceRefs: []string{"github_live_check_run_" + strconv.FormatInt(runID, 10) + "_logs"},
	}, nil
}

func (server server) githubInstallationTokenForRepository(
	ctx context.Context,
	repository db.GitHubRepositoryAccess,
) (string, error) {
	if !server.githubApp.isConfigured() {
		return "", fmt.Errorf("github app is not configured")
	}
	jwt, err := server.githubAppJWT(time.Now().UTC())
	if err != nil {
		return "", err
	}
	installation, err := requestGitHubRepositoryInstallation(
		ctx,
		server.githubApp,
		repository.Owner,
		repository.Name,
		jwt,
	)
	if err != nil {
		return "", err
	}
	return requestGitHubInstallationToken(ctx, server.githubApp, installation.ID, jwt)
}

func requestGitHubRepositoryInstallation(
	ctx context.Context,
	config GitHubAppConfig,
	owner string,
	name string,
	jwt string,
) (githubRepositoryInstallation, error) {
	var installation githubRepositoryInstallation
	err := requestGitHubAPIWithBearer(
		ctx,
		config,
		jwt,
		"/repos/"+githubPath(owner)+"/"+githubPath(name)+"/installation",
		&installation,
	)
	if err != nil {
		return githubRepositoryInstallation{}, err
	}
	if installation.ID == 0 {
		return githubRepositoryInstallation{}, fmt.Errorf("github repository installation id is empty")
	}
	return installation, nil
}

func requestGitHubAPI(
	ctx context.Context,
	config GitHubAppConfig,
	token string,
	pathAndQuery string,
	target any,
) error {
	return requestGitHubAPIWithBearer(ctx, config, token, pathAndQuery, target)
}

func requestGitHubAPIWithBearer(
	ctx context.Context,
	config GitHubAppConfig,
	bearer string,
	pathAndQuery string,
	target any,
) error {
	requestURL := strings.TrimRight(config.APIBaseURL, "/") + pathAndQuery
	for attempt := 0; attempt < 2; attempt++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
		if err != nil {
			return fmt.Errorf("create github api request: %w", err)
		}
		request.Header.Set("Accept", "application/vnd.github+json")
		request.Header.Set("Authorization", "Bearer "+bearer)
		request.Header.Set("X-GitHub-Api-Version", "2022-11-28")

		response, err := githubHTTPClient.Do(request)
		if err != nil {
			if attempt == 0 {
				continue
			}
			return fmt.Errorf("request github api: %w", err)
		}
		defer response.Body.Close()
		if response.StatusCode >= 200 && response.StatusCode < 300 {
			if err := json.NewDecoder(response.Body).Decode(target); err != nil {
				return fmt.Errorf("decode github api response: %w", err)
			}
			return nil
		}
		body, _ := io.ReadAll(io.LimitReader(response.Body, 1024))
		if isGitHubRateLimitResponse(response) {
			return fmt.Errorf("%w: github api status %d: %s", errGitHubRateLimited, response.StatusCode, string(body))
		}
		if response.StatusCode >= 500 && attempt == 0 {
			continue
		}
		return fmt.Errorf("github api status %d: %s", response.StatusCode, string(body))
	}
	return fmt.Errorf("github api request failed")
}

func requestGitHubAPIText(
	ctx context.Context,
	config GitHubAppConfig,
	token string,
	pathAndQuery string,
) (string, error) {
	requestURL := strings.TrimRight(config.APIBaseURL, "/") + pathAndQuery
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return "", fmt.Errorf("create github api request: %w", err)
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	response, err := githubHTTPClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("request github api: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 1024))
		if isGitHubRateLimitResponse(response) {
			return "", fmt.Errorf("%w: github api status %d: %s", errGitHubRateLimited, response.StatusCode, string(body))
		}
		return "", fmt.Errorf("github api status %d: %s", response.StatusCode, string(body))
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 64*1024))
	if err != nil {
		return "", fmt.Errorf("read github api text response: %w", err)
	}
	return string(body), nil
}

func truncateGitHubLogExcerpt(logs string, limit int) string {
	logs = strings.TrimSpace(logs)
	if len(logs) <= limit {
		return logs
	}
	return logs[:limit]
}

func isGitHubRateLimitResponse(response *http.Response) bool {
	if response.StatusCode != http.StatusForbidden && response.StatusCode != http.StatusTooManyRequests {
		return false
	}
	return response.Header.Get("X-RateLimit-Remaining") == "0"
}

func githubPath(value string) string {
	return url.PathEscape(value)
}
