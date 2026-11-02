package telegram

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

const testToken = "123456:TEST-token-value"

// newTestBot направляет клиента на фейковый сервер вместо api.telegram.org;
// адрес строится так же, как в NewBot: <base>/bot<TOKEN>/<method>.
func newTestBot(baseURL string) *bot {
	log := logrus.New()
	log.SetOutput(io.Discard)
	return &bot{
		token:       testToken,
		logger:      log,
		apiURL:      baseURL + "/bot" + testToken,
		client:      &http.Client{Timeout: 2 * time.Second},
		pollingStop: make(chan bool),
	}
}

func fakeTelegram(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestSetWebhookOkFalseReturnsError(t *testing.T) {
	// Telegram отвечает на отказ и 200, и 4xx — оба должны стать ошибкой.
	for _, status := range []int{http.StatusOK, http.StatusBadRequest} {
		srv := fakeTelegram(t, status, `{"ok":false,"error_code":400,"description":"Bad Request: bad webhook: HTTPS url must be provided for webhook"}`)

		err := newTestBot(srv.URL).SetWebhookWithSecret("http://insecure.example.test/hook", "s3cret")

		if err == nil {
			t.Fatalf("HTTP %d, ok:false: ожидалась ошибка, получен успех", status)
		}
		if !strings.Contains(err.Error(), "HTTPS url must be provided") {
			t.Errorf("HTTP %d: в ошибке нет description от Telegram: %v", status, err)
		}
		if strings.Contains(err.Error(), testToken) || strings.Contains(err.Error(), "s3cret") {
			t.Errorf("HTTP %d: ошибка содержит токен или секрет: %v", status, err)
		}
	}
}

func TestSetWebhookOkTrueSucceeds(t *testing.T) {
	srv := fakeTelegram(t, http.StatusOK, `{"ok":true,"result":true,"description":"Webhook was set"}`)

	if err := newTestBot(srv.URL).SetWebhookWithSecret("https://api.example.test/hook", "s3cret"); err != nil {
		t.Fatalf("ok:true: ожидался успех, получено %v", err)
	}
}

func TestNetworkErrorDoesNotLeakBotToken(t *testing.T) {
	// Закрытый сервер: client.Do вернёт *url.Error с полным адресом,
	// в котором стоит токен бота.
	srv := httptest.NewServer(http.NotFoundHandler())
	base := srv.URL
	srv.Close()

	err := newTestBot(base).SetWebhookWithSecret("https://api.example.test/hook", "s3cret")

	if err == nil {
		t.Fatal("закрытый сервер: ожидалась сетевая ошибка")
	}
	if strings.Contains(err.Error(), testToken) {
		t.Fatalf("ошибка содержит токен бота: %v", err)
	}
	if !strings.Contains(err.Error(), "setWebhook") {
		t.Errorf("в ошибке не назван метод: %v", err)
	}
}
