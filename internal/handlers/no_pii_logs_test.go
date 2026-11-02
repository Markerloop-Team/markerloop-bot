package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"telegram-bot-service/internal/services"
)

// SDL-738: журнал бота несёт тип обновления, chat_id и команду — никогда
// текст сообщения, username, имя или полный объект обновления Telegram.

const (
	probeText     = "my secret message 4111"
	probeUsername = "leakprobe_alice"
	probeFirst    = "Alisa-Probe"
	probeToken    = "login_SECRETTOKEN123"
)

func captureLogger() (*logrus.Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	l := logrus.New()
	l.SetOutput(buf)
	l.SetLevel(logrus.DebugLevel)
	l.SetFormatter(&logrus.JSONFormatter{})
	return l, buf
}

func probeUpdate(text string, withFrom bool) map[string]interface{} {
	user := map[string]interface{}{
		"id": float64(424242), "username": probeUsername, "first_name": probeFirst, "language_code": "ru",
	}
	msg := map[string]interface{}{
		"message_id": float64(7),
		"chat":       map[string]interface{}{"id": float64(424242), "type": "private", "username": probeUsername, "first_name": probeFirst},
		"text":       text,
	}
	if withFrom {
		msg["from"] = user
	}
	return map[string]interface{}{"update_id": float64(1), "message": msg}
}

func assertClean(t *testing.T, out string) {
	t.Helper()
	if out == "" {
		t.Fatal("логгер ничего не записал — тест перестал что-либо проверять")
	}
	for _, needle := range []string{probeText, probeUsername, probeFirst, "SECRETTOKEN123"} {
		if strings.Contains(out, needle) {
			t.Fatalf("лог содержит %q:\n%s", needle, out)
		}
	}
}

// Polling-путь и webhook: сводка обновления вместо %+v всего объекта.
// Без поля from хендлер выходит до обращения к сервису.
func TestNoPIIInUpdateLogs(t *testing.T) {
	logger, buf := captureLogger()
	h := NewTelegramHandler(nil, logger)

	_ = h.HandleUpdate(probeUpdate(probeText, false))

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/webhook/telegram", h.Webhook)
	body, _ := json.Marshal(probeUpdate(probeText, false))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/webhook/telegram", bytes.NewReader(body)))
	if w.Code != http.StatusOK {
		t.Fatalf("webhook: статус %d", w.Code)
	}

	out := buf.String()
	assertClean(t, out)
	if !strings.Contains(out, "type=message chat_id=424242") {
		t.Fatalf("в логе нет типа обновления и chat_id:\n%s", out)
	}
}

// Строка «получено сообщение» пишется до вызова сервиса; сервис здесь
// пустой и падает дальше по ходу — паника перехватывается, проверяется
// только то, что уже ушло в журнал.
func TestNoPIIInMessageLog(t *testing.T) {
	for _, text := range []string{probeText, "/start " + probeToken, "/" + probeUsername + " " + probeText} {
		logger, buf := captureLogger()
		h := NewTelegramHandler(&services.TelegramService{}, logger)
		func() {
			defer func() { _ = recover() }()
			_ = h.HandleUpdate(probeUpdate(text, true))
		}()
		out := buf.String()
		assertClean(t, out)
		if !strings.Contains(out, "Получено сообщение: chat_id=424242") {
			t.Fatalf("нет строки о сообщении с chat_id:\n%s", out)
		}
	}
}

func TestCommandOf(t *testing.T) {
	cases := map[string]string{
		"/start login_abc": "/start",
		"/help@markerbot":  "/help",
		"/auth":            "/auth",
		"/notifications":   "/notifications",
		"/моя-тайна текст": "other",
		"/leakprobe_alice": "other",
		"/":                "other",
		"hello /start":     "",
		probeText:          "",
	}
	for in, want := range cases {
		if got := commandOf(in); got != want {
			t.Errorf("commandOf(%q) = %q, want %q", in, got, want)
		}
	}
}

// Callback data в журнал не пишется — только tg_id, msg и длина data.
// Сервис пустой: AnswerCallback паникует на nil-боте уже после строки лога.
func TestNoCallbackDataInLog(t *testing.T) {
	logger, buf := captureLogger()
	h := NewTelegramHandler(&services.TelegramService{}, logger)
	update := map[string]interface{}{
		"update_id": float64(2),
		"callback_query": map[string]interface{}{
			"id":   "cbq-1",
			"from": map[string]interface{}{"id": float64(424242), "username": probeUsername, "first_name": probeFirst},
			"message": map[string]interface{}{
				"message_id": float64(9),
				"chat":       map[string]interface{}{"id": float64(424242)},
			},
			"data": probeToken,
		},
	}
	func() {
		defer func() { _ = recover() }()
		_ = h.HandleUpdate(update)
	}()
	out := buf.String()
	assertClean(t, out)
	if !strings.Contains(out, "Получен callback query: tg_id=424242 msg=9 data_len=20") {
		t.Fatalf("нет строки о callback query:\n%s", out)
	}
}
