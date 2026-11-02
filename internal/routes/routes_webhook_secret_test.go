package routes

import (
	"bytes"
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

// Вебхук Telegram обязан проверять secret token на самом маршруте: без этого
// поддельный update с чужим telegram_id и `/start login_<token>` проходил вход
// под чужим аккаунтом. Тест собирает роутер через SetupRoutes, то есть
// проверяет, что middleware реально стоит на /api/v1/webhook/telegram.

const testWebhookSecret = "test-webhook-secret_0123456789"

func newWebhookEngine(t *testing.T, secret string, logs io.Writer) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	log := logrus.New()
	log.SetOutput(logs)
	cfg := &config.Config{}
	cfg.Notifications.IdempotencyTTL = time.Hour
	cfg.Telegram.WebhookSecret = secret
	r := gin.New()
	SetupRoutes(r, handlers.NewTelegramHandler(nil, log), cfg, seededStore{keys: map[string]bool{}}, log)
	return r
}

// Обновление без message и callback_query: хендлер доходит до конца тела и
// отвечает «Webhook обработан», не трогая сервис (он здесь nil; логгер
// настоящий — хендлер пишет в журнал).
const neutralUpdate = `{"update_id":1}`

func postWebhook(r *gin.Engine, secret *string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhook/telegram", strings.NewReader(neutralUpdate))
	req.Header.Set("Content-Type", "application/json")
	if secret != nil {
		req.Header.Set(botmw.TelegramSecretHeader, *secret)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestWebhookRejectsMissingSecret(t *testing.T) {
	var logs bytes.Buffer
	r := newWebhookEngine(t, testWebhookSecret, &logs)

	w := postWebhook(r, nil)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("без заголовка код %d, ожидался 401", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Errorf("401 должен быть без тела, получено %q", w.Body.String())
	}
}

func TestWebhookRejectsWrongSecret(t *testing.T) {
	var logs bytes.Buffer
	r := newWebhookEngine(t, testWebhookSecret, &logs)

	wrong := "forged-secret-value"
	w := postWebhook(r, &wrong)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("с неверным секретом код %d, ожидался 401", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Errorf("401 должен быть без тела, получено %q", w.Body.String())
	}
	if !strings.Contains(logs.String(), "отклонён") {
		t.Errorf("отказ не попал в журнал: %q", logs.String())
	}
	if strings.Contains(logs.String(), wrong) || strings.Contains(logs.String(), testWebhookSecret) {
		t.Errorf("журнал содержит значение секрета: %q", logs.String())
	}
}

// Префикс верного секрета не должен проходить (защита от сравнения по длине
// присланного значения).
func TestWebhookRejectsSecretPrefix(t *testing.T) {
	r := newWebhookEngine(t, testWebhookSecret, io.Discard)

	prefix := testWebhookSecret[:5]
	if w := postWebhook(r, &prefix); w.Code != http.StatusUnauthorized {
		t.Fatalf("префикс секрета: код %d, ожидался 401", w.Code)
	}
}

func TestWebhookAcceptsValidSecret(t *testing.T) {
	r := newWebhookEngine(t, testWebhookSecret, io.Discard)

	secret := testWebhookSecret
	w := postWebhook(r, &secret)

	if w.Code != http.StatusOK {
		t.Fatalf("с верным секретом код %d, ожидался 200; тело %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Webhook обработан") {
		t.Errorf("update не дошёл до хендлера: %s", w.Body.String())
	}
}

// Пустой секрет (локальная разработка, polling) — проверка выключена; в режиме
// вебхука без секрета сервис не стартует, это проверяет пакет startup.
func TestWebhookWithoutConfiguredSecretPassesThrough(t *testing.T) {
	r := newWebhookEngine(t, "", io.Discard)

	if w := postWebhook(r, nil); w.Code != http.StatusOK {
		t.Fatalf("без настроенного секрета код %d, ожидался 200", w.Code)
	}
}
