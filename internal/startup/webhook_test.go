package startup

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
)

func newLogger(out io.Writer) (*logrus.Logger, *int) {
	log := logrus.New()
	log.SetOutput(out)
	exitCode := -1
	// logrus.Fatal зовёт ExitFunc — подменяем, чтобы проверить отказ старта
	// без выхода из тестового процесса.
	log.ExitFunc = func(code int) { exitCode = code }
	return log, &exitCode
}

const realWebhookURL = "https://api.example.test/v1/telegram/webhook"

// Режим вебхука без секрета — отказ старта на любом ENV: ENV больше не
// участвует в решении, поэтому test/development/production ведут себя одинаково.
func TestEnforceWebhookSecretRefusesWebhookModeWithoutSecret(t *testing.T) {
	var out bytes.Buffer
	log, exitCode := newLogger(&out)

	EnforceWebhookSecret(false, realWebhookURL, "", log)

	if *exitCode != 1 {
		t.Fatalf("вебхук без секрета: exit code %d, ожидался отказ старта (1)", *exitCode)
	}
	if !strings.Contains(out.String(), "TELEGRAM_WEBHOOK_SECRET") {
		t.Errorf("причина отказа не названа в журнале: %q", out.String())
	}
}

func TestEnforceWebhookSecretAllowsWebhookModeWithSecret(t *testing.T) {
	log, exitCode := newLogger(io.Discard)

	EnforceWebhookSecret(false, realWebhookURL, "configured", log)

	if *exitCode != -1 {
		t.Fatalf("вебхук с секретом не должен останавливаться, exit code %d", *exitCode)
	}
}

// Без вебхука (polling, URL не задан или заглушка) — локальная разработка:
// предупреждение, старт продолжается.
func TestEnforceWebhookSecretWarnsWithoutWebhook(t *testing.T) {
	cases := map[string]struct {
		polling bool
		url     string
	}{
		"polling с URL": {true, realWebhookURL},
		"polling":       {true, ""},
		"нет URL":       {false, ""},
		"placeholder":   {false, placeholderWebhookURL},
	}
	for name, c := range cases {
		var out bytes.Buffer
		log, exitCode := newLogger(&out)

		EnforceWebhookSecret(c.polling, c.url, "", log)

		if *exitCode != -1 {
			t.Fatalf("%s без секрета не должен останавливать старт, exit code %d", name, *exitCode)
		}
		if !strings.Contains(out.String(), "level=warning") {
			t.Errorf("%s: нет предупреждения в журнале: %q", name, out.String())
		}
	}
}

type fakeBot struct {
	calls  int
	url    string
	secret string
	err    error
}

func (f *fakeBot) SetWebhookWithSecret(url, secret string) error {
	f.calls++
	f.url, f.secret = url, secret
	return f.err
}

func TestRegisterWebhookPassesSecretToken(t *testing.T) {
	bot := &fakeBot{}
	log, _ := newLogger(io.Discard)

	RegisterWebhook(bot, realWebhookURL, "s3cret", log)

	if bot.calls != 1 {
		t.Fatalf("setWebhook вызван %d раз, ожидался 1", bot.calls)
	}
	if bot.url != realWebhookURL || bot.secret != "s3cret" {
		t.Fatalf("setWebhook(%q, secret len %d): ожидались URL и secret_token из конфига", bot.url, len(bot.secret))
	}
}

func TestRegisterWebhookSkipsWithoutURLOrSecret(t *testing.T) {
	cases := map[string][2]string{
		"нет URL":     {"", "s3cret"},
		"placeholder": {placeholderWebhookURL, "s3cret"},
		"нет секрета": {realWebhookURL, ""},
	}
	for name, c := range cases {
		bot := &fakeBot{}
		log, _ := newLogger(io.Discard)

		RegisterWebhook(bot, c[0], c[1], log)

		if bot.calls != 0 {
			t.Errorf("%s: setWebhook вызван, хотя регистрировать нечего", name)
		}
	}
}

func TestRegisterWebhookErrorDoesNotStopStart(t *testing.T) {
	var out bytes.Buffer
	log, exitCode := newLogger(&out)
	bot := &fakeBot{err: errors.New("telegram unavailable")}

	RegisterWebhook(bot, realWebhookURL, "s3cret", log)

	if *exitCode != -1 {
		t.Fatalf("ошибка setWebhook не должна останавливать сервис, exit code %d", *exitCode)
	}
	if !strings.Contains(out.String(), "level=error") {
		t.Errorf("ошибка регистрации не попала в журнал: %q", out.String())
	}
	if strings.Contains(out.String(), "s3cret") {
		t.Errorf("журнал содержит секрет: %q", out.String())
	}
}
