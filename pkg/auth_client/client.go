package auth_client

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"
)

// Client wraps the auth-service HTTP API used by markerloop-bot.
//
// The bot does NOT hold the JWT signing key (Phase -1.4 B.2). All token
// minting goes through auth-service, so this client owns two operations:
//
//   - IssueTokensForUser: internal endpoint, requires X-Service-Key.
//     Used after the bot has independently identified a user via
//     Telegram (deeplink / session restore).
//   - RefreshTokens: public /auth/refresh endpoint. The bot forwards a
//     stored refresh token instead of validating and re-signing it
//     locally — auth-service is the single signer.
type Client struct {
	baseURL    string
	serviceKey string
	httpClient *http.Client
	logger     *logrus.Logger
}

// ErrAccountDeletionPending — auth-service refused to mint tokens with 423
// account_deletion_pending (SDL-742): the user asked to delete the account
// and the deletion is not finished yet. The bot tells the user so instead of
// a generic error.
var ErrAccountDeletionPending = errors.New("auth-service: account deletion pending")

// AuthResponse mirrors auth-service models.AuthResponse. Fields here
// are the wire contract — keep in sync with auth-service.
type AuthResponse struct {
	UserID       string `json:"user_id"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
}

type apiResponse struct {
	Data    json.RawMessage `json:"data"`
	Message string          `json:"message"`
	Success bool            `json:"success"`
}

func NewClient(baseURL, serviceKey string, logger *logrus.Logger, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &Client{
		baseURL:    baseURL,
		serviceKey: serviceKey,
		httpClient: &http.Client{Timeout: timeout},
		logger:     logger,
	}
}

// IssueTokensForUser asks auth-service to mint a fresh access+refresh
// pair for an already-known user_id. Hits the internal endpoint guarded
// by X-Service-Key.
func (c *Client) IssueTokensForUser(userID, username, email string) (*AuthResponse, error) {
	body := map[string]string{
		"user_id":  userID,
		"username": username,
		"email":    email,
	}
	return c.do("POST", "/api/v1/auth/internal/issue-tokens-for-user", body, true)
}

// RefreshTokens exchanges a refresh token for a fresh pair via the
// public /auth/refresh endpoint. No service key — auth-service does
// rate-limit refresh by IP.
func (c *Client) RefreshTokens(refreshToken string) (*AuthResponse, error) {
	body := map[string]string{"refresh_token": refreshToken}
	return c.do("POST", "/api/v1/auth/refresh", body, false)
}

func (c *Client) do(method, path string, payload interface{}, withServiceKey bool) (*AuthResponse, error) {
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("auth_client: marshal request: %w", err)
	}

	req, err := http.NewRequest(method, c.baseURL+path, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("auth_client: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if withServiceKey {
		req.Header.Set("X-Service-Key", c.serviceKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("auth_client: do request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("auth_client: read response: %w", err)
	}

	if resp.StatusCode == http.StatusLocked {
		return nil, ErrAccountDeletionPending
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("auth-service %s %s: status %d: %s", method, path, resp.StatusCode, string(respBody))
	}

	var wrapped apiResponse
	if err := json.Unmarshal(respBody, &wrapped); err != nil {
		return nil, fmt.Errorf("auth_client: unmarshal envelope: %w", err)
	}

	var out AuthResponse
	if err := json.Unmarshal(wrapped.Data, &out); err != nil {
		return nil, fmt.Errorf("auth_client: unmarshal data: %w", err)
	}
	return &out, nil
}
