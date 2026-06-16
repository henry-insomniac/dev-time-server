package api

import (
	"bytes"
	"context"
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/henry-insomniac/dev-time-server/internal/db"
)

var githubHTTPClient = &http.Client{Timeout: 30 * time.Second}

type githubInstallationToken struct {
	Token string `json:"token"`
}

type githubInstallationRepositories struct {
	Repositories []struct {
		ID       int64  `json:"id"`
		Name     string `json:"name"`
		FullName string `json:"full_name"`
		Owner    struct {
			Login string `json:"login"`
		} `json:"owner"`
	} `json:"repositories"`
}

func (config GitHubAppConfig) withDefaults() GitHubAppConfig {
	if config.APIBaseURL == "" {
		config.APIBaseURL = "https://api.github.com"
	}
	if config.FrontendBaseURL == "" {
		config.FrontendBaseURL = "http://localhost:5173"
	}
	if config.InstallationBaseURL == "" {
		config.InstallationBaseURL = "https://github.com"
	}
	if config.OAuthBaseURL == "" {
		config.OAuthBaseURL = "https://github.com"
	}
	return config
}

func (config GitHubAppConfig) isConfigured() bool {
	return strings.TrimSpace(config.AppID) != "" &&
		strings.TrimSpace(config.AppSlug) != "" &&
		strings.TrimSpace(config.PrivateKeyPath) != "" &&
		strings.TrimSpace(config.SetupStateSecret) != ""
}

func (server server) handleGitHubInstallationStart(
	response http.ResponseWriter,
	request *http.Request,
) {
	if !server.githubApp.isConfigured() {
		writeJSON(response, http.StatusServiceUnavailable, map[string]string{
			"error": "github app installation is not configured",
		})
		return
	}

	state, err := newGitHubInstallationState(
		server.githubApp.SetupStateSecret,
		time.Now().UTC(),
	)
	if err != nil {
		writeJSON(response, http.StatusInternalServerError, map[string]string{
			"error": "create github installation state failed",
		})
		return
	}
	installURL, err := githubInstallationURL(server.githubApp, state)
	if err != nil {
		writeJSON(response, http.StatusInternalServerError, map[string]string{
			"error": "create github installation URL failed",
		})
		return
	}

	http.Redirect(response, request, installURL, http.StatusSeeOther)
}

func (server server) handleGitHubInstallationCallback(
	response http.ResponseWriter,
	request *http.Request,
) {
	if server.store == nil {
		writeJSON(response, http.StatusServiceUnavailable, map[string]string{
			"error": "repository store is not configured",
		})
		return
	}
	state := request.URL.Query().Get("state")
	if err := verifyGitHubInstallationState(
		server.githubApp.SetupStateSecret,
		state,
		time.Now().UTC(),
	); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{
			"error": "invalid github installation state",
		})
		return
	}

	installationID, err := strconv.ParseInt(
		request.URL.Query().Get("installation_id"),
		10,
		64,
	)
	if err != nil || installationID <= 0 {
		writeJSON(response, http.StatusBadRequest, map[string]string{
			"error": "installation_id is required",
		})
		return
	}

	repositories, err := server.loadGitHubInstallationRepositories(
		request.Context(),
		installationID,
	)
	if err != nil {
		writeJSON(response, http.StatusBadGateway, map[string]string{
			"error": "sync github installation repositories failed",
		})
		return
	}
	for _, repository := range repositories {
		if _, err := server.store.ImportRepository(request.Context(), repository); err != nil {
			writeJSON(response, http.StatusInternalServerError, map[string]string{
				"error": "import github installation repository failed",
			})
			return
		}
	}

	redirectURL := strings.TrimRight(server.githubApp.FrontendBaseURL, "/") +
		"?github_installation=success"
	http.Redirect(response, request, redirectURL, http.StatusSeeOther)
}

func (server server) loadGitHubInstallationRepositories(
	ctx context.Context,
	installationID int64,
) ([]db.RepositoryInput, error) {
	jwt, err := server.githubAppJWT(time.Now().UTC())
	if err != nil {
		return nil, err
	}
	token, err := requestGitHubInstallationToken(ctx, server.githubApp, installationID, jwt)
	if err != nil {
		return nil, err
	}

	githubRepos, err := requestGitHubInstallationRepositories(
		ctx,
		server.githubApp,
		token,
	)
	if err != nil {
		return nil, err
	}
	repositories := make([]db.RepositoryInput, 0, len(githubRepos.Repositories))
	for _, repository := range githubRepos.Repositories {
		if repository.ID == 0 ||
			repository.Owner.Login == "" ||
			repository.Name == "" ||
			repository.FullName == "" {
			continue
		}
		repositories = append(repositories, db.RepositoryInput{
			GitHubID: repository.ID,
			Owner:    repository.Owner.Login,
			Name:     repository.Name,
			FullName: repository.FullName,
		})
	}
	return repositories, nil
}

func (server server) githubAppJWT(now time.Time) (string, error) {
	if strings.TrimSpace(server.githubApp.AppID) == "" ||
		strings.TrimSpace(server.githubApp.PrivateKeyPath) == "" {
		return "", fmt.Errorf("github app id and private key are required")
	}
	privateKey, err := loadGitHubAppPrivateKey(server.githubApp.PrivateKeyPath)
	if err != nil {
		return "", err
	}

	header := map[string]string{"alg": "RS256", "typ": "JWT"}
	claims := map[string]any{
		"iat": now.Add(-60 * time.Second).Unix(),
		"exp": now.Add(9 * time.Minute).Unix(),
		"iss": server.githubApp.AppID,
	}
	encodedHeader, err := jsonBase64URL(header)
	if err != nil {
		return "", err
	}
	encodedClaims, err := jsonBase64URL(claims)
	if err != nil {
		return "", err
	}
	unsigned := encodedHeader + "." + encodedClaims
	digest := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(
		rand.Reader,
		privateKey,
		crypto.SHA256,
		digest[:],
	)
	if err != nil {
		return "", fmt.Errorf("sign github app jwt: %w", err)
	}
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func requestGitHubInstallationToken(
	ctx context.Context,
	config GitHubAppConfig,
	installationID int64,
	jwt string,
) (string, error) {
	requestURL := strings.TrimRight(config.APIBaseURL, "/") +
		"/app/installations/" + strconv.FormatInt(installationID, 10) +
		"/access_tokens"
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		requestURL,
		bytes.NewReader([]byte("{}")),
	)
	if err != nil {
		return "", fmt.Errorf("create github installation token request: %w", err)
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("Authorization", "Bearer "+jwt)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	response, err := githubHTTPClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("request github installation token: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 1024))
		return "", fmt.Errorf("github installation token status %d: %s", response.StatusCode, string(body))
	}

	var token githubInstallationToken
	if err := json.NewDecoder(response.Body).Decode(&token); err != nil {
		return "", fmt.Errorf("decode github installation token: %w", err)
	}
	if token.Token == "" {
		return "", fmt.Errorf("github installation token is empty")
	}
	return token.Token, nil
}

func requestGitHubInstallationRepositories(
	ctx context.Context,
	config GitHubAppConfig,
	token string,
) (githubInstallationRepositories, error) {
	requestURL := strings.TrimRight(config.APIBaseURL, "/") +
		"/installation/repositories?per_page=100"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return githubInstallationRepositories{}, fmt.Errorf("create github repositories request: %w", err)
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	response, err := githubHTTPClient.Do(request)
	if err != nil {
		return githubInstallationRepositories{}, fmt.Errorf("request github repositories: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 1024))
		return githubInstallationRepositories{}, fmt.Errorf("github repositories status %d: %s", response.StatusCode, string(body))
	}

	var repositories githubInstallationRepositories
	if err := json.NewDecoder(response.Body).Decode(&repositories); err != nil {
		return githubInstallationRepositories{}, fmt.Errorf("decode github repositories: %w", err)
	}
	return repositories, nil
}

func githubInstallationURL(config GitHubAppConfig, state string) (string, error) {
	baseURL, err := url.Parse(strings.TrimRight(config.InstallationBaseURL, "/"))
	if err != nil {
		return "", err
	}
	baseURL.Path = strings.TrimRight(baseURL.Path, "/") +
		"/apps/" + config.AppSlug + "/installations/new"
	query := baseURL.Query()
	query.Set("state", state)
	baseURL.RawQuery = query.Encode()
	return baseURL.String(), nil
}

func newGitHubInstallationState(secret string, now time.Time) (string, error) {
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("create github installation nonce: %w", err)
	}
	payload := strconv.FormatInt(now.Unix(), 10) + "." +
		base64.RawURLEncoding.EncodeToString(nonce)
	return payload + "." + signGitHubInstallationState(secret, payload), nil
}

func verifyGitHubInstallationState(secret string, state string, now time.Time) error {
	if strings.TrimSpace(secret) == "" || strings.TrimSpace(state) == "" {
		return fmt.Errorf("github installation state secret and state are required")
	}
	parts := strings.Split(state, ".")
	if len(parts) != 3 {
		return fmt.Errorf("github installation state is malformed")
	}
	payload := parts[0] + "." + parts[1]
	expected := signGitHubInstallationState(secret, payload)
	if !hmac.Equal([]byte(expected), []byte(parts[2])) {
		return fmt.Errorf("github installation state signature mismatch")
	}
	issuedAt, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return fmt.Errorf("github installation state timestamp is invalid")
	}
	if now.Sub(time.Unix(issuedAt, 0)) > 15*time.Minute ||
		time.Unix(issuedAt, 0).Sub(now) > time.Minute {
		return fmt.Errorf("github installation state expired")
	}
	return nil
}

func signGitHubInstallationState(secret string, payload string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func loadGitHubAppPrivateKey(path string) (*rsa.PrivateKey, error) {
	rawKey, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read github app private key: %w", err)
	}
	block, _ := pem.Decode(rawKey)
	if block == nil {
		return nil, fmt.Errorf("decode github app private key pem")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse github app private key: %w", err)
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("github app private key must be RSA")
	}
	return key, nil
}

func jsonBase64URL(payload any) (string, error) {
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(rawPayload), nil
}
