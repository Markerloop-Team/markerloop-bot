package routes

import (
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	"telegram-bot-service/internal/config"
	"telegram-bot-service/internal/handlers"
	botmw "telegram-bot-service/internal/middleware"

	// Импорт go-sdk
	"gitlab.com/markerloop/go-sdk/pkg/middleware"
	"gitlab.com/markerloop/go-sdk/pkg/requestid"

	// Swagger docs
	_ "telegram-bot-service/docs"
)

// SetupRoutes настраивает маршруты API.
// dedup — хранилище идемпотентности доставки уведомлений (SDL-345),
// всегда non-nil в проде (main передаёт go-sdk RedisService). Ветки
// «без дедупа» нет намеренно: условная сборка цепочки давала бы проду и
// тестам разные наборы middleware и тихое отключение дедупа при ошибке
// вайринга (вывод code-review 21 Aug 2026).
func SetupRoutes(r *gin.Engine, telegramHandler *handlers.TelegramHandler, cfg *config.Config, dedup botmw.DedupStore, logger *logrus.Logger) {
	// Middleware из SDK
	r.Use(requestid.Middleware()) // SDL-87: сквозной X-Request-Id
	r.Use(middleware.Recovery(logger))
	r.Use(middleware.Logger(logger))
	r.Use(middleware.CORS())

	// Health check из SDK
	r.GET("/health", middleware.HealthCheck("telegram-bot-service", "1.0.0"))

	// Swagger UI
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	// API v1
	v1 := r.Group("/api/v1")
	{
		// Telegram webhook: пускаем только с верным
		// X-Telegram-Bot-Api-Secret-Token, иначе 401.
		v1.POST("/webhook/telegram", botmw.TelegramWebhookSecret(cfg.Telegram.WebhookSecret, logger), telegramHandler.Webhook)

		// Сервис-сервисная поверхность: OAuth-токены и уведомления зовут
		// только шлюз и notification-service, оба с X-Service-Key. До этой
		// правки ключ проверял лишь шлюз, и под в кластере мог выпустить
		// токен или разослать сообщение подписчикам без него.
		serviceAuth := middleware.ServiceToServiceAuth(cfg.Services.ServiceKey)

		// OAuth токены
		oauth := v1.Group("/oauth", serviceAuth)
		{
			oauth.POST("/generate-token", telegramHandler.GenerateOAuthToken)
		}

		// Уведомления. Idempotency (SDL-345): повторная доставка с тем же
		// Idempotency-Key подавляется, чтобы ретраи notification-service
		// (EP-56) не рассылали дубли подписчикам. Ключ проверяется раньше
		// дедупа: запрос без ключа не должен занимать Idempotency-Key.
		notifications := v1.Group("/notifications", serviceAuth)
		notifications.Use(botmw.Idempotency(dedup, cfg.Notifications.IdempotencyTTL, logger))
		{
			notifications.POST("/send", telegramHandler.SendNotification)
			notifications.POST("/broadcast", telegramHandler.SendBroadcast)
			notifications.POST("/deploy", telegramHandler.DeployNotification)
		}
	}
}
