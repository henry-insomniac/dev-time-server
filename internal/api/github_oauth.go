package api

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const authSessionCookieName = "dev_time_session"

type githubOAuthUser struct {
	Login     string `json:"login"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
	HTMLURL   string `json:"html_url"`
}

func (server server) handleGitHubOAuthStart(
	response http.ResponseWriter,
	request *http.Request,
) {
	if !server.githubOAuthConfigured() {
		writeJSON(response, http.StatusServiceUnavailable, map[string]string{
			"error": "github oauth is not configured",
		})
		return
	}

	state, err := newGitHubInstallationState(
		server.githubApp.SetupStateSecret,
		time.Now().UTC(),
	)
	if err != nil {
		writeJSON(response, http.StatusInternalServerError, map[string]string{
			"error": "create github oauth state failed",
		})
		return
	}
	authURL, err := githubOAuthAuthorizationURL(server.githubApp, state)
	if err != nil {
		writeJSON(response, http.StatusInternalServerError, map[string]string{
			"error": "create github oauth URL failed",
		})
		return
	}
	http.Redirect(response, request, authURL, http.StatusSeeOther)
}

func (server server) handleGitHubOAuthCallback(
	response http.ResponseWriter,
	request *http.Request,
) {
	if !server.githubOAuthConfigured() {
		writeJSON(response, http.StatusServiceUnavailable, map[string]string{
			"error": "github oauth is not configured",
		})
		return
	}
	if err := verifyGitHubInstallationState(
		server.githubApp.SetupStateSecret,
		request.URL.Query().Get("state"),
		time.Now().UTC(),
	); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{
			"error": "invalid github oauth state",
		})
		return
	}
	code := strings.TrimSpace(request.URL.Query().Get("code"))
	if code == "" {
		writeJSON(response, http.StatusBadRequest, map[string]string{
			"error": "code is required",
		})
		return
	}
	token, err := requestGitHubOAuthToken(request, server.githubApp, code)
	if err != nil {
		writeJSON(response, http.StatusBadGateway, map[string]string{
			"error": "exchange github oauth token failed",
		})
		return
	}
	user, err := requestGitHubOAuthUser(request, server.githubApp, token)
	if err != nil {
		writeJSON(response, http.StatusBadGateway, map[string]string{
			"error": "load github oauth user failed",
		})
		return
	}
	cookieValue, err := signAuthSession(server.githubApp.SetupStateSecret, user)
	if err != nil {
		writeJSON(response, http.StatusInternalServerError, map[string]string{
			"error": "create auth session failed",
		})
		return
	}
	http.SetCookie(response, &http.Cookie{
		Name:     authSessionCookieName,
		Value:    cookieValue,
		Path:     "/",
		MaxAge:   int((14 * 24 * time.Hour).Seconds()),
		SameSite: http.SameSiteLaxMode,
		HttpOnly: true,
	})
	redirectURL := strings.TrimRight(server.githubApp.FrontendBaseURL, "/") +
		"?github_oauth=success"
	http.Redirect(response, request, redirectURL, http.StatusSeeOther)
}

func (server server) handleAuthSession(response http.ResponseWriter, request *http.Request) {
	cookie, err := request.Cookie(authSessionCookieName)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		writeJSON(response, http.StatusOK, map[string]any{
			"connected": false,
			"user":      nil,
		})
		return
	}
	user, err := verifyAuthSession(server.githubApp.SetupStateSecret, cookie.Value)
	if err != nil {
		writeJSON(response, http.StatusOK, map[string]any{
			"connected": false,
			"user":      nil,
		})
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{
		"connected": true,
		"user":      user,
	})
}

func (server server) githubOAuthConfigured() bool {
	return strings.TrimSpace(server.githubApp.OAuthClientID) != "" &&
		strings.TrimSpace(server.githubApp.OAuthClientSecret) != "" &&
		strings.TrimSpace(server.githubApp.SetupStateSecret) != ""
}

func githubOAuthAuthorizationURL(config GitHubAppConfig, state string) (string, error) {
	baseURL, err := url.Parse(strings.TrimRight(config.OAuthBaseURL, "/"))
	if err != nil {
		return "", err
	}
	baseURL.Path = strings.TrimRight(baseURL.Path, "/") + "/login/oauth/authorize"
	query := baseURL.Query()
	query.Set("client_id", config.OAuthClientID)
	query.Set("scope", "read:user user:email")
	query.Set("state", state)
	baseURL.RawQuery = query.Encode()
	return baseURL.String(), nil
}

func requestGitHubOAuthToken(
	request *http.Request,
	config GitHubAppConfig,
	code string,
) (string, error) {
	form := url.Values{}
	form.Set("client_id", config.OAuthClientID)
	form.Set("client_secret", config.OAuthClientSecret)
	form.Set("code", code)
	requestURL := strings.TrimRight(config.OAuthBaseURL, "/") + "/login/oauth/access_token"
	tokenRequest, err := http.NewRequestWithContext(
		request.Context(),
		http.MethodPost,
		requestURL,
		bytes.NewBufferString(form.Encode()),
	)
	if err != nil {
		return "", fmt.Errorf("create oauth token request: %w", err)
	}
	tokenRequest.Header.Set("Accept", "application/json")
	tokenRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := githubHTTPClient.Do(tokenRequest)
	if err != nil {
		return "", fmt.Errorf("request oauth token: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("oauth token status %d", response.StatusCode)
	}
	var body struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("decode oauth token: %w", err)
	}
	if strings.TrimSpace(body.AccessToken) == "" {
		return "", fmt.Errorf("oauth access token is empty")
	}
	return body.AccessToken, nil
}

func requestGitHubOAuthUser(
	request *http.Request,
	config GitHubAppConfig,
	token string,
) (githubOAuthUser, error) {
	requestURL := strings.TrimRight(config.APIBaseURL, "/") + "/user"
	userRequest, err := http.NewRequestWithContext(
		request.Context(),
		http.MethodGet,
		requestURL,
		nil,
	)
	if err != nil {
		return githubOAuthUser{}, fmt.Errorf("create oauth user request: %w", err)
	}
	userRequest.Header.Set("Accept", "application/vnd.github+json")
	userRequest.Header.Set("Authorization", "Bearer "+token)
	userRequest.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	response, err := githubHTTPClient.Do(userRequest)
	if err != nil {
		return githubOAuthUser{}, fmt.Errorf("request oauth user: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return githubOAuthUser{}, fmt.Errorf("oauth user status %d", response.StatusCode)
	}
	var user githubOAuthUser
	if err := json.NewDecoder(response.Body).Decode(&user); err != nil {
		return githubOAuthUser{}, fmt.Errorf("decode oauth user: %w", err)
	}
	if strings.TrimSpace(user.Login) == "" {
		return githubOAuthUser{}, fmt.Errorf("oauth user login is empty")
	}
	return user, nil
}

func signAuthSession(secret string, user githubOAuthUser) (string, error) {
	rawUser, err := json.Marshal(user)
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(rawUser)
	return payload + "." + signAuthPayload(secret, payload), nil
}

func verifyAuthSession(secret string, rawSession string) (githubOAuthUser, error) {
	parts := strings.Split(rawSession, ".")
	if len(parts) != 2 {
		return githubOAuthUser{}, fmt.Errorf("auth session is malformed")
	}
	expected := signAuthPayload(secret, parts[0])
	if !hmac.Equal([]byte(expected), []byte(parts[1])) {
		return githubOAuthUser{}, fmt.Errorf("auth session signature mismatch")
	}
	rawUser, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return githubOAuthUser{}, err
	}
	var user githubOAuthUser
	if err := json.Unmarshal(rawUser, &user); err != nil {
		return githubOAuthUser{}, err
	}
	return user, nil
}

func signAuthPayload(secret string, payload string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
