package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sirupsen/logrus"

	"telegram-bot-service/internal/middleware"
	"telegram-bot-service/pkg/auth_client"
	"telegram-bot-service/pkg/i18n"
	"telegram-bot-service/pkg/telegram"
	"telegram-bot-service/pkg/user_client"

	// Импорт go-sdk
	sdkI18n "gitlab.com/markerloop/go-sdk/pkg/i18n"
	"gitlab.com/markerloop/go-sdk/pkg/redis"
	"gitlab.com/markerloop/go-sdk/pkg/requestid"
)

// TelegramService сервис для работы с Telegram ботом
type TelegramService struct {
	bot           telegram.Bot
	botUsername   string
	redisService  *redis.RedisService
	authClient    *auth_client.Client
	userClient    *user_client.Client
	localizer     *sdkI18n.Localizer
	logger        *logrus.Logger
	gatewayURL    string
	webAppURL     string
	httpClient    *http.Client
	oauthTokenTTL time.Duration
	defaultLocale sdkI18n.LanguageCode
}

// NewTelegramService создает новый сервис Telegram
func NewTelegramService(
	bot telegram.Bot,
	botUsername string,
	redisService *redis.RedisService,
	authClient *auth_client.Client,
	userClient *user_client.Client,
	localizer *sdkI18n.Localizer,
	logger *logrus.Logger,
	gatewayURL string,
	webAppURL string,
	httpClientTimeout time.Duration,
	oauthTokenTTL time.Duration,
	defaultLocale sdkI18n.LanguageCode,
) *TelegramService {
	service := &TelegramService{
		bot:           bot,
		botUsername:   botUsername,
		redisService:  redisService,
		authClient:    authClient,
		userClient:    userClient,
		localizer:     localizer,
		logger:        logger,
		gatewayURL:    gatewayURL,
		webAppURL:     webAppURL,
		httpClient:    &http.Client{Timeout: httpClientTimeout},
		oauthTokenTTL: oauthTokenTTL,
		defaultLocale: defaultLocale,
	}

	// Если username не указан, получаем через getMe
	if botUsername == "" {
		botInfo, err := bot.GetMe()
		if err != nil {
			logger.Warnf("Не удалось получить username бота через getMe: %v", err)
		} else {
			service.botUsername = botInfo.Username
			logger.Infof("Username бота получен: %s", botInfo.Username)
		}
	}

	// Регистрируем команды бота (меню в Telegram)
	commands := []telegram.BotCommand{
		{Command: "start", Description: "Welcome & authorization"},
		{Command: "menu", Description: "Main menu"},
		{Command: "profile", Description: "My profile"},
		{Command: "settings", Description: "Language settings"},
		{Command: "users", Description: "Users (superadmin)"},
		{Command: "stats", Description: "Statistics (superadmin)"},
		{Command: "notifications", Description: "Deploy alerts (superadmin)"},
		{Command: "logout", Description: "Log out"},
	}
	if err := bot.SetMyCommands(commands); err != nil {
		logger.Warnf("Не удалось установить команды бота: %v", err)
	} else {
		logger.Info("Команды бота зарегистрированы")
	}

	// Устанавливаем Menu Button (кнопка "Open App" рядом с полем ввода)
	if err := bot.SetChatMenuButton(webAppURL); err != nil {
		logger.Warnf("Не удалось установить Menu Button: %v", err)
	} else {
		logger.Info("Menu Button установлен")
	}

	return service
}

// HandleStartCommand обрабатывает команду /start с возможным токеном OAuth
func (s *TelegramService) HandleStartCommand(ctx context.Context, telegramID int64, username, firstName, lastName, languageCode, authToken string) error {
	locale := s.resolveLocale(telegramID, languageCode)

	// Устанавливаем язык для локализатора
	s.localizer.SetLanguage(sdkI18n.LanguageCode(locale))

	// Если есть токен, проверяем формат и обрабатываем соответствующим образом
	if authToken != "" {
		if strings.HasPrefix(authToken, "login_") {
			// Авторизация на сайте через бот (SSE flow)
			token := strings.TrimPrefix(authToken, "login_")
			return s.handleLoginFlow(ctx, telegramID, username, firstName, lastName, languageCode, token)
		} else if strings.HasPrefix(authToken, "link_") {
			// Это deeplink токен из auth-service (привязка аккаунта)
			token := strings.TrimPrefix(authToken, "link_")
			return s.handleDeeplinkFlow(ctx, telegramID, username, firstName, lastName, languageCode, token)
		} else {
			// Это OAuth токен (старый формат)
			return s.handleOAuthFlow(ctx, telegramID, username, firstName, lastName, languageCode, authToken)
		}
	}

	// Проверяем, не привязан ли уже аккаунт
	user, err := s.userClient.GetUserByExternalAccount(ctx, "telegram", fmt.Sprintf("%d", telegramID))
	if isDeletionPending(err) {
		// SDL-742: аккаунт удаляется — не регистрируем заново, а говорим об этом.
		return s.sendDeletionPending(ctx, telegramID, locale)
	}
	if err == nil && user != nil {
		// Аккаунт привязан — восстанавливаем сессию
		if err := s.restoreOrCreateSession(ctx, telegramID, user.ID, username); err != nil {
			s.logger.WithContext(ctx).Warnf("Ошибка восстановления сессии: %v", err)
		}

		// /start для авторизованных — приветствие + кнопка "Открыть меню"
		msg := s.localizer.Get(ctx, "telegram.bot.start.welcome")
		keyboard := &telegram.InlineKeyboardMarkup{
			InlineKeyboard: [][]telegram.InlineKeyboardButton{
				{{Text: s.localizer.Get(ctx, "telegram.bot.menu.main"), CallbackData: "back_to_menu"}},
			},
		}
		return s.bot.SendKeyboard(telegramID, msg, keyboard)
	}

	// Аккаунт не привязан — автоматическая регистрация
	return s.handleAutoRegistration(ctx, telegramID, username, firstName, lastName, languageCode)
}

// handleLoginFlow обрабатывает авторизацию на сайте через бот (SSE flow)
// Пользователь нажал "Войти через Telegram" на сайте → получил deeplink → нажал Start в боте
// Бот аутентифицирует пользователя и пишет JWT в Redis → сайт получает через SSE
func (s *TelegramService) handleLoginFlow(ctx context.Context, telegramID int64, username, firstName, lastName, languageCode, token string) error {
	locale := i18n.DetectLocale(languageCode)
	s.localizer.SetLanguage(sdkI18n.LanguageCode(locale))

	// 1. Проверяем токен в Redis
	loginKey := fmt.Sprintf("telegram_login:%s", token)
	loginDataJSON, err := s.redisService.Get(ctx, loginKey)
	if err != nil || loginDataJSON == "" {
		s.logger.WithContext(ctx).Warnf("Неверный или истекший токен: flow=login tg_id=%d %s", telegramID, tokenFingerprint(token))
		msg := s.localizer.Get(ctx, "telegram.bot.auth.token_invalid")
		return s.bot.SendMessage(telegramID, msg, &telegram.MessageOptions{ParseMode: "HTML"})
	}

	var loginData map[string]interface{}
	if err := json.Unmarshal([]byte(loginDataJSON), &loginData); err != nil {
		s.logger.WithContext(ctx).Errorf("Ошибка парсинга login токена: %v", err)
		msg := s.localizer.Get(ctx, "telegram.bot.auth.error")
		return s.bot.SendMessage(telegramID, msg, &telegram.MessageOptions{ParseMode: "HTML"})
	}

	status, _ := loginData["status"].(string)
	if status != "pending" {
		s.logger.WithContext(ctx).Warnf("Токен уже использован: flow=login tg_id=%d %s", telegramID, tokenFingerprint(token))
		msg := s.localizer.Get(ctx, "telegram.bot.auth.token_invalid")
		return s.bot.SendMessage(telegramID, msg, &telegram.MessageOptions{ParseMode: "HTML"})
	}

	// 2. Ищем пользователя по Telegram ID
	telegramIDStr := fmt.Sprintf("%d", telegramID)
	user, err := s.userClient.GetUserByExternalAccount(ctx, "telegram", telegramIDStr)
	if isDeletionPending(err) {
		// SDL-742: Telegram ID принадлежит удаляемому аккаунту — не заводим
		// tg_{id} заново и не выдаём токены.
		return s.sendDeletionPending(ctx, telegramID, locale)
	}

	var userID, userUsername string

	if err != nil || user == nil {
		// 3. Telegram ID не привязан — создаём нового пользователя
		// БЕЗОПАСНОСТЬ: НЕ привязываем к существующему пользователю по username,
		// так как Telegram username можно подделать. Привязка к существующему
		// аккаунту возможна только через deeplink из личного кабинета (link_ prefix).
		registrationUsername := fmt.Sprintf("tg_%d", telegramID)

		randomPassword := s.generateRandomPassword()
		createReq := &user_client.CreateUserRequest{
			Username: registrationUsername,
			Password: randomPassword,
		}

		newUser, err := s.userClient.CreateUser(ctx, createReq)
		if err != nil {
			if strings.Contains(err.Error(), "уже существует") {
				// Пользователь tg_{id} уже создан ранее, но external_account потерялся
				// Находим его и привязываем заново
				existingUser, lookupErr := s.userClient.GetUserByUsername(ctx, registrationUsername)
				if isDeletionPending(lookupErr) {
					return s.sendDeletionPending(ctx, telegramID, locale)
				}
				if lookupErr != nil {
					s.logger.WithContext(ctx).Errorf("Ошибка поиска пользователя tg_id=%d: %v", telegramID, lookupErr)
					msg := s.localizer.Get(ctx, "telegram.bot.auth.error")
					return s.bot.SendMessage(telegramID, msg, &telegram.MessageOptions{ParseMode: "HTML"})
				}
				newUser = existingUser
				err = nil
			}
			if err != nil {
				s.logger.WithContext(ctx).Errorf("Ошибка создания пользователя: %v", err)
				msg := s.localizer.Get(ctx, "telegram.bot.auth.error")
				return s.bot.SendMessage(telegramID, msg, &telegram.MessageOptions{ParseMode: "HTML"})
			}
		}

		// Создаем external_account для привязки Telegram ID
		displayName := fmt.Sprintf("%s %s", firstName, lastName)
		externalAccountReq := &user_client.ExternalAccountRequest{
			UserID:       newUser.ID,
			Provider:     "telegram",
			ExternalID:   telegramIDStr,
			Username:     &username,
			DisplayName:  &displayName,
			ProviderData: s.buildProviderData(username, firstName, lastName, languageCode),
		}
		s.userClient.CreateExternalAccount(ctx, externalAccountReq)

		userID = newUser.ID
		userUsername = newUser.Username
	} else {
		userID = user.ID
		userUsername = user.Username
	}

	// 4. Запрашиваем у auth-service пару токенов для уже идентифицированного user_id.
	tokenPair, err := s.authClient.IssueTokensForUser(userID, userUsername, "")
	if isDeletionPending(err) {
		return s.sendDeletionPending(ctx, telegramID, locale)
	}
	if err != nil {
		s.logger.WithContext(ctx).Errorf("Ошибка получения токенов из auth-service для login flow: %v", err)
		msg := s.localizer.Get(ctx, "telegram.bot.auth.error")
		return s.bot.SendMessage(telegramID, msg, &telegram.MessageOptions{ParseMode: "HTML"})
	}

	// 5. Записываем результат в Redis (SSE подхватит)
	resultData := map[string]interface{}{
		"status":        "completed",
		"access_token":  tokenPair.AccessToken,
		"refresh_token": tokenPair.RefreshToken,
		"token_type":    tokenPair.TokenType,
		"expires_in":    tokenPair.ExpiresIn,
	}
	resultJSON, _ := json.Marshal(resultData)
	s.redisService.Set(ctx, loginKey, string(resultJSON), 30*time.Second)

	// 6. Создаём сессию в боте
	if err := s.createSession(ctx, telegramID, userID, userUsername); err != nil {
		s.logger.WithContext(ctx).Warnf("Ошибка создания сессии после login flow: %v", err)
	}

	// 7. Сохраняем refresh token в Redis (как при обычном логине)
	refreshKey := "refresh:" + userID
	s.redisService.Set(ctx, refreshKey, tokenPair.RefreshToken, 24*time.Hour)

	// 8. Отправляем сообщение пользователю
	s.logger.WithContext(ctx).Infof("Telegram login flow: user_id=%s авторизован на сайте", userID)
	successMsg := s.localizer.Get(ctx, "telegram.bot.auth.web_login_success")
	if successMsg == "" {
		successMsg = "Вы успешно авторизованы на сайте! Вернитесь в браузер."
	}
	keyboard := s.buildPostAuthKeyboard(ctx, telegramID, locale)
	return s.bot.SendKeyboard(telegramID, successMsg, keyboard)
}

// handleDeeplinkFlow обрабатывает deeplink flow с токеном из auth-service
func (s *TelegramService) handleDeeplinkFlow(ctx context.Context, telegramID int64, username, firstName, lastName, languageCode, token string) error {
	locale := i18n.DetectLocale(languageCode)
	s.localizer.SetLanguage(sdkI18n.LanguageCode(locale))

	// Проверяем токен в Redis (токен хранится там с user_id и created_at)
	key := fmt.Sprintf("telegram_link:%s", token)
	linkDataJSON, err := s.redisService.Get(ctx, key)
	if err != nil || linkDataJSON == "" {
		s.logger.WithContext(ctx).Warnf("Неверный или истекший токен: flow=deeplink tg_id=%d %s", telegramID, tokenFingerprint(token))
		msg := s.localizer.Get(ctx, "telegram.bot.auth.token_invalid")
		msg += "\n\n" + s.localizer.Get(ctx, "telegram.bot.auth.generate_new_link")
		return s.bot.SendMessage(telegramID, msg, &telegram.MessageOptions{
			ParseMode: "HTML",
		})
	}

	// Парсим данные из Redis
	var linkData map[string]interface{}
	if err := json.Unmarshal([]byte(linkDataJSON), &linkData); err != nil {
		s.logger.WithContext(ctx).Errorf("Ошибка парсинга данных deeplink токена: %v", err)
		msg := s.localizer.Get(ctx, "telegram.bot.auth.error")
		return s.bot.SendMessage(telegramID, msg, &telegram.MessageOptions{
			ParseMode: "HTML",
		})
	}

	userID, ok := linkData["user_id"].(string)
	if !ok || userID == "" {
		s.logger.WithContext(ctx).Warnf("Неверный формат данных deeplink токена")
		msg := s.localizer.Get(ctx, "telegram.bot.auth.token_invalid")
		return s.bot.SendMessage(telegramID, msg, &telegram.MessageOptions{
			ParseMode: "HTML",
		})
	}

	// Проверяем существование external_account для этого пользователя и Telegram
	existingUser, err := s.userClient.GetUserByExternalAccount(ctx, "telegram", fmt.Sprintf("%d", telegramID))
	if isDeletionPending(err) {
		return s.sendDeletionPending(ctx, telegramID, locale) // SDL-742
	}
	if err == nil && existingUser != nil {
		// External account уже существует - обновляем last_used_at
		// TODO: добавить метод обновления last_used_at в user-service
		s.logger.WithContext(ctx).Infof("External account уже существует для Telegram %d", telegramID)
	} else {
		// Создаем external_account
		externalAccountReq := &user_client.ExternalAccountRequest{
			UserID:       userID,
			Provider:     "telegram",
			ExternalID:   fmt.Sprintf("%d", telegramID),
			Username:     &username,
			DisplayName:  func() *string { s := fmt.Sprintf("%s %s", firstName, lastName); return &s }(),
			ProviderData: s.buildProviderData(username, firstName, lastName, languageCode),
		}

		_, err = s.userClient.CreateExternalAccount(ctx, externalAccountReq)
		if err != nil {
			s.logger.WithContext(ctx).Errorf("Ошибка создания external_account: %v", err)
			msg := s.localizer.Get(ctx, "telegram.bot.auth.error")
			return s.bot.SendMessage(telegramID, msg, &telegram.MessageOptions{
				ParseMode: "HTML",
			})
		}
	}

	// Создаем сессию в Redis
	if err := s.createSession(ctx, telegramID, userID, username); err != nil {
		if isDeletionPending(err) {
			return s.sendDeletionPending(ctx, telegramID, locale) // SDL-742
		}
		s.logger.WithContext(ctx).Warnf("Ошибка создания сессии после deeplink: %v", err)
		// Не критично, продолжаем
	}

	// Удаляем токен deeplink из Redis
	s.redisService.Del(ctx, key)

	s.logger.WithContext(ctx).Infof("Telegram аккаунт %d успешно привязан к пользователю %s через deeplink", telegramID, userID)

	// Показываем успешное сообщение и главное меню
	successMsg := s.localizer.Get(ctx, "telegram.bot.auth.success")
	welcomeMsg := s.localizer.Get(ctx, "telegram.bot.start.message")
	message := fmt.Sprintf("%s\n\n%s", successMsg, welcomeMsg)
	keyboard := s.buildPostAuthKeyboard(ctx, telegramID, locale)
	return s.bot.SendKeyboard(telegramID, message, keyboard)
}

// handleOAuthFlow обрабатывает OAuth flow с токеном
func (s *TelegramService) handleOAuthFlow(ctx context.Context, telegramID int64, username, firstName, lastName, languageCode, authToken string) error {
	locale := i18n.DetectLocale(languageCode)
	s.localizer.SetLanguage(sdkI18n.LanguageCode(locale))

	// Проверяем токен в Redis (токен хранится там с user_id)
	key := fmt.Sprintf("oauth_token:%s", authToken)
	userID, err := s.redisService.Get(ctx, key)
	if err != nil || userID == "" {
		s.logger.WithContext(ctx).Warnf("Неверный или истекший токен: flow=oauth tg_id=%d %s", telegramID, tokenFingerprint(authToken))
		msg := s.localizer.Get(ctx, "telegram.bot.auth.token_invalid")
		return s.bot.SendMessage(telegramID, msg, &telegram.MessageOptions{
			ParseMode: "HTML",
		})
	}

	// Проверяем существование ключа (если Get вернул ошибку, значит токен истек)
	exists, err := s.redisService.Exists(ctx, key)
	if err != nil || exists == 0 {
		msg := s.localizer.Get(ctx, "telegram.bot.auth.token_expired")
		return s.bot.SendMessage(telegramID, msg, &telegram.MessageOptions{
			ParseMode: "HTML",
		})
	}

	// Привязываем аккаунт через user-service
	req := &user_client.ExternalAccountRequest{
		AuthToken:    authToken,
		Provider:     "telegram",
		ExternalID:   fmt.Sprintf("%d", telegramID),
		Username:     &username,
		DisplayName:  func() *string { s := fmt.Sprintf("%s %s", firstName, lastName); return &s }(),
		ProviderData: s.buildProviderData(username, firstName, lastName, languageCode),
	}

	account, err := s.userClient.LinkExternalAccount(ctx, req)
	if err != nil {
		s.logger.WithContext(ctx).Errorf("Ошибка привязки аккаунта: %v", err)
		msg := s.localizer.Get(ctx, "telegram.bot.auth.error")
		return s.bot.SendMessage(telegramID, msg, &telegram.MessageOptions{
			ParseMode: "HTML",
		})
	}

	// Удаляем токен из Redis
	s.redisService.Del(ctx, key)

	s.logger.WithContext(ctx).Infof("Telegram аккаунт %d успешно привязан к пользователю %s", telegramID, account.UserID)

	// Создаем сессию в Redis
	if err := s.createSession(ctx, telegramID, account.UserID, username); err != nil {
		if isDeletionPending(err) {
			return s.sendDeletionPending(ctx, telegramID, locale) // SDL-742
		}
		s.logger.WithContext(ctx).Warnf("Ошибка создания сессии после OAuth: %v", err)
		// Не критично, продолжаем
	}

	// Показываем успешное сообщение и главное меню
	successMsg := s.localizer.Get(ctx, "telegram.bot.auth.success")
	welcomeMsg := s.localizer.Get(ctx, "telegram.bot.start.message")
	message := fmt.Sprintf("%s\n\n%s", successMsg, welcomeMsg)
	keyboard := s.buildPostAuthKeyboard(ctx, telegramID, locale)
	return s.bot.SendKeyboard(telegramID, message, keyboard)
}

// buildProviderData создает JSON с дополнительными данными провайдера
func (s *TelegramService) buildProviderData(username, firstName, lastName, languageCode string) string {
	data := map[string]interface{}{
		"username":      username,
		"first_name":    firstName,
		"last_name":     lastName,
		"language_code": languageCode,
	}
	jsonData, _ := json.Marshal(data)
	return string(jsonData)
}

// getUserRoles получает роли пользователя через gateway
func (s *TelegramService) getUserRoles(ctx context.Context, accessToken string) []string {
	access, err := s.userClient.GetUserAccess(ctx, s.gatewayURL, accessToken)
	if err != nil {
		s.logger.WithContext(ctx).Warnf("Ошибка получения ролей пользователя: %v", err)
		return []string{"user"}
	}
	if len(access.Roles) == 0 {
		return []string{"user"}
	}
	return access.Roles
}

// hasRole проверяет наличие роли в списке
func hasRole(roles []string, role string) bool {
	for _, r := range roles {
		if r == role {
			return true
		}
	}
	return false
}

// buildPostAuthKeyboard создает клавиатуру после авторизации — пытается получить роли
func (s *TelegramService) buildPostAuthKeyboard(ctx context.Context, telegramID int64, locale i18n.Locale) *telegram.InlineKeyboardMarkup {
	session, err := s.CheckSession(ctx, telegramID)
	if err != nil || session == nil {
		return s.createMainKeyboard(locale)
	}
	roles := s.getUserRoles(ctx, session.AccessToken)
	return s.createRoleKeyboard(locale, roles)
}

// createMainKeyboard создает главную клавиатуру с кнопками (без авторизации)
func (s *TelegramService) createMainKeyboard(locale i18n.Locale) *telegram.InlineKeyboardMarkup {
	ctx := context.Background()
	authButtonText := s.localizer.Get(ctx, "telegram.bot.button.auth")

	return &telegram.InlineKeyboardMarkup{
		InlineKeyboard: [][]telegram.InlineKeyboardButton{
			{
				{Text: authButtonText, CallbackData: "auth"},
			},
		},
	}
}

// createRoleKeyboard создает клавиатуру на основе ролей пользователя
func (s *TelegramService) createRoleKeyboard(locale i18n.Locale, roles []string) *telegram.InlineKeyboardMarkup {
	ctx := context.Background()

	var rows [][]telegram.InlineKeyboardButton

	// Кнопка "Открыть сайт" (Mini App) для всех авторизованных
	rows = append(rows, []telegram.InlineKeyboardButton{
		{Text: "🌐 " + s.localizer.Get(ctx, "telegram.bot.button.open_site"), WebApp: &telegram.WebAppInfo{URL: s.webAppURL}},
	})

	if hasRole(roles, "superadmin") {
		// Superadmin layout (2 колонки)
		rows = append(rows, []telegram.InlineKeyboardButton{
			{Text: s.localizer.Get(ctx, "telegram.bot.button.profile"), CallbackData: "profile"},
			{Text: s.localizer.Get(ctx, "telegram.bot.button.users"), CallbackData: "users_list"},
		})
		rows = append(rows, []telegram.InlineKeyboardButton{
			{Text: s.localizer.Get(ctx, "telegram.bot.button.stats"), CallbackData: "stats"},
			{Text: s.localizer.Get(ctx, "telegram.bot.button.notifications"), CallbackData: "notifications"},
		})
		rows = append(rows, []telegram.InlineKeyboardButton{
			{Text: s.localizer.Get(ctx, "telegram.bot.button.settings"), CallbackData: "settings"},
			{Text: s.localizer.Get(ctx, "telegram.bot.button.logout"), CallbackData: "logout"},
		})
	} else {
		// User layout
		rows = append(rows, []telegram.InlineKeyboardButton{
			{Text: s.localizer.Get(ctx, "telegram.bot.button.profile"), CallbackData: "profile"},
		})
		rows = append(rows, []telegram.InlineKeyboardButton{
			{Text: s.localizer.Get(ctx, "telegram.bot.button.settings"), CallbackData: "settings"},
		})
		rows = append(rows, []telegram.InlineKeyboardButton{
			{Text: s.localizer.Get(ctx, "telegram.bot.button.logout"), CallbackData: "logout"},
		})
	}

	return &telegram.InlineKeyboardMarkup{
		InlineKeyboard: rows,
	}
}

// HandleAuthButton обрабатывает нажатие кнопки "Авторизация"
func (s *TelegramService) HandleAuthButton(ctx context.Context, telegramID int64, languageCode string) error {
	return s.HandleAuthCommand(ctx, telegramID, languageCode)
}

// HandleAuthCommand обрабатывает команду /auth - начинает интерактивный диалог
func (s *TelegramService) HandleAuthCommand(ctx context.Context, telegramID int64, languageCode string) error {
	locale := i18n.DetectLocale(languageCode)
	s.localizer.SetLanguage(sdkI18n.LanguageCode(locale))

	// Проверяем, не привязан ли уже аккаунт
	user, err := s.userClient.GetUserByExternalAccount(ctx, "telegram", fmt.Sprintf("%d", telegramID))
	if isDeletionPending(err) {
		return s.sendDeletionPending(ctx, telegramID, locale) // SDL-742
	}
	if err == nil && user != nil {
		// Аккаунт уже привязан — восстанавливаем сессию и показываем кнопку "Меню"
		if err := s.restoreOrCreateSession(ctx, telegramID, user.ID, ""); err != nil {
			s.logger.WithContext(ctx).Warnf("Ошибка восстановления сессии при auth: %v", err)
		}
		msg := s.localizer.Get(ctx, "telegram.bot.auth.already_linked")
		keyboard := &telegram.InlineKeyboardMarkup{
			InlineKeyboard: [][]telegram.InlineKeyboardButton{
				{{Text: s.localizer.Get(ctx, "telegram.bot.menu.main"), CallbackData: "back_to_menu"}},
			},
		}
		return s.bot.SendKeyboard(telegramID, msg, keyboard)
	}

	// Начинаем интерактивный диалог - запрашиваем username
	authState := map[string]interface{}{
		"step": "waiting_username",
	}
	authStateJSON, _ := json.Marshal(authState)
	authStateKey := fmt.Sprintf("auth_dialog:%d", telegramID)
	s.redisService.Set(ctx, authStateKey, string(authStateJSON), 10*time.Minute) // TTL 10 минут

	msg := s.localizer.Get(ctx, "telegram.bot.auth.enter_username")
	return s.bot.SendMessage(telegramID, msg, &telegram.MessageOptions{
		ParseMode: "HTML",
	})
}

// HandleAuthMessage обрабатывает сообщения в рамках диалога /auth
func (s *TelegramService) HandleAuthMessage(ctx context.Context, telegramID int64, text, languageCode string) error {
	locale := i18n.DetectLocale(languageCode)
	s.localizer.SetLanguage(sdkI18n.LanguageCode(locale))

	// Получаем состояние диалога из Redis
	authStateKey := fmt.Sprintf("auth_dialog:%d", telegramID)
	authStateJSON, err := s.redisService.Get(ctx, authStateKey)
	if err != nil || authStateJSON == "" {
		// Состояние не найдено - игнорируем сообщение
		return nil
	}

	var authState map[string]interface{}
	if err := json.Unmarshal([]byte(authStateJSON), &authState); err != nil {
		s.logger.WithContext(ctx).Warnf("Ошибка парсинга состояния диалога: %v", err)
		return nil
	}

	step, ok := authState["step"].(string)
	if !ok {
		return nil
	}

	switch step {
	case "waiting_username":
		return s.handleAuthUsername(ctx, telegramID, text, languageCode, authState, authStateKey)
	case "waiting_password":
		return s.handleAuthPassword(ctx, telegramID, text, languageCode, authState, authStateKey)
	case "waiting_email":
		return s.handleAuthEmail(ctx, telegramID, text, languageCode, authState, authStateKey)
	default:
		// Неизвестный шаг - очищаем состояние
		s.redisService.Del(ctx, authStateKey)
		return nil
	}
}

// handleAuthUsername обрабатывает ввод username
func (s *TelegramService) handleAuthUsername(ctx context.Context, telegramID int64, username, languageCode string, authState map[string]interface{}, authStateKey string) error {
	locale := i18n.DetectLocale(languageCode)
	s.localizer.SetLanguage(sdkI18n.LanguageCode(locale))

	if username == "" {
		msg := s.localizer.Get(ctx, "telegram.bot.auth.username_required")
		return s.bot.SendMessage(telegramID, msg, nil)
	}

	// Сохраняем username в состоянии
	authState["username"] = username
	authState["step"] = "waiting_password"
	authStateJSON, _ := json.Marshal(authState)
	s.redisService.Set(ctx, authStateKey, string(authStateJSON), 10*time.Minute)

	msg := s.localizer.Get(ctx, "telegram.bot.auth.enter_password")
	return s.bot.SendMessage(telegramID, msg, nil)
}

// handleAuthPassword обрабатывает ввод password и пытается авторизоваться
func (s *TelegramService) handleAuthPassword(ctx context.Context, telegramID int64, password, languageCode string, authState map[string]interface{}, authStateKey string) error {
	locale := i18n.DetectLocale(languageCode)
	s.localizer.SetLanguage(sdkI18n.LanguageCode(locale))

	if password == "" {
		msg := s.localizer.Get(ctx, "telegram.bot.auth.password_required")
		return s.bot.SendMessage(telegramID, msg, nil)
	}

	username, _ := authState["username"].(string)
	if username == "" {
		// Начинаем заново
		s.redisService.Del(ctx, authStateKey)
		return s.HandleAuthCommand(ctx, telegramID, languageCode)
	}

	// Пытаемся авторизоваться через auth-service
	loginReq := map[string]string{
		"username": username,
		"password": password,
	}

	loginResp, err := s.callAuthServiceLogin(ctx, loginReq)
	if err != nil {
		// Если логин не удался, возможно пользователь не существует - запрашиваем email для регистрации
		if strings.Contains(err.Error(), "неверные учетные данные") || strings.Contains(err.Error(), "не найден") {
			// Сохраняем password и переходим к запросу email
			authState["password"] = password
			authState["step"] = "waiting_email"
			authStateJSON, _ := json.Marshal(authState)
			s.redisService.Set(ctx, authStateKey, string(authStateJSON), 10*time.Minute)

			msg := s.localizer.Get(ctx, "telegram.bot.auth.user_not_found_enter_email")
			return s.bot.SendMessage(telegramID, msg, nil)
		}

		// Другая ошибка
		s.redisService.Del(ctx, authStateKey)
		msg := s.localizer.Get(ctx, "telegram.bot.auth.login_error")
		return s.bot.SendMessage(telegramID, msg, nil)
	}

	// Логин успешен - создаем сессию и external_account
	return s.completeAuth(ctx, telegramID, loginResp, username, languageCode, authStateKey)
}

// handleAuthEmail обрабатывает ввод email и регистрирует пользователя
func (s *TelegramService) handleAuthEmail(ctx context.Context, telegramID int64, email, languageCode string, authState map[string]interface{}, authStateKey string) error {
	locale := i18n.DetectLocale(languageCode)
	s.localizer.SetLanguage(sdkI18n.LanguageCode(locale))

	username, _ := authState["username"].(string)
	password, _ := authState["password"].(string)

	if username == "" || password == "" {
		s.redisService.Del(ctx, authStateKey)
		return s.HandleAuthCommand(ctx, telegramID, languageCode)
	}

	// Регистрируем пользователя через auth-service
	registerReq := map[string]interface{}{
		"username": username,
		"password": password,
	}
	if email != "" && strings.Contains(email, "@") {
		registerReq["email"] = email
	}

	registerResp, err := s.callAuthServiceRegister(ctx, registerReq)
	if err != nil {
		s.redisService.Del(ctx, authStateKey)
		if strings.Contains(err.Error(), "уже существует") {
			msg := s.localizer.Get(ctx, "telegram.bot.auth.user_exists_use_login")
			return s.bot.SendMessage(telegramID, msg, nil)
		}
		msg := s.localizer.Get(ctx, "telegram.bot.auth.registration_error")
		return s.bot.SendMessage(telegramID, msg, nil)
	}

	// Регистрация успешна - создаем сессию и external_account
	return s.completeAuth(ctx, telegramID, registerResp, username, languageCode, authStateKey)
}

// completeAuth завершает процесс авторизации/регистрации
func (s *TelegramService) completeAuth(ctx context.Context, telegramID int64, authResp map[string]interface{}, username, languageCode string, authStateKey string) error {
	locale := i18n.DetectLocale(languageCode)
	s.localizer.SetLanguage(sdkI18n.LanguageCode(locale))

	// Извлекаем токены и user_id из ответа auth-service.
	// Phase -1.4: AuthResponse теперь явно возвращает user_id, поэтому
	// бот больше не парсит access_token самостоятельно.
	accessToken, _ := authResp["access_token"].(string)
	refreshToken, _ := authResp["refresh_token"].(string)
	userID, _ := authResp["user_id"].(string)

	if accessToken == "" || refreshToken == "" || userID == "" {
		s.redisService.Del(ctx, authStateKey)
		msg := s.localizer.Get(ctx, "telegram.bot.auth.error")
		return s.bot.SendMessage(telegramID, msg, nil)
	}

	// Создаем или обновляем external_account
	externalAccountReq := &user_client.ExternalAccountRequest{
		UserID:     userID,
		Provider:   "telegram",
		ExternalID: fmt.Sprintf("%d", telegramID),
		Username:   &username,
	}

	if _, err := s.userClient.CreateExternalAccount(ctx, externalAccountReq); err != nil {
		s.logger.WithContext(ctx).Warnf("Ошибка создания external_account: %v", err)
		// Не критично, продолжаем
	}

	// Создаем сессию в Redis
	if err := s.createSession(ctx, telegramID, userID, username); err != nil {
		s.logger.WithContext(ctx).Warnf("Ошибка создания сессии: %v", err)
		// Не критично, продолжаем
	}

	// Удаляем состояние диалога
	s.redisService.Del(ctx, authStateKey)

	// Отправляем сообщение об успехе
	msg := s.localizer.Get(ctx, "telegram.bot.auth.success")
	keyboard := s.buildPostAuthKeyboard(ctx, telegramID, locale)
	return s.bot.SendKeyboard(telegramID, msg, keyboard)
}

// callAuthServiceLogin вызывает auth-service для логина
func (s *TelegramService) callAuthServiceLogin(ctx context.Context, req map[string]string) (map[string]interface{}, error) {
	url := fmt.Sprintf("%s/api/v1/auth/login", s.gatewayURL)

	jsonData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("ошибка сериализации запроса: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(jsonData)))
	if err != nil {
		return nil, fmt.Errorf("ошибка создания запроса: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	requestid.Inject(ctx, httpReq)

	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ошибка выполнения запроса: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("ошибка чтения ответа: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("auth-service вернул статус %d: %s", resp.StatusCode, string(body))
	}

	var apiResp struct {
		Data    map[string]interface{} `json:"data"`
		Message string                 `json:"message"`
		Success bool                   `json:"success"`
	}

	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("ошибка десериализации ответа: %w", err)
	}

	return apiResp.Data, nil
}

// callAuthServiceRegister вызывает auth-service для регистрации
func (s *TelegramService) callAuthServiceRegister(ctx context.Context, req map[string]interface{}) (map[string]interface{}, error) {
	url := fmt.Sprintf("%s/api/v1/auth/register", s.gatewayURL)

	jsonData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("ошибка сериализации запроса: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(jsonData)))
	if err != nil {
		return nil, fmt.Errorf("ошибка создания запроса: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	requestid.Inject(ctx, httpReq)

	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ошибка выполнения запроса: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("ошибка чтения ответа: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("auth-service вернул статус %d: %s", resp.StatusCode, string(body))
	}

	var apiResp struct {
		Data    map[string]interface{} `json:"data"`
		Message string                 `json:"message"`
		Success bool                   `json:"success"`
	}

	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("ошибка десериализации ответа: %w", err)
	}

	return apiResp.Data, nil
}

// SendNotification отправляет уведомление пользователю
func (s *TelegramService) SendNotification(ctx context.Context, userID string, title, message string) error {
	// Получаем Telegram аккаунт пользователя через user-service
	// Нужно найти external account по user_id
	// Для этого нужно добавить endpoint в user-service для получения external account по user_id и provider

	// Временная реализация - используем Redis для кэширования
	telegramIDKey := fmt.Sprintf("user_telegram_id:%s", userID)
	telegramIDStr, err := s.redisService.Get(ctx, telegramIDKey)
	if err != nil || telegramIDStr == "" {
		s.logger.WithContext(ctx).Warnf("Telegram аккаунт не найден для пользователя %s", userID)
		return nil // Не считаем это ошибкой
	}

	// Парсим telegram_id
	var telegramID int64
	if _, err := fmt.Sscanf(telegramIDStr, "%d", &telegramID); err != nil {
		return fmt.Errorf("ошибка парсинга telegram_id: %w", err)
	}

	// Получаем формат из dictionary-service
	s.localizer.SetLanguage(s.defaultLocale) // Используем язык по умолчанию из конфигурации
	titleFormat := s.localizer.Get(ctx, "telegram.bot.notification.title_format")
	formattedMessage := fmt.Sprintf(titleFormat, title, message)

	// Отправляем сообщение
	if err := s.bot.SendMessage(telegramID, formattedMessage, &telegram.MessageOptions{
		ParseMode: "HTML",
	}); err != nil {
		return fmt.Errorf("ошибка отправки уведомления: %w", err)
	}

	s.logger.WithContext(ctx).Infof("Уведомление отправлено пользователю %s в Telegram", userID)
	return nil
}

// SendBroadcastNotification отправляет уведомление всем активным пользователям
func (s *TelegramService) SendBroadcastNotification(ctx context.Context, title, message string) error {
	// Для broadcast нужно получать список всех пользователей с Telegram
	// Это требует endpoint в user-service для получения всех external accounts по provider
	// Временная реализация - просто логируем
	s.logger.WithContext(ctx).Warn("SendBroadcastNotification требует endpoint для получения всех telegram пользователей")
	return nil
}

// SendMessage отправляет сообщение через бота
func (s *TelegramService) SendMessage(chatID int64, text string, options *telegram.MessageOptions) error {
	return s.bot.SendMessage(chatID, text, options)
}

// SendKeyboard отправляет сообщение с клавиатурой
func (s *TelegramService) SendKeyboard(chatID int64, text string, keyboard *telegram.InlineKeyboardMarkup) error {
	return s.bot.SendKeyboard(chatID, text, keyboard)
}

// GenerateOAuthToken генерирует токен для OAuth привязки и сохраняет в Redis
func (s *TelegramService) GenerateOAuthToken(ctx context.Context, userID string) (string, error) {
	// Генерируем токен через user-service
	token, err := s.userClient.GenerateAuthToken(ctx, userID, "telegram")
	if err != nil {
		s.logger.WithContext(ctx).Errorf("Ошибка генерации токена через user-service: %v", err)
		// Fallback: генерируем токен локально, если user-service недоступен.
		// Только случайные байты: токен из user_id и времени угадывается (SDL-738).
		buf := make([]byte, 16)
		if _, rerr := rand.Read(buf); rerr != nil {
			return "", fmt.Errorf("ошибка генерации fallback токена: %w", rerr)
		}
		token = hex.EncodeToString(buf)
		s.logger.WithContext(ctx).Warnf("Использован fallback токен: flow=fallback %s", tokenFingerprint(token))
	}

	// Сохраняем токен в Redis с TTL из конфигурации
	key := fmt.Sprintf("oauth_token:%s", token)
	if err := s.redisService.Set(ctx, key, userID, s.oauthTokenTTL); err != nil {
		return "", fmt.Errorf("ошибка сохранения токена в Redis: %w", err)
	}

	return token, nil
}

// GenerateOAuthDeepLink генерирует Telegram deep link для OAuth авторизации
// Формат: https://t.me/BOT_USERNAME?start=TOKEN
func (s *TelegramService) GenerateOAuthDeepLink(ctx context.Context, userID string) (string, error) {
	if s.botUsername == "" {
		return "", fmt.Errorf("username бота не установлен")
	}

	token, err := s.GenerateOAuthToken(ctx, userID)
	if err != nil {
		return "", err
	}

	// Формируем deep link в формате https://t.me/BOT_USERNAME?start=TOKEN
	deepLink := fmt.Sprintf("https://t.me/%s?start=%s", s.botUsername, token)
	return deepLink, nil
}

// handleAutoRegistration обрабатывает автоматическую регистрацию пользователя
func (s *TelegramService) handleAutoRegistration(ctx context.Context, telegramID int64, username, firstName, lastName, languageCode string) error {
	locale := i18n.DetectLocale(languageCode)
	s.localizer.SetLanguage(sdkI18n.LanguageCode(locale))

	// Определяем username для регистрации
	var registrationUsername string
	if username != "" {
		// Убираем @ если есть
		registrationUsername = strings.TrimPrefix(username, "@")
		// Проверяем уникальность username
		// Пока просто используем username из Telegram, если он валиден
		// В будущем можно добавить проверку через user-service
		registrationUsername = strings.ToLower(registrationUsername)
	} else {
		// Если нет username, генерируем временный (будет заменен позже)
		registrationUsername = fmt.Sprintf("user_%d", telegramID)
	}

	// Генерируем случайный пароль (не используется для Telegram, но обязателен)
	randomPassword := s.generateRandomPassword()

	// Создаем пользователя через user-service
	createReq := &user_client.CreateUserRequest{
		Username: registrationUsername,
		Email:    nil, // Email можно добавить позже
		Password: randomPassword,
	}

	user, err := s.userClient.CreateUser(ctx, createReq)
	if err != nil {
		if strings.Contains(err.Error(), "уже существует") {
			// Пользователь уже есть — ищем его и привязываем external_account
			existingUser, lookupErr := s.userClient.GetUserByUsername(ctx, registrationUsername)
			if lookupErr != nil && !isDeletionPending(lookupErr) {
				// Пробуем fallback username
				fallbackUsername := fmt.Sprintf("user_%d", telegramID)
				existingUser, lookupErr = s.userClient.GetUserByUsername(ctx, fallbackUsername)
			}
			if isDeletionPending(lookupErr) {
				// SDL-742: найденный по логину аккаунт удаляется — не
				// привязываемся к нему и не выдаём токены.
				return s.sendDeletionPending(ctx, telegramID, locale)
			}
			if lookupErr != nil {
				s.logger.WithContext(ctx).Errorf("Пользователь существует, но не найден по username: %v", lookupErr)
				msg := s.localizer.Get(ctx, "telegram.bot.auth.error")
				return s.bot.SendMessage(telegramID, msg, &telegram.MessageOptions{ParseMode: "HTML"})
			}
			user = existingUser
		} else {
			s.logger.WithContext(ctx).Errorf("Ошибка создания пользователя: %v", err)
			msg := s.localizer.Get(ctx, "telegram.bot.auth.error")
			return s.bot.SendMessage(telegramID, msg, &telegram.MessageOptions{ParseMode: "HTML"})
		}
	}

	// Создаем external_account
	externalAccountReq := &user_client.ExternalAccountRequest{
		UserID:       user.ID,
		Provider:     "telegram",
		ExternalID:   fmt.Sprintf("%d", telegramID),
		Username:     &username,
		DisplayName:  func() *string { s := fmt.Sprintf("%s %s", firstName, lastName); return &s }(),
		ProviderData: s.buildProviderData(username, firstName, lastName, languageCode),
	}

	_, err = s.userClient.CreateExternalAccount(ctx, externalAccountReq)
	if err != nil {
		s.logger.WithContext(ctx).Errorf("Ошибка создания external_account: %v", err)
		// Пользователь создан, но external_account не создан - это не критично
		// Можно продолжить
	}

	// Создаем сессию в Redis
	if err := s.createSession(ctx, telegramID, user.ID, username); err != nil {
		if isDeletionPending(err) {
			return s.sendDeletionPending(ctx, telegramID, locale) // SDL-742
		}
		s.logger.WithContext(ctx).Warnf("Ошибка создания сессии: %v", err)
		// Не критично, продолжаем
	}

	// Отправляем приветственное сообщение
	welcomeMsg := s.localizer.Get(ctx, "telegram.bot.start.welcome")
	registrationMsg := s.localizer.Get(ctx, "telegram.bot.start.registration_success")
	message := fmt.Sprintf("%s\n\n%s", welcomeMsg, registrationMsg)
	keyboard := s.buildPostAuthKeyboard(ctx, telegramID, locale)

	s.logger.WithContext(ctx).Infof("Пользователь %s автоматически зарегистрирован через Telegram %d", user.ID, telegramID)
	return s.bot.SendKeyboard(telegramID, message, keyboard)
}

// restoreOrCreateSession восстанавливает или создает сессию для пользователя
func (s *TelegramService) restoreOrCreateSession(ctx context.Context, telegramID int64, userID, username string) error {
	sessionKey := fmt.Sprintf("telegram_session:%d", telegramID)

	// Проверяем существование сессии
	exists, err := s.redisService.Exists(ctx, sessionKey)
	if err == nil && exists > 0 {
		// Сессия существует - обновляем TTL (sliding expiration)
		sessionData, err := s.redisService.Get(ctx, sessionKey)
		if err == nil && sessionData != "" {
			// Обновляем TTL на 7 дней
			if err := s.redisService.Set(ctx, sessionKey, sessionData, 7*24*time.Hour); err != nil {
				s.logger.WithContext(ctx).Warnf("Ошибка обновления TTL сессии: %v", err)
			}
			return nil
		}
	}

	// Сессия не найдена или истекла - создаем новую
	return s.createSession(ctx, telegramID, userID, username)
}

// createSession создает сессию в Redis
func (s *TelegramService) createSession(ctx context.Context, telegramID int64, userID, username string) error {
	// Запрашиваем токены у auth-service (бот не держит JWT signing key).
	tokenPair, err := s.authClient.IssueTokensForUser(userID, username, "")
	if err != nil {
		return fmt.Errorf("ошибка получения токенов из auth-service: %w", err)
	}

	// Создаем структуру сессии
	sessionData := map[string]interface{}{
		"user_id":       userID,
		"access_token":  tokenPair.AccessToken,
		"refresh_token": tokenPair.RefreshToken,
		"expires_at":    time.Now().Add(7 * 24 * time.Hour).Unix(),
	}

	sessionJSON, err := json.Marshal(sessionData)
	if err != nil {
		return fmt.Errorf("ошибка сериализации сессии: %w", err)
	}

	// Сохраняем сессию в Redis с TTL 7 дней
	sessionKey := fmt.Sprintf("telegram_session:%d", telegramID)
	if err := s.redisService.Set(ctx, sessionKey, string(sessionJSON), 7*24*time.Hour); err != nil {
		return fmt.Errorf("ошибка сохранения сессии: %w", err)
	}

	s.logger.WithContext(ctx).Infof("Сессия создана для Telegram %d, user_id=%s", telegramID, userID)
	return nil
}

// generateRandomPassword генерирует случайный пароль
func (s *TelegramService) generateRandomPassword() string {
	// Генерируем 32 байта случайных данных
	bytes := make([]byte, 16)
	rand.Read(bytes)
	// Конвертируем в hex строку (32 символа)
	return hex.EncodeToString(bytes)
}

// CheckSession проверяет сессию пользователя и возвращает данные сессии
func (s *TelegramService) CheckSession(ctx context.Context, telegramID int64) (*middleware.SessionData, error) {
	cfg := middleware.SessionMiddlewareConfig{
		RedisService: s.redisService,
		AuthClient:   s.authClient,
		Logger:       s.logger,
	}
	return middleware.CheckSession(cfg, telegramID)
}

// AnswerCallback отвечает на callback query (убирает "часики")
func (s *TelegramService) AnswerCallback(ctx context.Context, callbackQueryID string) {
	s.bot.AnswerCallbackQuery(callbackQueryID)
}

// sendOrEdit — если messageID > 0, редактирует сообщение; иначе отправляет новое
func (s *TelegramService) sendOrEdit(ctx context.Context, chatID, messageID int64, text string, keyboard *telegram.InlineKeyboardMarkup) error {
	if messageID > 0 {
		return s.bot.EditMessageText(chatID, messageID, text, keyboard)
	}
	if keyboard != nil {
		return s.bot.SendKeyboard(chatID, text, keyboard)
	}
	return s.bot.SendMessage(chatID, text, &telegram.MessageOptions{ParseMode: "HTML"})
}

// resolveLocale определяет язык: сначала из Redis, потом из Telegram languageCode
func (s *TelegramService) resolveLocale(telegramID int64, languageCode string) i18n.Locale {
	ctx := context.Background()
	langKey := fmt.Sprintf("telegram_lang:%d", telegramID)
	savedLang, err := s.redisService.Get(ctx, langKey)
	if err == nil && savedLang != "" {
		return i18n.DetectLocale(savedLang)
	}
	return i18n.DetectLocale(languageCode)
}

// HandleCallbackQuery обрабатывает нажатия на кнопки
func (s *TelegramService) HandleCallbackQuery(ctx context.Context, callbackData string, telegramID int64, messageID int64, languageCode string, sessionData *middleware.SessionData) error {
	locale := s.resolveLocale(telegramID, languageCode)
	s.localizer.SetLanguage(sdkI18n.LanguageCode(locale))

	switch callbackData {
	case "auth":
		return s.HandleAuthButton(ctx, telegramID, languageCode)
	case "back_to_menu":
		return s.handleBackToMenu(ctx, telegramID, messageID, languageCode, sessionData)
	case "profile":
		return s.handleProfile(ctx, telegramID, messageID, languageCode, sessionData)
	case "settings":
		return s.handleSettings(ctx, telegramID, messageID, languageCode)
	case "lang_ru", "lang_en", "lang_kk":
		return s.handleLanguageChoice(ctx, telegramID, messageID, callbackData)
	case "logout":
		return s.handleLogout(ctx, telegramID, messageID, languageCode)
	case "users_list":
		return s.handleUsersList(ctx, telegramID, messageID, languageCode, sessionData)
	case "stats":
		return s.handleStats(ctx, telegramID, messageID, languageCode, sessionData)
	case "notifications":
		return s.handleNotifications(ctx, telegramID, messageID, languageCode, sessionData)
	case "notify_enable":
		return s.handleDeployToggle(ctx, telegramID, messageID, true, languageCode)
	case "notify_disable":
		return s.handleDeployToggle(ctx, telegramID, messageID, false, languageCode)
	default:
		msg := s.localizer.Get(ctx, "telegram.bot.command.unknown")
		return s.bot.SendMessage(telegramID, msg, nil)
	}
}

// handleBackToMenu возвращает в главное меню
func (s *TelegramService) handleBackToMenu(ctx context.Context, telegramID, messageID int64, languageCode string, sessionData *middleware.SessionData) error {
	if sessionData == nil {
		keyboard := s.createMainKeyboard(s.resolveLocale(telegramID, languageCode))
		msg := s.localizer.Get(ctx, "telegram.bot.start.welcome")
		return s.sendOrEdit(ctx, telegramID, messageID, msg, keyboard)
	}

	roles := s.getUserRoles(ctx, sessionData.AccessToken)
	keyboard := s.createRoleKeyboard(s.resolveLocale(telegramID, languageCode), roles)
	msg := s.localizer.Get(ctx, "telegram.bot.menu.main")
	return s.sendOrEdit(ctx, telegramID, messageID, msg, keyboard)
}

// handleProfile показывает профиль пользователя
func (s *TelegramService) handleProfile(ctx context.Context, telegramID, messageID int64, languageCode string, sessionData *middleware.SessionData) error {
	if sessionData == nil {
		return s.sendAuthRequired(ctx, telegramID, languageCode)
	}

	user, err := s.userClient.GetUserByExternalAccount(ctx, "telegram", fmt.Sprintf("%d", telegramID))
	if err != nil {
		s.logger.WithContext(ctx).Warnf("Ошибка получения пользователя: %v", err)
		msg := s.localizer.Get(ctx, "telegram.bot.error.occurred")
		return s.bot.SendMessage(telegramID, msg, nil)
	}

	roles := s.getUserRoles(ctx, sessionData.AccessToken)

	email := "-"
	if user.Email != nil && *user.Email != "" {
		email = *user.Email
	}

	roleName := "user"
	if len(roles) > 0 {
		roleName = roles[0]
	}

	msg := fmt.Sprintf(
		"%s\n\n<b>Username:</b> %s\n<b>Email:</b> %s\n<b>%s:</b> %s\n<b>%s:</b> %s",
		s.localizer.Get(ctx, "telegram.bot.profile.title"),
		user.Username,
		email,
		s.localizer.Get(ctx, "telegram.bot.profile.role"),
		roleName,
		s.localizer.Get(ctx, "telegram.bot.profile.registered"),
		user.CreatedAt,
	)

	keyboard := &telegram.InlineKeyboardMarkup{
		InlineKeyboard: [][]telegram.InlineKeyboardButton{
			{{Text: s.localizer.Get(ctx, "telegram.bot.button.back"), CallbackData: "back_to_menu"}},
		},
	}

	return s.sendOrEdit(ctx, telegramID, messageID, msg, keyboard)
}

// handleSettings показывает настройки (язык)
func (s *TelegramService) handleSettings(ctx context.Context, telegramID, messageID int64, languageCode string) error {
	msg := s.localizer.Get(ctx, "telegram.bot.language.choose")

	keyboard := &telegram.InlineKeyboardMarkup{
		InlineKeyboard: [][]telegram.InlineKeyboardButton{
			{
				{Text: "🇷🇺 Русский", CallbackData: "lang_ru"},
				{Text: "🇬🇧 English", CallbackData: "lang_en"},
				{Text: "🇰🇿 Қазақша", CallbackData: "lang_kk"},
			},
			{{Text: s.localizer.Get(ctx, "telegram.bot.button.back"), CallbackData: "back_to_menu"}},
		},
	}

	return s.sendOrEdit(ctx, telegramID, messageID, msg, keyboard)
}

// handleLanguageChoice обрабатывает выбор языка и показывает обновлённые настройки
func (s *TelegramService) handleLanguageChoice(ctx context.Context, telegramID, messageID int64, callbackData string) error {
	var newLang string
	switch callbackData {
	case "lang_ru":
		newLang = "ru"
	case "lang_en":
		newLang = "en"
	case "lang_kk":
		newLang = "kk"
	}

	// Сохраняем язык в Redis
	langKey := fmt.Sprintf("telegram_lang:%d", telegramID)
	s.redisService.Set(ctx, langKey, newLang, 0)

	// Переключаем на новый язык
	locale := i18n.DetectLocale(newLang)
	s.localizer.SetLanguage(sdkI18n.LanguageCode(locale))

	msg := s.localizer.Get(ctx, "telegram.bot.language.changed")
	keyboard := &telegram.InlineKeyboardMarkup{
		InlineKeyboard: [][]telegram.InlineKeyboardButton{
			{{Text: s.localizer.Get(ctx, "telegram.bot.button.back"), CallbackData: "back_to_menu"}},
		},
	}

	return s.sendOrEdit(ctx, telegramID, messageID, msg, keyboard)
}

// handleLogout очищает сессию и показывает гостевое меню
func (s *TelegramService) handleLogout(ctx context.Context, telegramID, messageID int64, languageCode string) error {
	locale := s.resolveLocale(telegramID, languageCode)

	// Удаляем сессию из Redis
	sessionKey := fmt.Sprintf("telegram_session:%d", telegramID)
	s.redisService.Del(ctx, sessionKey)

	msg := s.localizer.Get(ctx, "telegram.bot.logout.success")
	keyboard := s.createMainKeyboard(locale)
	return s.sendOrEdit(ctx, telegramID, messageID, msg, keyboard)
}

// handleUsersList показывает список пользователей (superadmin)
func (s *TelegramService) handleUsersList(ctx context.Context, telegramID, messageID int64, languageCode string, sessionData *middleware.SessionData) error {
	if sessionData == nil {
		return s.sendAuthRequired(ctx, telegramID, languageCode)
	}

	usersResp, err := s.userClient.GetUsers(ctx, s.gatewayURL, sessionData.AccessToken, 1, 10)
	if err != nil {
		s.logger.WithContext(ctx).Warnf("Ошибка получения списка пользователей: %v", err)
		msg := s.localizer.Get(ctx, "telegram.bot.error.occurred")
		return s.bot.SendMessage(telegramID, msg, nil)
	}

	msg := s.localizer.Get(ctx, "telegram.bot.users.title") + "\n\n"
	for i, user := range usersResp.Data.Users {
		email := "-"
		if user.Email != nil && *user.Email != "" {
			email = *user.Email
		}
		msg += fmt.Sprintf("%d. <b>%s</b> | %s\n", i+1, user.Username, email)
	}
	msg += fmt.Sprintf("\n%s: %d", s.localizer.Get(ctx, "telegram.bot.users.total"), usersResp.Data.Total)

	keyboard := &telegram.InlineKeyboardMarkup{
		InlineKeyboard: [][]telegram.InlineKeyboardButton{
			{{Text: s.localizer.Get(ctx, "telegram.bot.button.back"), CallbackData: "back_to_menu"}},
		},
	}

	return s.sendOrEdit(ctx, telegramID, messageID, msg, keyboard)
}

// handleStats показывает статистику (superadmin)
func (s *TelegramService) handleStats(ctx context.Context, telegramID, messageID int64, languageCode string, sessionData *middleware.SessionData) error {
	if sessionData == nil {
		return s.sendAuthRequired(ctx, telegramID, languageCode)
	}

	// Получаем количество пользователей
	totalUsers := 0
	usersResp, err := s.userClient.GetUsers(ctx, s.gatewayURL, sessionData.AccessToken, 1, 1)
	if err != nil {
		s.logger.WithContext(ctx).Warnf("Ошибка получения кол-ва пользователей: %v", err)
	} else {
		totalUsers = usersResp.Data.Total
	}

	// Получаем количество ролей
	totalRoles := 0
	rolesResp, err := s.userClient.GetRoles(ctx, s.gatewayURL, sessionData.AccessToken)
	if err != nil {
		s.logger.WithContext(ctx).Warnf("Ошибка получения кол-ва ролей: %v", err)
	} else {
		totalRoles = rolesResp.Total
	}

	deploySubs := len(s.getDeploySubscribers())

	msg := fmt.Sprintf(
		"%s\n\n<b>%s:</b> %d\n<b>%s:</b> %d\n<b>%s:</b> %d",
		s.localizer.Get(ctx, "telegram.bot.stats.title"),
		s.localizer.Get(ctx, "telegram.bot.stats.total_users"), totalUsers,
		s.localizer.Get(ctx, "telegram.bot.stats.total_roles"), totalRoles,
		s.localizer.Get(ctx, "telegram.bot.stats.deploy_subscribers"), deploySubs,
	)

	keyboard := &telegram.InlineKeyboardMarkup{
		InlineKeyboard: [][]telegram.InlineKeyboardButton{
			{{Text: s.localizer.Get(ctx, "telegram.bot.button.back"), CallbackData: "back_to_menu"}},
		},
	}

	return s.sendOrEdit(ctx, telegramID, messageID, msg, keyboard)
}

// getDeploySubscribers возвращает список подписчиков на деплой-уведомления
func (s *TelegramService) getDeploySubscribers() []int64 {
	ctx := context.Background()
	data, err := s.redisService.Get(ctx, "deploy_subscribers")
	if err != nil || data == "" {
		return nil
	}
	var subs []int64
	json.Unmarshal([]byte(data), &subs)
	return subs
}

// setDeploySubscribers сохраняет список подписчиков
func (s *TelegramService) setDeploySubscribers(subs []int64) {
	ctx := context.Background()
	data, _ := json.Marshal(subs)
	s.redisService.Set(ctx, "deploy_subscribers", string(data), 0)
}

// isDeploySubscriber проверяет, подписан ли пользователь
func (s *TelegramService) isDeploySubscriber(telegramID int64) bool {
	for _, id := range s.getDeploySubscribers() {
		if id == telegramID {
			return true
		}
	}
	return false
}

// handleNotifications показывает настройки уведомлений о деплое (superadmin)
func (s *TelegramService) handleNotifications(ctx context.Context, telegramID, messageID int64, languageCode string, sessionData *middleware.SessionData) error {
	if sessionData == nil {
		return s.sendAuthRequired(ctx, telegramID, languageCode)
	}

	subscribed := s.isDeploySubscriber(telegramID)

	var statusMsg, buttonText, buttonAction string
	if subscribed {
		statusMsg = s.localizer.Get(ctx, "telegram.bot.notifications.enabled")
		buttonText = s.localizer.Get(ctx, "telegram.bot.notifications.disable")
		buttonAction = "notify_disable"
	} else {
		statusMsg = s.localizer.Get(ctx, "telegram.bot.notifications.disabled")
		buttonText = s.localizer.Get(ctx, "telegram.bot.notifications.enable")
		buttonAction = "notify_enable"
	}

	msg := fmt.Sprintf("%s\n\n%s\n\n%s",
		s.localizer.Get(ctx, "telegram.bot.notifications.title"),
		statusMsg,
		s.localizer.Get(ctx, "telegram.bot.notifications.description"),
	)

	keyboard := &telegram.InlineKeyboardMarkup{
		InlineKeyboard: [][]telegram.InlineKeyboardButton{
			{{Text: buttonText, CallbackData: buttonAction}},
			{{Text: s.localizer.Get(ctx, "telegram.bot.button.back"), CallbackData: "back_to_menu"}},
		},
	}

	return s.sendOrEdit(ctx, telegramID, messageID, msg, keyboard)
}

// handleDeployToggle подписывает/отписывает от уведомлений о деплое
func (s *TelegramService) handleDeployToggle(ctx context.Context, telegramID, messageID int64, enable bool, languageCode string) error {
	subs := s.getDeploySubscribers()

	if enable {
		// Добавляем если ещё нет
		if !s.isDeploySubscriber(telegramID) {
			subs = append(subs, telegramID)
			s.setDeploySubscribers(subs)
		}
	} else {
		// Удаляем
		var newSubs []int64
		for _, id := range subs {
			if id != telegramID {
				newSubs = append(newSubs, id)
			}
		}
		s.setDeploySubscribers(newSubs)
	}

	// Показываем обновлённый экран уведомлений
	return s.handleNotifications(ctx, telegramID, messageID, languageCode, &middleware.SessionData{})
}

// sendAuthRequired отправляет сообщение о необходимости авторизации
func (s *TelegramService) sendAuthRequired(ctx context.Context, telegramID int64, languageCode string) error {
	locale := i18n.DetectLocale(languageCode)
	s.localizer.SetLanguage(sdkI18n.LanguageCode(locale))

	msg := s.localizer.Get(ctx, "telegram.bot.auth.required")
	keyboard := s.createMainKeyboard(locale)
	return s.bot.SendKeyboard(telegramID, msg, keyboard)
}

// HandleDialogMessage обрабатывает текстовые сообщения в рамках диалогов меню
func (s *TelegramService) HandleDialogMessage(ctx context.Context, telegramID int64, text, languageCode string, sessionData *middleware.SessionData) (bool, error) {
	// Нет активных text-based диалогов в текущей реализации
	return false, nil
}

// DeployNotificationPayload carries every field the bot might receive
// from notification-service, which proxies the ArgoCD Notifications
// webhook. Every field except App is optional — older payloads only
// carry App/Status/Revision and the renderer falls back gracefully.
//
// EventType discriminates ArgoCD trigger ids:
//   - "on-deployed"        — sync Succeeded + Healthy   (🚀)
//   - "on-sync-running"    — sync started               (🟡)
//   - "on-sync-failed"     — sync errored               (🔴)
//   - "on-health-degraded" — app became Degraded        (⚠️)
//
// and the dev-infra regression alerts (SDL-715, regression_state.go):
//   - "regression-failed"  — rendered as before, also marks the app red
//   - "regression-passed"  — "✅ регресс снова зелёный" only after red
//
// Anything else falls back to a neutral "📦 Update".
type DeployNotificationPayload struct {
	App          string
	Status       string
	Revision     string
	EventType    string
	RevisionURL  string
	ArgocdURL    string
	RepoURL      string
	StartedAt    string
	FinishedAt   string
	Message      string
	HealthStatus string
	SyncStatus   string
}

// formatDeployNotification renders the payload as an HTML message ready
// for the Telegram Bot API (parse_mode=HTML). Pure function — separate
// so it can be unit-tested without a bot.
func formatDeployNotification(p DeployNotificationPayload) string {
	icon, headline := deployHeadlineFor(p.EventType, p.Status, p.HealthStatus)

	var b strings.Builder
	fmt.Fprintf(&b, "%s <b>%s</b> — %s\n", icon, htmlEscape(p.App), headline)

	// Status / health line: include whichever sub-statuses we got.
	statusLine := strings.TrimSpace(strings.Join(nonEmpty(p.SyncStatus, p.HealthStatus, p.Status), " · "))
	if statusLine != "" {
		fmt.Fprintf(&b, "<i>%s</i>\n", htmlEscape(statusLine))
	}

	// Time: prefer FinishedAt, fall back to StartedAt.
	if t := formatTimestamp(firstNonEmpty(p.FinishedAt, p.StartedAt)); t != "" {
		fmt.Fprintf(&b, "🕒 %s\n", htmlEscape(t))
	}

	// Revision (with optional commit link).
	if p.Revision != "" {
		short := p.Revision
		if len(short) > 8 {
			short = short[:8]
		}
		if p.RevisionURL != "" {
			fmt.Fprintf(&b, "📝 <a href=\"%s\"><code>%s</code></a>\n",
				htmlEscape(p.RevisionURL), htmlEscape(short))
		} else {
			fmt.Fprintf(&b, "📝 <code>%s</code>\n", htmlEscape(short))
		}
	}

	// ArgoCD link.
	if p.ArgocdURL != "" {
		fmt.Fprintf(&b, "🔗 <a href=\"%s\">ArgoCD</a>\n", htmlEscape(p.ArgocdURL))
	}

	// Detail / error message — keep the last 600 chars to stay well
	// inside the 4096-char Telegram limit even when stacked with the
	// header lines above.
	if msg := strings.TrimSpace(p.Message); msg != "" {
		if len(msg) > 600 {
			msg = msg[:600] + "…"
		}
		fmt.Fprintf(&b, "\n<pre>%s</pre>", htmlEscape(msg))
	}

	return strings.TrimRight(b.String(), "\n")
}

// deployHeadlineFor maps the ArgoCD trigger id to (icon, headline). When
// EventType is empty (older payloads) the fallback uses Status alone so
// the bot keeps working through the pre-MR-C window.
func deployHeadlineFor(eventType, status, health string) (string, string) {
	switch eventType {
	case "on-deployed":
		return "🚀", "Deployed"
	case "on-sync-running":
		return "🟡", "Sync running"
	case "on-sync-failed":
		return "🔴", "Sync failed"
	case "on-health-degraded":
		return "⚠️", "Health degraded"
	}
	// Fallback path used by older notification-service / pre-MR-C
	// argocd template. Best-effort guess from the status string.
	switch strings.ToLower(status) {
	case "healthy", "succeeded":
		return "🚀", "Deployed"
	case "degraded":
		return "⚠️", "Health degraded"
	case "failed", "error":
		return "🔴", "Failed"
	default:
		_ = health
		return "📦", "Update"
	}
}

// formatTimestamp accepts ISO-8601 strings (the shape ArgoCD emits) and
// renders them as `YYYY-MM-DD HH:MM UTC`. Anything unparseable is
// returned verbatim so we never lose the value the operator sees.
func formatTimestamp(s string) string {
	if s == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		// ArgoCD sometimes uses RFC3339Nano; fall back to that, then
		// give up gracefully.
		t, err = time.Parse(time.RFC3339Nano, s)
		if err != nil {
			return s
		}
	}
	return t.UTC().Format("2006-01-02 15:04 UTC")
}

func nonEmpty(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// htmlEscape covers the four characters Telegram's HTML parse_mode
// treats as markup. Avoids pulling html.EscapeString to keep the
// output minimal (no &#39; etc.).
func htmlEscape(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		"\"", "&quot;",
	)
	return r.Replace(s)
}

// SendDeployNotification рассылает уведомление о деплое подписанным superadmin'ам.
//
// The legacy 3-string signature kept for backward-compat with any
// caller that still uses it; it forwards to the rich path with empty
// extras so the formatter falls back gracefully.
func (s *TelegramService) SendDeployNotification(ctx context.Context, app, status, revision string) error {
	return s.SendDeployNotificationPayload(ctx, DeployNotificationPayload{
		App:      app,
		Status:   status,
		Revision: revision,
	})
}

// SendDeployNotificationPayload is the rich-payload entrypoint used
// once notification-service starts forwarding the extended fields.
func (s *TelegramService) SendDeployNotificationPayload(ctx context.Context, p DeployNotificationPayload) error {
	_, err := s.DispatchDeployNotification(ctx, p)
	return err
}

// DispatchDeployNotification маршрутизирует deploy-событие (SDL-715) и
// сообщает, ушло ли сообщение: sent=false — regression-passed без прошлого
// красного (или с недоступным состоянием), молчание намеренное.
func (s *TelegramService) DispatchDeployNotification(ctx context.Context, p DeployNotificationPayload) (bool, error) {
	if p.App == "" {
		return false, fmt.Errorf("deploy notification: empty app name")
	}
	tracker := regressionTracker{store: s.redisService, logger: s.logger}
	return dispatchDeployEvent(ctx, tracker, p, func(msg string) error {
		return s.deliverDeployMessage(ctx, p, msg)
	})
}

// deliverDeployMessage рассылает готовое сообщение подписчикам deploy-нотификаций.
func (s *TelegramService) deliverDeployMessage(ctx context.Context, p DeployNotificationPayload, msg string) error {
	subs := s.getDeploySubscribers()
	if len(subs) == 0 {
		s.logger.WithContext(ctx).Info("Нет подписчиков на deploy notifications")
		return nil
	}

	delivered := fanOutDeploy(ctx, s.bot, s.logger, subs, msg)

	// SDL-345: полный провал рассылки обязан всплыть ошибкой (и HTTP 500),
	// иначе idempotency-ключ отметится по 200 и ретрай отправителя будет
	// подавлен — уведомление, которое не получил НИКТО, потеряется навсегда.
	// Частичный успех остаётся 200: повторная доставка всем ради отставших
	// дала бы дубли получившим (per-subscriber дедупа нет — слепая зона
	// в REGRESSION.md).
	if delivered == 0 {
		return fmt.Errorf("deploy notification: не доставлено ни одному из %d подписчиков", len(subs))
	}

	s.logger.WithContext(ctx).Infof(
		"Deploy notification отправлен %d/%d подписчикам: app=%s event=%s status=%s",
		delivered, len(subs), p.App, p.EventType, p.Status,
	)
	return nil
}

// fanOutDeploy рассылает сообщение подписчикам и возвращает число успешных
// доставок. Вынесена из SendDeployNotificationPayload тем же приёмом, что
// formatDeployNotification: TelegramService держит конкретный
// *redis.RedisService, и целиком метод без живого Redis не исполнить,
// а инвариант «0 из N — это ошибка» обязан быть под тестом (SDL-345).
func fanOutDeploy(ctx context.Context, bot telegram.Bot, logger *logrus.Logger, subs []int64, msg string) int {
	delivered := 0
	for _, telegramID := range subs {
		if err := bot.SendMessage(telegramID, msg, &telegram.MessageOptions{ParseMode: "HTML"}); err != nil {
			logger.WithContext(ctx).Warnf("Ошибка отправки deploy notification пользователю %d: %v", telegramID, err)
			continue
		}
		delivered++
	}
	return delivered
}
