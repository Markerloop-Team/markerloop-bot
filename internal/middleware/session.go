package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"

	"telegram-bot-service/pkg/auth_client"

	// Импорт go-sdk
	"gitlab.com/markerloop/go-sdk/pkg/redis"
)

// SessionData данные сессии пользователя
type SessionData struct {
	UserID       string `json:"user_id"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    int64  `json:"expires_at"`
}

// SessionMiddlewareConfig конфигурация для session middleware.
//
// Phase -1.4 B.2: бот больше не подписывает и не валидирует JWT
// самостоятельно — обновление сессии идёт через auth-service /refresh.
type SessionMiddlewareConfig struct {
	RedisService *redis.RedisService
	AuthClient   *auth_client.Client
	Logger       *logrus.Logger
}

// CheckSession проверяет сессию пользователя и возвращает данные сессии
// Если сессия не найдена или истекла, пытается обновить через refresh_token
func CheckSession(cfg SessionMiddlewareConfig, telegramID int64) (*SessionData, error) {
	ctx := context.Background()
	sessionKey := fmt.Sprintf("telegram_session:%d", telegramID)

	// Проверяем сессию в Redis
	sessionJSON, err := cfg.RedisService.Get(ctx, sessionKey)
	if err != nil || sessionJSON == "" {
		// Сессия не найдена - возвращаем ошибку
		cfg.Logger.Debugf("Сессия не найдена для Telegram %d", telegramID)
		return nil, fmt.Errorf("сессия не найдена")
	}

	// Парсим данные сессии
	var sessionData SessionData
	if err := json.Unmarshal([]byte(sessionJSON), &sessionData); err != nil {
		cfg.Logger.Warnf("Ошибка парсинга сессии: %v", err)
		return nil, fmt.Errorf("ошибка парсинга сессии")
	}

	// Проверяем, не истекла ли сессия
	if time.Now().Unix() > sessionData.ExpiresAt {
		cfg.Logger.Debugf("Сессия истекла для Telegram %d, пытаемся обновить через refresh_token", telegramID)

		// Пытаемся обновить через refresh_token
		newSessionData, err := refreshSession(cfg, telegramID, sessionData.RefreshToken)
		if err != nil {
			return nil, fmt.Errorf("сессия истекла и не может быть обновлена")
		}
		return newSessionData, nil
	}

	// Сессия валидна - обновляем TTL (sliding expiration)
	if err := cfg.RedisService.Set(ctx, sessionKey, sessionJSON, 7*24*time.Hour); err != nil {
		cfg.Logger.Warnf("Ошибка обновления TTL сессии: %v", err)
		// Не критично, продолжаем
	}

	return &sessionData, nil
}

// refreshSession обновляет сессию через auth-service /refresh.
// Бот не валидирует refresh-token локально и не выпускает новые
// токены — это работа auth-service. Сюда приходит только тот
// refresh, который ранее был сохранён в Redis вместе с сессией.
func refreshSession(cfg SessionMiddlewareConfig, telegramID int64, refreshToken string) (*SessionData, error) {
	ctx := context.Background()

	tokenPair, err := cfg.AuthClient.RefreshTokens(refreshToken)
	if err != nil {
		return nil, fmt.Errorf("auth-service отказал в refresh: %w", err)
	}

	sessionData := SessionData{
		UserID:       tokenPair.UserID,
		AccessToken:  tokenPair.AccessToken,
		RefreshToken: tokenPair.RefreshToken,
		ExpiresAt:    time.Now().Add(7 * 24 * time.Hour).Unix(),
	}

	sessionJSON, err := json.Marshal(sessionData)
	if err != nil {
		return nil, fmt.Errorf("ошибка сериализации сессии: %w", err)
	}

	sessionKey := fmt.Sprintf("telegram_session:%d", telegramID)
	if err := cfg.RedisService.Set(ctx, sessionKey, string(sessionJSON), 7*24*time.Hour); err != nil {
		return nil, fmt.Errorf("ошибка сохранения обновлённой сессии: %w", err)
	}

	cfg.Logger.Infof("Сессия обновлена через auth-service для Telegram %d, user_id=%s", telegramID, tokenPair.UserID)
	return &sessionData, nil
}
