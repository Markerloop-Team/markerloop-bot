// Package startup — стартовые проверки и регистрация вебхука Telegram,
// вынесенные из main, чтобы их можно было проверить тестом.
package startup

import (
	"github.com/sirupsen/logrus"
)

// placeholderWebhookURL — значение-заглушка из env.example; с ним вебхук
// не регистрируем.
const placeholderWebhookURL = "https://your-domain.com/api/v1/telegram/webhook"

// WebhookSetter — часть telegram.Bot, нужная для регистрации вебхука.
type WebhookSetter interface {
	SetWebhookWithSecret(url string, secretToken string) error
}

// WebhookMode — бот принимает обновления вебхуком: polling не включён и
// TELEGRAM_WEBHOOK_URL задан настоящим адресом, а не заглушкой.
func WebhookMode(usePolling bool, url string) bool {
	return !usePolling && url != "" && url != placeholderWebhookURL
}

// EnforceWebhookSecret не даёт стартовать в режиме вебхука без
// TELEGRAM_WEBHOOK_SECRET, на любом ENV: без секрета вебхук принимает
// поддельные обновления (вход под чужим аккаунтом). Без вебхука — polling
// или URL не задан — это локальная разработка: только предупреждение.
//
// Остановка идёт через logger.Fatal, то есть через logger.ExitFunc —
// тест подменяет её и проверяет отказ старта без выхода из процесса.
func EnforceWebhookSecret(usePolling bool, url, secret string, logger *logrus.Logger) {
	if secret != "" {
		return
	}
	if WebhookMode(usePolling, url) {
		logger.Fatal("TELEGRAM_WEBHOOK_SECRET не задан в режиме вебхука (TELEGRAM_WEBHOOK_URL задан, polling выключен): принимать вебхук Telegram без проверки secret token нельзя")
		return
	}
	logger.Warn("⚠️  TELEGRAM_WEBHOOK_SECRET не задан: вебхук не используется (polling или нет TELEGRAM_WEBHOOK_URL), допустимо только для локальной разработки")
}

// RegisterWebhook на каждом старте регистрирует вебхук у Telegram вместе с
// secret_token. Так Telegram гарантированно присылает тот секрет, который
// проверяет middleware, даже если вебхук когда-то ставили руками без него.
// Ошибка регистрации не роняет сервис: пишем error и работаем дальше.
func RegisterWebhook(bot WebhookSetter, url, secret string, logger *logrus.Logger) {
	if !WebhookMode(false, url) {
		logger.Warn("⚠️  TELEGRAM_WEBHOOK_URL не указан или является placeholder")
		logger.Info("Для работы бота установите TELEGRAM_WEBHOOK_URL или включите TELEGRAM_USE_POLLING=true для локальной разработки")
		return
	}
	if secret == "" {
		logger.Warn("⚠️  TELEGRAM_WEBHOOK_SECRET не задан: вебхук не регистрируется, чтобы не снять secret_token у Telegram")
		return
	}
	if err := bot.SetWebhookWithSecret(url, secret); err != nil {
		logger.Error("Ошибка установки webhook:", err)
		return
	}
	logger.Info("Webhook установлен:", url)
	logger.Info("Webhook защищен secret_token")
	logger.Info("Telegram Bot Service готов принимать обновления через webhook")
}
