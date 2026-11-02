package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"

	"gitlab.com/markerloop/go-sdk/pkg/requestid"
)

// WebhookService сервис для отправки webhook уведомлений
type WebhookService struct {
	telegramBotURL string
	httpClient     *http.Client
	logger         *logrus.Logger
}

// WebhookNotification структура уведомления для webhook
type WebhookNotification struct {
	Type      string                 `json:"type"` // deploy, error, info, warning
	Title     string                 `json:"title"`
	Message   string                 `json:"message"`
	Service   string                 `json:"service,omitempty"`
	Level     string                 `json:"level"` // info, warning, error, success
	Data      map[string]interface{} `json:"data,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
}

// NewWebhookService создает новый сервис webhook
func NewWebhookService(telegramBotURL string, logger *logrus.Logger) *WebhookService {
	return &WebhookService{
		telegramBotURL: telegramBotURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		logger: logger,
	}
}

// SendDeployNotification отправляет уведомление о деплое
func (ws *WebhookService) SendDeployNotification(ctx context.Context, service, version, status, environment string) error {
	title := fmt.Sprintf("🚀 Деплой %s", service)
	message := fmt.Sprintf("Сервис: %s\nВерсия: %s\nСтатус: %s\nОкружение: %s",
		service, version, status, environment)

	notification := WebhookNotification{
		Type:    "deploy",
		Title:   title,
		Message: message,
		Service: service,
		Level:   getStatusLevel(status),
		Data: map[string]interface{}{
			"version":     version,
			"status":      status,
			"environment": environment,
		},
		Timestamp: time.Now(),
	}

	return ws.sendNotification(ctx, notification)
}

// SendErrorNotification отправляет уведомление об ошибке
func (ws *WebhookService) SendErrorNotification(ctx context.Context, service, errorType, message string, details map[string]interface{}) error {
	title := fmt.Sprintf("❌ Ошибка в %s", service)
	fullMessage := fmt.Sprintf("Тип: %s\nСообщение: %s", errorType, message)

	notification := WebhookNotification{
		Type:      "error",
		Title:     title,
		Message:   fullMessage,
		Service:   service,
		Level:     "error",
		Data:      details,
		Timestamp: time.Now(),
	}

	return ws.sendNotification(ctx, notification)
}

// SendSystemNotification отправляет системное уведомление
func (ws *WebhookService) SendSystemNotification(ctx context.Context, title, message string, level string) error {
	notification := WebhookNotification{
		Type:      "system",
		Title:     title,
		Message:   message,
		Level:     level,
		Timestamp: time.Now(),
	}

	return ws.sendNotification(ctx, notification)
}

// sendNotification отправляет уведомление в Telegram Bot Service
func (ws *WebhookService) sendNotification(ctx context.Context, notification WebhookNotification) error {
	jsonData, err := json.Marshal(notification)
	if err != nil {
		return fmt.Errorf("ошибка сериализации уведомления: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", ws.telegramBotURL+"/api/v1/notifications/broadcast", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("ошибка создания запроса: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	requestid.Inject(ctx, req)

	resp, err := ws.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("ошибка отправки уведомления: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Telegram Bot Service вернул статус %d", resp.StatusCode)
	}

	ws.logger.WithContext(ctx).Infof("Уведомление отправлено: %s", notification.Title)
	return nil
}

// getStatusLevel возвращает уровень уведомления на основе статуса
func getStatusLevel(status string) string {
	switch status {
	case "success", "completed", "deployed":
		return "success"
	case "warning", "partial":
		return "warning"
	case "error", "failed", "rollback":
		return "error"
	default:
		return "info"
	}
}
