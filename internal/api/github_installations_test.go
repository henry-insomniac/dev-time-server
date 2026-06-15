package api_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/henry-insomniac/dev-time-server/internal/api"
	"github.com/henry-insomniac/dev-time-server/internal/testsupport"
)

func TestGitHubInstallationStartRedirectsToGitHub(t *testing.T) {
	router := api.NewRouter(api.Dependencies{
		GitHubApp: api.GitHubAppConfig{
			AppID:               "12345",
			AppSlug:             "dev-time-test",
			PrivateKeyPath:      writeTestGitHubAppPrivateKey(t),
			InstallationBaseURL: "https://github.test",
			SetupStateSecret:    "state-secret",
		},
	})

	response := performJSONRequest(
		router,
		http.MethodGet,
		"/api/github/installations/start",
		nil,
	)

	if response.Code != http.StatusSeeOther {
		t.Fatalf("expected install redirect 303, got %d: %s", response.Code, response.Body.String())
	}
	location := response.Header().Get("Location")
	if !strings.HasPrefix(
		location,
		"https://github.test/apps/dev-time-test/installations/new?",
	) {
		t.Fatalf("expected github installation location, got %q", location)
	}
	parsed, err := url.Parse(location)
	if err != nil {
		t.Fatalf("parse redirect location: %v", err)
	}
	if parsed.Query().Get("state") == "" {
		t.Fatalf("expected signed state in redirect location, got %q", location)
	}
}

func TestGitHubInstallationCallbackImportsInstallationRepositories(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	privateKeyPath := writeTestGitHubAppPrivateKey(t)
	var tokenRequested bool
	var repositoriesRequested bool
	githubServer := httptest.NewServer(http.HandlerFunc(func(
		response http.ResponseWriter,
		request *http.Request,
	) {
		switch {
		case request.Method == http.MethodPost &&
			request.URL.Path == "/app/installations/123/access_tokens":
			tokenRequested = true
			if !strings.HasPrefix(request.Header.Get("Authorization"), "Bearer ") {
				t.Fatalf("expected app JWT authorization header, got %q", request.Header.Get("Authorization"))
			}
			writeTestJSON(response, map[string]any{"token": "installation-token"})
		case request.Method == http.MethodGet &&
			request.URL.Path == "/installation/repositories":
			repositoriesRequested = true
			if request.Header.Get("Authorization") != "Bearer installation-token" {
				t.Fatalf("expected installation token authorization, got %q", request.Header.Get("Authorization"))
			}
			writeTestJSON(response, map[string]any{
				"repositories": []map[string]any{
					{
						"id":        1002,
						"name":      "dev-time-agent",
						"full_name": "henry-insomniac/dev-time-agent",
						"owner": map[string]any{
							"login": "henry-insomniac",
						},
					},
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
			AppID:               "12345",
			AppSlug:             "dev-time-test",
			PrivateKeyPath:      privateKeyPath,
			SetupStateSecret:    "state-secret",
			APIBaseURL:          githubServer.URL,
			FrontendBaseURL:     "http://localhost:5173",
			InstallationBaseURL: "https://github.test",
		},
	})

	startResponse := performJSONRequest(
		router,
		http.MethodGet,
		"/api/github/installations/start",
		nil,
	)
	if startResponse.Code != http.StatusSeeOther {
		t.Fatalf("expected start redirect 303, got %d: %s", startResponse.Code, startResponse.Body.String())
	}
	installURL, err := url.Parse(startResponse.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse install URL: %v", err)
	}
	state := installURL.Query().Get("state")
	if state == "" {
		t.Fatal("expected installation state")
	}

	callbackResponse := performJSONRequest(
		router,
		http.MethodGet,
		"/api/github/installations/callback?installation_id=123&setup_action=install&state="+url.QueryEscape(state),
		nil,
	)
	if callbackResponse.Code != http.StatusSeeOther {
		t.Fatalf("expected callback redirect 303, got %d: %s", callbackResponse.Code, callbackResponse.Body.String())
	}
	if callbackResponse.Header().Get("Location") != "http://localhost:5173?github_installation=success" {
		t.Fatalf("expected frontend success redirect, got %q", callbackResponse.Header().Get("Location"))
	}
	if !tokenRequested || !repositoriesRequested {
		t.Fatalf("expected token and repositories requests, token=%v repositories=%v", tokenRequested, repositoriesRequested)
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
	var settings struct {
		Repositories []struct {
			FullName string `json:"full_name"`
		} `json:"repositories"`
	}
	if err := json.NewDecoder(settingsResponse.Body).Decode(&settings); err != nil {
		t.Fatalf("decode github settings: %v", err)
	}
	if len(settings.Repositories) != 1 ||
		settings.Repositories[0].FullName != "henry-insomniac/dev-time-agent" {
		t.Fatalf("expected imported installation repository, got %#v", settings.Repositories)
	}
}

func writeTestGitHubAppPrivateKey(t *testing.T) string {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	encoded := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	path := t.TempDir() + "/github-app.pem"
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatalf("write private key: %v", err)
	}
	return path
}

func writeTestJSON(response http.ResponseWriter, payload any) {
	response.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(response).Encode(payload); err != nil {
		panic(err)
	}
}
