package user_client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"

	"gitlab.com/markerloop/go-sdk/pkg/requestid"
)

// ErrAccountDeletionPending — user-service ответил 423
// account_deletion_pending (SDL-742): аккаунт в процессе самоудаления, вход
// в него закрыт. Бот на это отвечает пользователю понятным отказом и не
// заводит нового пользователя.
var ErrAccountDeletionPending = errors.New("user-service: account deletion pending")

// Client клиент для работы с user-service
type Client struct {
	baseURL    string
	serviceKey string
	httpClient *http.Client
	logger     *logrus.Logger
}

// NewClient создает новый клиент user-service
func NewClient(baseURL, serviceKey string, logger *logrus.Logger) *Client {
	return &Client{
		baseURL:    baseURL,
		serviceKey: serviceKey,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		logger:     logger,
	}
}

// ExternalAccountRequest запрос для работы с external account
type ExternalAccountRequest struct {
	UserID       string  `json:"user_id"`
	Provider     string  `json:"provider"`
	ExternalID   string  `json:"external_id"`
	Username     *string `json:"username,omitempty"`
	DisplayName  *string `json:"display_name,omitempty"`
	AvatarURL    *string `json:"avatar_url,omitempty"`
	ProviderData string  `json:"provider_data,omitempty"`
	AuthToken    string  `json:"auth_token,omitempty"`
}

// ExternalAccountResponse ответ с external account
type ExternalAccountResponse struct {
	ID          string  `json:"id"`
	UserID      string  `json:"user_id"`
	Provider    string  `json:"provider"`
	ExternalID  string  `json:"external_id"`
	Username    *string `json:"username"`
	DisplayName *string `json:"display_name"`
	AvatarURL   *string `json:"avatar_url"`
	IsActive    bool    `json:"is_active"`
	LastUsedAt  *string `json:"last_used_at"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

// UserResponse ответ с пользователем
type UserResponse struct {
	ID            string  `json:"id"`
	Username      string  `json:"username"`
	Email         *string `json:"email"`
	EmailVerified bool    `json:"email_verified"`
	CreatedAt     string  `json:"created_at"`
	UpdatedAt     string  `json:"updated_at"`
}

// APIResponse стандартный ответ API
type APIResponse struct {
	Data    interface{} `json:"data"`
	Message string      `json:"message"`
	Success bool        `json:"success"`
}

// CreateExternalAccount создаёт внешний аккаунт напрямую (service-to-service, по user_id)
func (c *Client) CreateExternalAccount(ctx context.Context, req *ExternalAccountRequest) (*ExternalAccountResponse, error) {
	url := fmt.Sprintf("%s/api/v1/external-accounts/", c.baseURL)

	jsonData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("ошибка сериализации запроса: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("ошибка создания запроса: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Service-Key", c.serviceKey)

	requestid.Inject(ctx, httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ошибка выполнения запроса: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("ошибка чтения ответа: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("user-service вернул статус %d: %s", resp.StatusCode, string(body))
	}

	var apiResp APIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("ошибка десериализации ответа: %w", err)
	}

	accountData, err := json.Marshal(apiResp.Data)
	if err != nil {
		return nil, fmt.Errorf("ошибка сериализации данных: %w", err)
	}

	var account ExternalAccountResponse
	if err := json.Unmarshal(accountData, &account); err != nil {
		return nil, fmt.Errorf("ошибка десериализации аккаунта: %w", err)
	}

	return &account, nil
}

// LinkExternalAccount привязывает внешний аккаунт через OAuth токен
func (c *Client) LinkExternalAccount(ctx context.Context, req *ExternalAccountRequest) (*ExternalAccountResponse, error) {
	url := fmt.Sprintf("%s/api/v1/external-accounts/link", c.baseURL)

	jsonData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("ошибка сериализации запроса: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("ошибка создания запроса: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Service-Key", c.serviceKey)

	requestid.Inject(ctx, httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ошибка выполнения запроса: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("ошибка чтения ответа: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("user-service вернул статус %d: %s", resp.StatusCode, string(body))
	}

	var apiResp APIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("ошибка десериализации ответа: %w", err)
	}

	// Конвертируем data в ExternalAccountResponse
	accountData, err := json.Marshal(apiResp.Data)
	if err != nil {
		return nil, fmt.Errorf("ошибка сериализации данных: %w", err)
	}

	var account ExternalAccountResponse
	if err := json.Unmarshal(accountData, &account); err != nil {
		return nil, fmt.Errorf("ошибка десериализации аккаунта: %w", err)
	}

	return &account, nil
}

// GetUserByExternalAccount получает пользователя по внешнему аккаунту
// GetUserByUsername ищет пользователя по username
func (c *Client) GetUserByUsername(ctx context.Context, username string) (*UserResponse, error) {
	url := fmt.Sprintf("%s/api/v1/users/username/%s", c.baseURL, username)

	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("ошибка создания запроса: %w", err)
	}
	httpReq.Header.Set("X-Service-Key", c.serviceKey)

	requestid.Inject(ctx, httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ошибка выполнения запроса: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("ошибка чтения ответа: %w", err)
	}

	if resp.StatusCode == http.StatusLocked {
		return nil, ErrAccountDeletionPending
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("user-service вернул статус %d: %s", resp.StatusCode, string(body))
	}

	var apiResp APIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("ошибка десериализации ответа: %w", err)
	}

	userData, err := json.Marshal(apiResp.Data)
	if err != nil {
		return nil, fmt.Errorf("ошибка сериализации данных: %w", err)
	}

	var user UserResponse
	if err := json.Unmarshal(userData, &user); err != nil {
		return nil, fmt.Errorf("ошибка десериализации пользователя: %w", err)
	}

	return &user, nil
}

func (c *Client) GetUserByExternalAccount(ctx context.Context, provider, externalID string) (*UserResponse, error) {
	url := fmt.Sprintf("%s/api/v1/internal/external-accounts/get-user", c.baseURL)

	reqBody := map[string]string{
		"provider":    provider,
		"external_id": externalID,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("ошибка сериализации запроса: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("ошибка создания запроса: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Service-Key", c.serviceKey)

	requestid.Inject(ctx, httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ошибка выполнения запроса: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("ошибка чтения ответа: %w", err)
	}

	if resp.StatusCode == http.StatusLocked {
		return nil, ErrAccountDeletionPending
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("user-service вернул статус %d: %s", resp.StatusCode, string(body))
	}

	var apiResp APIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("ошибка десериализации ответа: %w", err)
	}

	// Конвертируем data в UserResponse
	userData, err := json.Marshal(apiResp.Data)
	if err != nil {
		return nil, fmt.Errorf("ошибка сериализации данных: %w", err)
	}

	var user UserResponse
	if err := json.Unmarshal(userData, &user); err != nil {
		return nil, fmt.Errorf("ошибка десериализации пользователя: %w", err)
	}

	return &user, nil
}

// GenerateAuthToken генерирует токен для OAuth привязки (internal endpoint)
func (c *Client) GenerateAuthToken(ctx context.Context, userID, provider string) (string, error) {
	url := fmt.Sprintf("%s/api/v1/internal/external-accounts/generate-token", c.baseURL)

	reqBody := map[string]string{
		"user_id":  userID,
		"provider": provider,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("ошибка сериализации запроса: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("ошибка создания запроса: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Service-Key", c.serviceKey)

	requestid.Inject(ctx, httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("ошибка выполнения запроса: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("ошибка чтения ответа: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("user-service вернул статус %d: %s", resp.StatusCode, string(body))
	}

	var apiResp APIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return "", fmt.Errorf("ошибка десериализации ответа: %w", err)
	}

	// Извлекаем auth_token из data
	dataMap, ok := apiResp.Data.(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("неверный формат данных в ответе")
	}

	authToken, ok := dataMap["auth_token"].(string)
	if !ok {
		return "", fmt.Errorf("токен не найден в ответе")
	}

	return authToken, nil
}

// UserAccessResponse ответ с ролями и разрешениями пользователя
type UserAccessResponse struct {
	Roles       []string `json:"roles"`
	Permissions []string `json:"permissions"`
}

// CreateUserRequest запрос на создание пользователя
type CreateUserRequest struct {
	Username string  `json:"username"`
	Email    *string `json:"email,omitempty"`
	Password string  `json:"password"`
}

// CreateUser создает нового пользователя
func (c *Client) CreateUser(ctx context.Context, req *CreateUserRequest) (*UserResponse, error) {
	url := fmt.Sprintf("%s/api/v1/users", c.baseURL)

	jsonData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("ошибка сериализации запроса: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("ошибка создания запроса: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Service-Key", c.serviceKey)

	requestid.Inject(ctx, httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ошибка выполнения запроса: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("ошибка чтения ответа: %w", err)
	}

	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("user-service вернул статус %d: %s", resp.StatusCode, string(body))
	}

	var apiResp APIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("ошибка десериализации ответа: %w", err)
	}

	// Конвертируем data в UserResponse
	userData, err := json.Marshal(apiResp.Data)
	if err != nil {
		return nil, fmt.Errorf("ошибка сериализации данных: %w", err)
	}

	var user UserResponse
	if err := json.Unmarshal(userData, &user); err != nil {
		return nil, fmt.Errorf("ошибка десериализации пользователя: %w", err)
	}

	return &user, nil
}

// GetUserAccess получает роли и разрешения пользователя через gateway
// Использует JWT access_token пользователя (не service key)
func (c *Client) GetUserAccess(ctx context.Context, gatewayURL, accessToken string) (*UserAccessResponse, error) {
	url := fmt.Sprintf("%s/api/v1/users/my/access", gatewayURL)

	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("ошибка создания запроса: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+accessToken)

	requestid.Inject(ctx, httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ошибка выполнения запроса: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("ошибка чтения ответа: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gateway вернул статус %d: %s", resp.StatusCode, string(body))
	}

	var apiResp APIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("ошибка десериализации ответа: %w", err)
	}

	accessData, err := json.Marshal(apiResp.Data)
	if err != nil {
		return nil, fmt.Errorf("ошибка сериализации данных: %w", err)
	}

	var access UserAccessResponse
	if err := json.Unmarshal(accessData, &access); err != nil {
		return nil, fmt.Errorf("ошибка десериализации доступа: %w", err)
	}

	return &access, nil
}

// UsersListData данные списка пользователей из user-service
type UsersListData struct {
	Users []UserResponse `json:"users"`
	Total int            `json:"total"`
}

// UsersListMeta мета пагинации
type UsersListMeta struct {
	Total      int `json:"total"`
	TotalPages int `json:"total_pages"`
}

// PaginatedUsersResponse полный ответ списка пользователей
type PaginatedUsersResponse struct {
	Data UsersListData `json:"data"`
	Meta UsersListMeta `json:"meta"`
}

// RoleResponse роль
type RoleResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// RolesListData данные списка ролей
type RolesListData struct {
	Roles []RoleResponse `json:"roles"`
	Total int            `json:"total"`
}

// GetUsers получает пагинированный список пользователей через gateway
func (c *Client) GetUsers(ctx context.Context, gatewayURL, accessToken string, page, limit int) (*PaginatedUsersResponse, error) {
	url := fmt.Sprintf("%s/api/v1/users?page=%d&limit=%d&sort_by=created_at&sort_order=desc", gatewayURL, page, limit)

	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("ошибка создания запроса: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+accessToken)

	requestid.Inject(ctx, httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ошибка выполнения запроса: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("ошибка чтения ответа: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gateway вернул статус %d: %s", resp.StatusCode, string(body))
	}

	var result PaginatedUsersResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("ошибка десериализации: %w", err)
	}

	return &result, nil
}

// GetRoles получает список ролей через gateway
func (c *Client) GetRoles(ctx context.Context, gatewayURL, accessToken string) (*RolesListData, error) {
	url := fmt.Sprintf("%s/api/v1/roles", gatewayURL)

	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("ошибка создания запроса: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+accessToken)

	requestid.Inject(ctx, httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ошибка выполнения запроса: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("ошибка чтения ответа: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gateway вернул статус %d: %s", resp.StatusCode, string(body))
	}

	var apiResp struct {
		Data RolesListData `json:"data"`
	}
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("ошибка десериализации: %w", err)
	}

	return &apiResp.Data, nil
}
