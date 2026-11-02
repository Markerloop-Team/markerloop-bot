package handlers

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"telegram-bot-service/internal/middleware"
	"telegram-bot-service/internal/services"

	// Импорт go-sdk
	"gitlab.com/markerloop/go-sdk/pkg/requestid"
	"gitlab.com/markerloop/go-sdk/pkg/response"
)

// getStringFromMap безопасно извлекает строку из map
func getStringFromMap(m map[string]interface{}, key string) string {
	if val, ok := m[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return ""
}

// TelegramHandler обработчик HTTP запросов для Telegram бота
type TelegramHandler struct {
	telegramService *services.TelegramService
	logger          *logrus.Logger
}

// NewTelegramHandler создает новый обработчик Telegram
func NewTelegramHandler(telegramService *services.TelegramService, logger *logrus.Logger) *TelegramHandler {
	return &TelegramHandler{
		telegramService: telegramService,
		logger:          logger,
	}
}

// HandleUpdate обрабатывает обновление от Telegram (для polling)
func (h *TelegramHandler) HandleUpdate(update map[string]interface{}) error {
	// SDL-99: polling-путь не имеет входящего HTTP-запроса, поэтому
	// request-id генерируется здесь — один id на обновление.
	ctx := requestid.NewContext(context.Background(), requestid.New())
	h.logger.WithContext(ctx).Infof("Получено обновление: %s", describeUpdate(update))

	// Обрабатываем разные типы обновлений
	if message, ok := update["message"].(map[string]interface{}); ok {
		h.handleMessage(ctx, message)
	} else if callbackQuery, ok := update["callback_query"].(map[string]interface{}); ok {
		h.handleCallbackQuery(ctx, callbackQuery)
	}

	return nil
}

// Webhook обрабатывает webhook от Telegram
// @Summary Telegram Webhook
// @Description Принимает обновления от Telegram Bot API.
// @Tags telegram
// @Accept json
// @Produce json
// @Param X-Telegram-Bot-Api-Secret-Token header string true "secret_token, заданный при setWebhook (обязателен, если задан TELEGRAM_WEBHOOK_SECRET)"
// @Param update body object true "Telegram Update объект"
// @Success 200 {object} models.GenericSuccessResponse
// @Failure 400 {object} models.ErrorResponseSwagger
// @Failure 401 "secret token отсутствует или не совпадает (без тела)"
// @Router /webhook/telegram [post]
func (h *TelegramHandler) Webhook(c *gin.Context) {
	ctx := c.Request.Context()

	var update map[string]interface{}
	if err := c.ShouldBindJSON(&update); err != nil {
		h.logger.WithContext(ctx).Warn("Ошибка парсинга webhook:", err) // 4xx по вине вызывающего — не Sentry (SDL-215)
		response.BadRequest(c, "Неверный формат данных")
		return
	}

	h.logger.WithContext(ctx).Infof("Получен webhook: %s", describeUpdate(update))

	// Обрабатываем разные типы обновлений
	if message, ok := update["message"].(map[string]interface{}); ok {
		h.handleMessage(ctx, message)
	} else if callbackQuery, ok := update["callback_query"].(map[string]interface{}); ok {
		h.handleCallbackQuery(ctx, callbackQuery)
	}

	response.Success(c, gin.H{"status": "ok"}, "Webhook обработан")
}

// GenerateOAuthToken генерирует OAuth токен и deep link для привязки аккаунта
// @Summary Генерация OAuth deep link
// @Description Генерирует deep link для привязки Telegram аккаунта. Service-to-service endpoint.
// @Tags telegram
// @Accept json
// @Produce json
// @Security ServiceKey
// @Param request body models.GenerateOAuthTokenRequest true "ID пользователя"
// @Success 200 {object} models.GenericSuccessResponse
// @Failure 400 {object} models.ErrorResponseSwagger
// @Failure 500 {object} models.ErrorResponseSwagger
// @Router /oauth/generate-token [post]
func (h *TelegramHandler) GenerateOAuthToken(c *gin.Context) {
	ctx := c.Request.Context()

	var req struct {
		UserID string `json:"user_id" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Неверный формат данных")
		return
	}

	// Генерируем deep link
	deepLink, err := h.telegramService.GenerateOAuthDeepLink(ctx, req.UserID)
	if err != nil {
		h.logger.WithContext(ctx).Error("Ошибка генерации OAuth deep link:", err)
		response.InternalServerError(c, "Ошибка генерации deep link")
		return
	}

	response.Success(c, gin.H{
		"deep_link": deepLink,
		"message":   "Deep link для авторизации через Telegram сгенерирован.",
	}, "Deep link сгенерирован")
}

// SendNotification отправляет уведомление пользователю
// @Summary Отправить уведомление
// @Description Отправляет Telegram уведомление пользователю. Service-to-service endpoint.
// @Tags telegram
// @Accept json
// @Produce json
// @Security ServiceKey
// @Param request body models.SendNotificationRequest true "Данные уведомления"
// @Success 200 {object} models.GenericSuccessResponse
// @Failure 400 {object} models.ErrorResponseSwagger
// @Failure 500 {object} models.ErrorResponseSwagger
// @Router /notifications/send [post]
func (h *TelegramHandler) SendNotification(c *gin.Context) {
	ctx := c.Request.Context()

	var req struct {
		UserID  string `json:"user_id" binding:"required"`
		Title   string `json:"title" binding:"required"`
		Message string `json:"message" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Неверный формат данных")
		return
	}

	if err := h.telegramService.SendNotification(ctx, req.UserID, req.Title, req.Message); err != nil {
		h.logger.WithContext(ctx).Error("Ошибка отправки уведомления:", err)
		response.InternalServerError(c, "Ошибка отправки уведомления")
		return
	}

	response.Success(c, gin.H{"status": "sent"}, "Уведомление отправлено")
}

// SendBroadcast отправляет уведомление всем пользователям
// @Summary Рассылка уведомлений
// @Description Отправляет Telegram уведомление всем пользователям. Service-to-service endpoint.
// @Tags telegram
// @Accept json
// @Produce json
// @Security ServiceKey
// @Param request body models.SendBroadcastRequest true "Данные рассылки"
// @Success 200 {object} models.GenericSuccessResponse
// @Failure 400 {object} models.ErrorResponseSwagger
// @Failure 500 {object} models.ErrorResponseSwagger
// @Router /notifications/broadcast [post]
func (h *TelegramHandler) SendBroadcast(c *gin.Context) {
	ctx := c.Request.Context()

	var req struct {
		Title   string `json:"title" binding:"required"`
		Message string `json:"message" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Неверный формат данных")
		return
	}

	if err := h.telegramService.SendBroadcastNotification(ctx, req.Title, req.Message); err != nil {
		h.logger.WithContext(ctx).Error("Ошибка отправки рассылки:", err)
		response.InternalServerError(c, "Ошибка отправки рассылки")
		return
	}

	response.Success(c, gin.H{"status": "sent"}, "Рассылка отправлена")
}

// DeployNotification принимает webhook от notification-service (который
// проксирует ArgoCD Notifications) и рассылает подписчикам.
//
// All fields except App are optional — older payloads only carry the
// original three (app/status/revision); the renderer in the service
// layer falls back gracefully so we don't break the in-flight pipeline
// while MR B / MR C are landing.
//
// Регресс dev-infra (SDL-715): event_type=regression-failed рассылается как
// раньше и ставит отметку red; event_type=regression-passed отправляет
// «✅ регресс снова зелёный» только при отметке red, иначе отвечает
// 200 {"status":"skipped"} без сообщения.
func (h *TelegramHandler) DeployNotification(c *gin.Context) {
	ctx := c.Request.Context()

	var req struct {
		App          string `json:"app" binding:"required"`
		Status       string `json:"status"`
		Revision     string `json:"revision"`
		EventType    string `json:"event_type"`
		RevisionURL  string `json:"revision_url"`
		ArgocdURL    string `json:"argocd_url"`
		RepoURL      string `json:"repo_url"`
		StartedAt    string `json:"started_at"`
		FinishedAt   string `json:"finished_at"`
		Message      string `json:"message"`
		HealthStatus string `json:"health_status"`
		SyncStatus   string `json:"sync_status"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Неверный формат данных")
		return
	}

	sent, err := h.telegramService.DispatchDeployNotification(ctx, services.DeployNotificationPayload{
		App:          req.App,
		Status:       req.Status,
		Revision:     req.Revision,
		EventType:    req.EventType,
		RevisionURL:  req.RevisionURL,
		ArgocdURL:    req.ArgocdURL,
		RepoURL:      req.RepoURL,
		StartedAt:    req.StartedAt,
		FinishedAt:   req.FinishedAt,
		Message:      req.Message,
		HealthStatus: req.HealthStatus,
		SyncStatus:   req.SyncStatus,
	})
	if err != nil {
		h.logger.WithContext(ctx).Error("Ошибка отправки deploy notification:", err)
		response.InternalServerError(c, "Ошибка отправки уведомления")
		return
	}
	if !sent {
		// SDL-715: regression-passed без прошлого красного — тишина, но 200:
		// событие принято, отправителю повторять нечего.
		response.Success(c, gin.H{"status": "skipped"}, "Сообщение не требуется")
		return
	}

	response.Success(c, gin.H{"status": "sent"}, "Deploy notification отправлен")
}

// TestAPI и GetAPIHistory будут реализованы позже через dictionary-service

// handleMessage обрабатывает текстовые сообщения
func (h *TelegramHandler) handleMessage(ctx context.Context, message map[string]interface{}) {
	chat, ok := message["chat"].(map[string]interface{})
	if !ok {
		return
	}

	chatID, ok := chat["id"].(float64)
	if !ok {
		return
	}

	text, ok := message["text"].(string)
	if !ok {
		return
	}

	from, ok := message["from"].(map[string]interface{})
	if !ok {
		return
	}

	telegramID, ok := from["id"].(float64)
	if !ok {
		return
	}

	// Извлекаем данные пользователя
	username := getStringFromMap(from, "username")
	firstName := getStringFromMap(from, "first_name")
	lastName := getStringFromMap(from, "last_name")
	languageCode := getStringFromMap(from, "language_code")

	h.logger.WithContext(ctx).Infof("Получено сообщение: chat_id=%d command=%q", int64(chatID), commandOf(text))

	// Обрабатываем команды
	if text == "/start" || strings.HasPrefix(text, "/start ") {
		var authToken string
		if strings.HasPrefix(text, "/start ") {
			authToken = strings.TrimPrefix(text, "/start ")
		}
		h.handleStartCommand(ctx, int64(chatID), int64(telegramID), username, firstName, lastName, languageCode, authToken)
	} else if text == "/auth" || strings.HasPrefix(text, "/auth ") {
		h.handleAuthCommand(ctx, int64(chatID), int64(telegramID), text, languageCode)
	} else {
		// Для остальных команд проверяем сессию
		sessionData, err := h.telegramService.CheckSession(ctx, int64(telegramID))
		if err != nil {
			// Проверяем, не является ли это частью диалога /auth
			if err := h.telegramService.HandleAuthMessage(ctx, int64(telegramID), text, languageCode); err != nil {
				h.logger.WithContext(ctx).Debugf("Сообщение не является частью диалога /auth: %v", err)
			}
			h.sendAuthRequiredMessage(ctx, int64(telegramID), languageCode)
			return
		}

		// Сессия валидна — команды-разделы (новое сообщение, messageID=0)
		tid := int64(telegramID)
		switch text {
		case "/menu", "/help":
			h.telegramService.HandleCallbackQuery(ctx, "back_to_menu", tid, 0, languageCode, sessionData)
		case "/profile":
			h.telegramService.HandleCallbackQuery(ctx, "profile", tid, 0, languageCode, sessionData)
		case "/settings":
			h.telegramService.HandleCallbackQuery(ctx, "settings", tid, 0, languageCode, nil)
		case "/logout":
			h.telegramService.HandleCallbackQuery(ctx, "logout", tid, 0, languageCode, nil)
		case "/users":
			h.telegramService.HandleCallbackQuery(ctx, "users_list", tid, 0, languageCode, sessionData)
		case "/stats":
			h.telegramService.HandleCallbackQuery(ctx, "stats", tid, 0, languageCode, sessionData)
		case "/notifications":
			h.telegramService.HandleCallbackQuery(ctx, "notifications", tid, 0, languageCode, sessionData)
		default:
			handled, err := h.telegramService.HandleDialogMessage(ctx, tid, text, languageCode, sessionData)
			if err != nil {
				h.logger.WithContext(ctx).Errorf("Ошибка обработки диалога: %v", err)
			}
			if !handled {
				h.handleTextMessage(ctx, int64(chatID), tid, text, languageCode, sessionData)
			}
		}
	}
}

// handleCallbackQuery обрабатывает нажатия на кнопки
func (h *TelegramHandler) handleCallbackQuery(ctx context.Context, callbackQuery map[string]interface{}) {
	from, ok := callbackQuery["from"].(map[string]interface{})
	if !ok {
		return
	}

	telegramID, ok := from["id"].(float64)
	if !ok {
		return
	}

	data, ok := callbackQuery["data"].(string)
	if !ok {
		return
	}

	languageCode := getStringFromMap(from, "language_code")

	// Извлекаем message_id и callback_query_id для редактирования сообщения
	var messageID int64
	if msg, ok := callbackQuery["message"].(map[string]interface{}); ok {
		if mid, ok := msg["message_id"].(float64); ok {
			messageID = int64(mid)
		}
	}
	callbackQueryID := getStringFromMap(callbackQuery, "id")

	h.logger.WithContext(ctx).Infof("Получен callback query: tg_id=%d msg=%d data_len=%d", int64(telegramID), messageID, len(data))

	// Отвечаем на callback query (убираем "часики")
	if callbackQueryID != "" {
		h.telegramService.AnswerCallback(ctx, callbackQueryID)
	}

	var sessionData *middleware.SessionData

	// Проверяем сессию для callback query (кроме кнопки "auth")
	if data != "auth" {
		sd, err := h.telegramService.CheckSession(ctx, int64(telegramID))
		if err != nil {
			h.sendAuthRequiredMessage(ctx, int64(telegramID), languageCode)
			return
		}
		sessionData = sd
	}

	if err := h.telegramService.HandleCallbackQuery(ctx, data, int64(telegramID), messageID, languageCode, sessionData); err != nil {
		h.logger.WithContext(ctx).Errorf("Ошибка обработки callback query: %v", err)
	}
}

// handleStartCommand обрабатывает команду /start с возможным токеном
func (h *TelegramHandler) handleStartCommand(ctx context.Context, chatID, telegramID int64, username, firstName, lastName, languageCode, authToken string) {
	if err := h.telegramService.HandleStartCommand(ctx, telegramID, username, firstName, lastName, languageCode, authToken); err != nil {
		h.logger.WithContext(ctx).Errorf("Ошибка обработки команды /start: %v", err)
	}
}

// handleTextMessage обрабатывает обычные текстовые сообщения
func (h *TelegramHandler) handleTextMessage(ctx context.Context, chatID, telegramID int64, text string, languageCode string, sessionData *middleware.SessionData) {
	// Обычные сообщения пока не обрабатываются
	// TODO: добавить обработку текстовых сообщений через сервис
	// sessionData содержит user_id и access_token для использования в обработчиках
	h.logger.WithContext(ctx).Debugf("Получено текстовое сообщение от user_id=%s (len=%d)", sessionData.UserID, len(text))
}

// handleAuthCommand обрабатывает команду /auth
func (h *TelegramHandler) handleAuthCommand(ctx context.Context, chatID, telegramID int64, text string, languageCode string) {
	// Извлекаем languageCode из данных пользователя если нужно
	if err := h.telegramService.HandleAuthCommand(ctx, telegramID, languageCode); err != nil {
		h.logger.WithContext(ctx).Errorf("Ошибка обработки команды /auth: %v", err)
	}
}

// sendAuthRequiredMessage отправляет сообщение о необходимости авторизации с кнопкой (новое сообщение)
func (h *TelegramHandler) sendAuthRequiredMessage(ctx context.Context, telegramID int64, languageCode string) {
	if err := h.telegramService.HandleCallbackQuery(ctx, "back_to_menu", telegramID, 0, languageCode, nil); err != nil {
		h.logger.WithContext(ctx).Warnf("Ошибка отправки сообщения авторизации для Telegram %d: %v", telegramID, err)
	}
}
