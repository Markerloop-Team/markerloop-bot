package middleware

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// TelegramSecretHeader — заголовок, в котором Telegram присылает secret_token,
// заданный при setWebhook. Gateway пробрасывает его без изменений.
const TelegramSecretHeader = "X-Telegram-Bot-Api-Secret-Token"

// TelegramWebhookSecret пускает к вебхуку только запросы с верным secret_token.
//
// Без проверки любой мог прислать поддельный update с чужим telegram_id и
// `/start login_<token>` — и войти под чужим аккаунтом. Поэтому при заданном
// секрете заголовок обязателен: нет или не совпал — 401 без тела, в журнал
// только факт отказа, значение заголовка не пишется никогда.
//
// Пустой secret — проверка выключена. Это допустимо только без вебхука
// (polling, локальная разработка): старт в режиме вебхука без секрета
// отсекает startup.EnforceWebhookSecret на любом ENV.
func TelegramWebhookSecret(secret string, logger *logrus.Logger) gin.HandlerFunc {
	if secret == "" {
		return func(c *gin.Context) { c.Next() }
	}
	// Сравниваем хэши: длины всегда равны, и ConstantTimeCompare не выдаёт
	// длину секрета по времени ответа.
	want := sha256.Sum256([]byte(secret))
	return func(c *gin.Context) {
		got := c.GetHeader(TelegramSecretHeader)
		sum := sha256.Sum256([]byte(got))
		if got == "" || subtle.ConstantTimeCompare(sum[:], want[:]) != 1 {
			logger.WithContext(c.Request.Context()).
				WithField("secret_header_present", got != "").
				Warn("Telegram webhook отклонён: secret token отсутствует или не совпадает")
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		c.Next()
	}
}
