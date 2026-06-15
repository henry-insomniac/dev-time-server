package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/henry-insomniac/dev-time-server/internal/api"
	"github.com/henry-insomniac/dev-time-server/internal/testsupport"
)

func TestImportGitHubRepositoryCreatesProjectIdempotently(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	store := testsupport.NewMigratedStore(t, ctx)
	router := api.NewRouter(api.Dependencies{Store: store})

	payload := []byte(`{
		"github_id": 1001,
		"owner": "henry-insomniac",
		"name": "dev-time",
		"full_name": "henry-insomniac/dev-time"
	}`)

	first := performJSONRequest(router, http.MethodPost, "/api/github/repositories/import", payload)
	if first.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d: %s", first.Code, first.Body.String())
	}

	second := performJSONRequest(router, http.MethodPost, "/api/github/repositories/import", payload)
	if second.Code != http.StatusOK {
		t.Fatalf("expected idempotent status 200, got %d: %s", second.Code, second.Body.String())
	}

	firstProjectID := decodeProjectID(t, first)
	secondProjectID := decodeProjectID(t, second)
	if secondProjectID != firstProjectID {
		t.Fatalf("expected idempotent project id %q, got %q", firstProjectID, secondProjectID)
	}
}

func TestImportGitHubRepositoryUpdatesExistingFullNameGitHubID(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	store := testsupport.NewMigratedStore(t, ctx)
	router := api.NewRouter(api.Dependencies{Store: store})

	first := performJSONRequest(
		router,
		http.MethodPost,
		"/api/github/repositories/import",
		[]byte(`{
			"github_id": 9001,
			"owner": "henry-insomniac",
			"name": "dev-time",
			"full_name": "henry-insomniac/dev-time"
		}`),
	)
	if first.Code != http.StatusCreated {
		t.Fatalf("expected first import 201, got %d: %s", first.Code, first.Body.String())
	}
	second := performJSONRequest(
		router,
		http.MethodPost,
		"/api/github/repositories/import",
		[]byte(`{
			"github_id": 1265191048,
			"owner": "henry-insomniac",
			"name": "dev-time",
			"full_name": "henry-insomniac/dev-time"
		}`),
	)
	if second.Code != http.StatusOK {
		t.Fatalf("expected second import 200, got %d: %s", second.Code, second.Body.String())
	}

	var body struct {
		Repository struct {
			ID       string `json:"id"`
			GitHubID int64  `json:"github_id"`
		} `json:"repository"`
	}
	if err := json.NewDecoder(second.Body).Decode(&body); err != nil {
		t.Fatalf("decode second import response: %v", err)
	}
	if body.Repository.ID != "repo_9001" ||
		body.Repository.GitHubID != 1265191048 {
		t.Fatalf("expected same repository with updated github id, got %#v", body.Repository)
	}
}

func performJSONRequest(
	handler http.Handler,
	method string,
	target string,
	body []byte,
) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	return response
}

func decodeProjectID(t *testing.T, response *httptest.ResponseRecorder) string {
	t.Helper()

	var body struct {
		Project struct {
			ID string `json:"id"`
		} `json:"project"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode import response: %v", err)
	}

	if body.Project.ID == "" {
		t.Fatal("expected project id in import response")
	}

	return body.Project.ID
}
