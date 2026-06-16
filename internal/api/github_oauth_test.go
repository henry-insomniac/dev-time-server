package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/henry-insomniac/dev-time-server/internal/api"
)

func TestGitHubOAuthCallbackCreatesUserSession(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var tokenRequested bool
	var userRequested bool
	githubServer := httptest.NewServer(http.HandlerFunc(func(
		response http.ResponseWriter,
		request *http.Request,
	) {
		switch {
		case request.Method == http.MethodPost && request.URL.Path == "/login/oauth/access_token":
			tokenRequested = true
			if err := request.ParseForm(); err != nil {
				t.Fatalf("parse token form: %v", err)
			}
			if request.Form.Get("client_id") != "oauth-client" ||
				request.Form.Get("client_secret") != "oauth-secret" ||
				request.Form.Get("code") != "oauth-code" {
				t.Fatalf("unexpected token exchange form: %#v", request.Form)
			}
			writeTestJSON(response, map[string]any{"access_token": "user-token"})
		case request.Method == http.MethodGet && request.URL.Path == "/user":
			userRequested = true
			if request.Header.Get("Authorization") != "Bearer user-token" {
				t.Fatalf("expected user token authorization, got %q", request.Header.Get("Authorization"))
			}
			writeTestJSON(response, map[string]any{
				"login":      "octocat",
				"name":       "The Octocat",
				"avatar_url": "https://github.test/avatar.png",
				"html_url":   "https://github.test/octocat",
			})
		default:
			t.Fatalf("unexpected github request %s %s", request.Method, request.URL.Path)
		}
	}))
	defer githubServer.Close()

	router := api.NewRouter(api.Dependencies{
		GitHubApp: api.GitHubAppConfig{
			SetupStateSecret:  "state-secret",
			FrontendBaseURL:   "http://localhost:5173",
			OAuthClientID:     "oauth-client",
			OAuthClientSecret: "oauth-secret",
			OAuthBaseURL:      githubServer.URL,
			APIBaseURL:        githubServer.URL,
		},
	})

	startResponse := performJSONRequest(router, http.MethodGet, "/api/auth/github/start", nil)
	if startResponse.Code != http.StatusSeeOther {
		t.Fatalf("expected oauth start redirect 303, got %d: %s", startResponse.Code, startResponse.Body.String())
	}
	startURL, err := url.Parse(startResponse.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse oauth start URL: %v", err)
	}
	if startURL.Path != "/login/oauth/authorize" ||
		startURL.Query().Get("client_id") != "oauth-client" ||
		startURL.Query().Get("state") == "" {
		t.Fatalf("unexpected oauth start URL: %q", startResponse.Header().Get("Location"))
	}

	callbackResponse := performJSONRequest(
		router,
		http.MethodGet,
		"/api/auth/github/callback?code=oauth-code&state="+url.QueryEscape(startURL.Query().Get("state")),
		nil,
	)
	if callbackResponse.Code != http.StatusSeeOther {
		t.Fatalf("expected oauth callback redirect 303, got %d: %s", callbackResponse.Code, callbackResponse.Body.String())
	}
	if callbackResponse.Header().Get("Location") != "http://localhost:5173?github_oauth=success" {
		t.Fatalf("expected frontend oauth success redirect, got %q", callbackResponse.Header().Get("Location"))
	}
	if !tokenRequested || !userRequested {
		t.Fatalf("expected token and user requests, token=%v user=%v", tokenRequested, userRequested)
	}
	sessionCookie := callbackResponse.Result().Cookies()[0]
	if sessionCookie.Name != "dev_time_session" || sessionCookie.Value == "" {
		t.Fatalf("expected signed session cookie, got %#v", sessionCookie)
	}

	sessionRequest := httptest.NewRequest(http.MethodGet, "/api/auth/session", nil).WithContext(ctx)
	sessionRequest.AddCookie(sessionCookie)
	sessionResponse := httptest.NewRecorder()
	router.ServeHTTP(sessionResponse, sessionRequest)
	if sessionResponse.Code != http.StatusOK {
		t.Fatalf("expected auth session 200, got %d: %s", sessionResponse.Code, sessionResponse.Body.String())
	}

	var body struct {
		Connected bool `json:"connected"`
		User      struct {
			Login     string `json:"login"`
			Name      string `json:"name"`
			AvatarURL string `json:"avatar_url"`
			HTMLURL   string `json:"html_url"`
		} `json:"user"`
	}
	if err := json.NewDecoder(sessionResponse.Body).Decode(&body); err != nil {
		t.Fatalf("decode auth session: %v", err)
	}
	if !body.Connected ||
		body.User.Login != "octocat" ||
		body.User.Name != "The Octocat" ||
		!strings.HasSuffix(body.User.AvatarURL, "/avatar.png") {
		t.Fatalf("expected connected oauth user, got %#v", body)
	}
}
