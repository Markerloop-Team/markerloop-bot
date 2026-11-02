package config

import (
	"strings"
	"testing"
)

// requiredEnv — переменные, без которых Load паникует. Тест ставит их
// заглушками, потому что проверяет не их, а поведение необязательных дефолтов.
var requiredEnv = map[string]string{
	"REDIS_ADDR":               "localhost:6379",
	"GATEWAY_URL":              "http://gateway",
	"AUTH_SERVICE_URL":         "http://auth",
	"USER_SERVICE_URL":         "http://user",
	"NOTIFICATION_SERVICE_URL": "http://notification",
	"DICTIONARY_SERVICE_URL":   "http://dictionary",
	"SERVICE_KEY":              "test-key",
}

// SDL-256: дефолт WEBAPP_URL участвует только при запуске без ConfigMap — оба
// контура задают переменную манифестом. Именно поэтому он однажды протух
// незамеченным и указывал на лендинг, где /webapp не было. Тест держит дефолт
// на живом адресе продукта: с SDL-607 это апекс, прежний хост админки — 301.
func TestWebAppURLDefaultPointsAtProductHost(t *testing.T) {
	for k, v := range requiredEnv {
		t.Setenv(k, v)
	}

	cfg := Load()

	const want = "https://markerloop.com/webapp"
	if got := cfg.Telegram.WebAppURL; got != want {
		t.Fatalf("WebAppURL default = %q, want %q", got, want)
	}
}

// Дефолт не должен побеждать заданное значение — иначе тест-контур, который
// переопределяет WEBAPP_URL на свой апекс test.markerloop.com, молча уезжал бы в прод.
func TestWebAppURLEnvOverridesDefault(t *testing.T) {
	for k, v := range requiredEnv {
		t.Setenv(k, v)
	}
	t.Setenv("WEBAPP_URL", "https://test.markerloop.com/webapp")

	cfg := Load()

	if got := cfg.Telegram.WebAppURL; !strings.HasPrefix(got, "https://test.markerloop.com/") {
		t.Fatalf("WebAppURL = %q, want the value from the environment", got)
	}
}
