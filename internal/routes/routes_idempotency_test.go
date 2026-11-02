package routes

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"telegram-bot-service/internal/config"
	"telegram-bot-service/internal/handlers"
	botmw "telegram-bot-service/internal/middleware"
)

// SDL-345: проверяем, что Idempotency-middleware реально зарегистрирован
// на группе /api/v1/notifications, а не только существует как функция.
// Хендлер передаётся с nil-сервисами — до его тела запрос дойти не должен:
// store предзасеян done-ключами (со скоупом маршрута), и middleware обязан
// ответить дублем ДО хендлера (дойди запрос до тела — nil-сервис уронил бы
// тест паникой, что и есть сигнал «middleware не стоит»).

type seededStore struct{ keys map[string]bool }

func (s seededStore) Exists(_ context.Context, keys ...string) (int64, error) {
	var n int64
	for _, k := range keys {
		if s.keys[k] {
			n++
		}
	}
	return n, nil
}
func (s seededStore) IncrWithTTL(_ context.Context, key string, _ time.Duration) (int64, error) {
	if s.keys[key] {
		return 2, nil
	}
	s.keys[key] = true
	return 1, nil
}
func (s seededStore) Set(_ context.Context, key string, _ interface{}, _ time.Duration) error {
	s.keys[key] = true
	return nil
}
func (s seededStore) Del(_ context.Context, keys ...string) error {
	for _, k := range keys {
		delete(s.keys, k)
	}
	return nil
}

func newRoutedEngine(t *testing.T, dedup botmw.DedupStore) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	log := logrus.New()
	log.SetOutput(io.Discard)
	cfg := &config.Config{}
	cfg.Notifications.IdempotencyTTL = time.Hour
	cfg.Services.ServiceKey = testServiceKey
	r := gin.New()
	SetupRoutes(r, &handlers.TelegramHandler{}, cfg, dedup, log)
	return r
}

func TestNotificationsGroupHasIdempotencyMiddleware(t *testing.T) {
	store := seededStore{keys: map[string]bool{
		"notify:idem:done:/api/v1/notifications/send:seen-key":      true,
		"notify:idem:done:/api/v1/notifications/broadcast:seen-key": true,
		"notify:idem:done:/api/v1/notifications/deploy:seen-key":    true,
	}}
	r := newRoutedEngine(t, store)

	for _, path := range []string{
		"/api/v1/notifications/send",
		"/api/v1/notifications/broadcast",
		"/api/v1/notifications/deploy",
	} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
			req.Header.Set(botmw.IdempotencyHeader, "seen-key")
			req.Header.Set("X-Service-Key", testServiceKey)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("код %d, ожидался 200 (подавленный дубль)", w.Code)
			}
			if !strings.Contains(w.Body.String(), "duplicate") {
				t.Errorf("ответ не помечен как дубль: %s", w.Body.String())
			}
		})
	}
}
