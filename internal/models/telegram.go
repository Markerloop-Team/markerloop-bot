package models

import (
	"time"

	"github.com/google/uuid"
)

// TelegramUser представляет привязку Telegram аккаунта к пользователю
// Хранится в Redis, ключ: telegram_user:{telegram_id}
type TelegramUser struct {
	ID           string    `json:"id"`
	UserID       string    `json:"user_id"`     // ID пользователя из user-service
	TelegramID   int64     `json:"telegram_id"` // ID пользователя в Telegram
	Username     *string   `json:"username"`    // @username в Telegram
	FirstName    string    `json:"first_name"`
	LastName     *string   `json:"last_name"`
	LanguageCode *string   `json:"language_code"`
	IsActive     bool      `json:"is_active"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// NewTelegramUser создает новый TelegramUser с автогенерацией ID
func NewTelegramUser() *TelegramUser {
	return &TelegramUser{
		ID:        uuid.New().String(),
		IsActive:  true,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
}

// TelegramAuthRequest запрос на привязку Telegram аккаунта
type TelegramAuthRequest struct {
	TelegramID   int64   `json:"telegram_id" binding:"required"`
	Username     *string `json:"username"`
	FirstName    string  `json:"first_name" binding:"required"`
	LastName     *string `json:"last_name"`
	LanguageCode *string `json:"language_code"`
	AuthCode     string  `json:"auth_code" binding:"required"` // Код для привязки аккаунта
}

// TelegramAuthResponse ответ на привязку аккаунта
type TelegramAuthResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	UserID  string `json:"user_id,omitempty"`
}

// TelegramMessage структура для отправки сообщений
type TelegramMessage struct {
	ChatID      int64  `json:"chat_id" binding:"required"`
	Text        string `json:"text" binding:"required"`
	ParseMode   string `json:"parse_mode,omitempty"`   // HTML, Markdown
	ReplyMarkup string `json:"reply_markup,omitempty"` // JSON строка с клавиатурой
}

// WebhookNotification уведомление через webhook
// Хранится временно в Redis для истории (опционально)
type WebhookNotification struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"` // deploy, error, info
	Title     string    `json:"title"`
	Message   string    `json:"message"`
	Service   string    `json:"service"` // Название сервиса
	Level     string    `json:"level"`   // info, warning, error
	Data      string    `json:"data"`    // Дополнительные данные в JSON
	CreatedAt time.Time `json:"created_at"`
}

// NewWebhookNotification создает новое уведомление с автогенерацией ID
func NewWebhookNotification() *WebhookNotification {
	return &WebhookNotification{
		ID:        uuid.New().String(),
		CreatedAt: time.Now(),
	}
}

// APIRequest запрос для тестирования API через бота
// Хранится временно в Redis для истории (опционально, TTL 7 дней)
type APIRequest struct {
	ID         string    `json:"id"`
	UserID     string    `json:"user_id"`
	Method     string    `json:"method"` // GET, POST, PUT, DELETE
	URL        string    `json:"url"`
	Headers    string    `json:"headers"`  // JSON строка с заголовками
	Body       string    `json:"body"`     // Тело запроса
	Response   string    `json:"response"` // Ответ сервера
	StatusCode int       `json:"status_code"`
	CreatedAt  time.Time `json:"created_at"`
}

// NewAPIRequest создает новый API запрос с автогенерацией ID
func NewAPIRequest() *APIRequest {
	return &APIRequest{
		ID:        uuid.New().String(),
		CreatedAt: time.Now(),
	}
}
