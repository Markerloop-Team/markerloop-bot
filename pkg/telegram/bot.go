package telegram

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

// Bot интерфейс для работы с Telegram Bot API
type Bot interface {
	SendMessage(chatID int64, text string, options *MessageOptions) error
	SendKeyboard(chatID int64, text string, keyboard *InlineKeyboardMarkup) error
	EditMessageText(chatID int64, messageID int64, text string, keyboard *InlineKeyboardMarkup) error
	AnswerCallbackQuery(callbackQueryID string) error
	SetMyCommands(commands []BotCommand) error
	SetChatMenuButton(webAppURL string) error
	SetWebhook(url string) error
	SetWebhookWithSecret(url string, secretToken string) error
	DeleteWebhook() error
	GetWebhookInfo() (*WebhookInfo, error)
	GetMe() (*BotInfo, error)
	StartPolling(handler UpdateHandler) error
	StopPolling()
}

// UpdateHandler обработчик обновлений от Telegram
type UpdateHandler func(update map[string]interface{}) error

// BotInfo информация о боте
type BotInfo struct {
	ID        int64  `json:"id"`
	IsBot     bool   `json:"is_bot"`
	FirstName string `json:"first_name"`
	Username  string `json:"username"`
}

// bot реализация Telegram бота
type bot struct {
	token       string
	client      *http.Client
	logger      *logrus.Logger
	apiURL      string
	polling     bool
	pollingStop chan bool
	lastUpdateID int64
}

// MessageOptions опции для отправки сообщения
type MessageOptions struct {
	ParseMode   string `json:"parse_mode,omitempty"`
	ReplyMarkup string `json:"reply_markup,omitempty"`
}

// InlineKeyboardMarkup клавиатура с кнопками
type InlineKeyboardMarkup struct {
	InlineKeyboard [][]InlineKeyboardButton `json:"inline_keyboard"`
}

// WebAppInfo информация о Mini App
type WebAppInfo struct {
	URL string `json:"url"`
}

// InlineKeyboardButton кнопка клавиатуры
type InlineKeyboardButton struct {
	Text         string      `json:"text"`
	CallbackData string      `json:"callback_data,omitempty"`
	URL          string      `json:"url,omitempty"`
	WebApp       *WebAppInfo `json:"web_app,omitempty"`
}

// BotCommand описание команды для меню бота
type BotCommand struct {
	Command     string `json:"command"`
	Description string `json:"description"`
}

// WebhookInfo информация о webhook
type WebhookInfo struct {
	URL                  string `json:"url"`
	HasCustomCertificate bool   `json:"has_custom_certificate"`
	PendingUpdateCount   int    `json:"pending_update_count"`
}

// NewBot создает новый экземпляр бота
func NewBot(token string, logger *logrus.Logger) Bot {
	return &bot{
		token:       token,
		logger:      logger,
		apiURL:      fmt.Sprintf("https://api.telegram.org/bot%s", token),
		client:      &http.Client{Timeout: 30 * time.Second},
		pollingStop: make(chan bool),
		lastUpdateID: 0,
	}
}

// SendMessage отправляет сообщение
func (b *bot) SendMessage(chatID int64, text string, options *MessageOptions) error {
	message := map[string]interface{}{
		"chat_id": chatID,
		"text":    text,
	}

	if options != nil {
		if options.ParseMode != "" {
			message["parse_mode"] = options.ParseMode
		}
		if options.ReplyMarkup != "" {
			message["reply_markup"] = options.ReplyMarkup
		}
	}

	return b.makeRequest("sendMessage", message)
}

// SendKeyboard отправляет сообщение с клавиатурой
func (b *bot) SendKeyboard(chatID int64, text string, keyboard *InlineKeyboardMarkup) error {
	keyboardJSON, err := json.Marshal(keyboard)
	if err != nil {
		return fmt.Errorf("ошибка сериализации клавиатуры: %w", err)
	}

	options := &MessageOptions{
		ParseMode:   "HTML",
		ReplyMarkup: string(keyboardJSON),
	}

	return b.SendMessage(chatID, text, options)
}

// EditMessageText редактирует текст существующего сообщения (inline keyboard опционально)
func (b *bot) EditMessageText(chatID int64, messageID int64, text string, keyboard *InlineKeyboardMarkup) error {
	params := map[string]interface{}{
		"chat_id":    chatID,
		"message_id": messageID,
		"text":       text,
		"parse_mode": "HTML",
	}

	if keyboard != nil {
		keyboardJSON, err := json.Marshal(keyboard)
		if err != nil {
			return fmt.Errorf("ошибка сериализации клавиатуры: %w", err)
		}
		params["reply_markup"] = json.RawMessage(keyboardJSON)
	}

	return b.makeRequest("editMessageText", params)
}

// AnswerCallbackQuery отвечает на callback query (убирает "часики" на кнопке)
func (b *bot) AnswerCallbackQuery(callbackQueryID string) error {
	params := map[string]interface{}{
		"callback_query_id": callbackQueryID,
	}
	return b.makeRequest("answerCallbackQuery", params)
}

// SetMyCommands устанавливает список команд бота (видны в меню Telegram)
func (b *bot) SetMyCommands(commands []BotCommand) error {
	params := map[string]interface{}{
		"commands": commands,
	}
	return b.makeRequest("setMyCommands", params)
}

// SetChatMenuButton устанавливает кнопку Mini App рядом с полем ввода
func (b *bot) SetChatMenuButton(webAppURL string) error {
	params := map[string]interface{}{
		"menu_button": map[string]interface{}{
			"type":    "web_app",
			"text":    "Open App",
			"web_app": map[string]string{"url": webAppURL},
		},
	}
	return b.makeRequest("setChatMenuButton", params)
}

// SetWebhook устанавливает webhook
func (b *bot) SetWebhook(url string) error {
	return b.SetWebhookWithSecret(url, "")
}

// SetWebhookWithSecret устанавливает webhook с secret token
func (b *bot) SetWebhookWithSecret(url string, secretToken string) error {
	params := map[string]interface{}{
		"url": url,
	}

	if secretToken != "" {
		params["secret_token"] = secretToken
	}

	return b.makeRequest("setWebhook", params)
}

// DeleteWebhook удаляет webhook
func (b *bot) DeleteWebhook() error {
	return b.makeRequest("deleteWebhook", nil)
}

// GetWebhookInfo получает информацию о webhook
func (b *bot) GetWebhookInfo() (*WebhookInfo, error) {
	var response struct {
		OK     bool        `json:"ok"`
		Result WebhookInfo `json:"result"`
	}

	err := b.makeRequestWithResponse("getWebhookInfo", nil, &response)
	if err != nil {
		return nil, err
	}

	if !response.OK {
		return nil, fmt.Errorf("ошибка получения информации о webhook")
	}

	return &response.Result, nil
}

// GetMe получает информацию о боте
func (b *bot) GetMe() (*BotInfo, error) {
	var response struct {
		OK     bool    `json:"ok"`
		Result BotInfo `json:"result"`
	}

	err := b.makeRequestWithResponse("getMe", nil, &response)
	if err != nil {
		return nil, err
	}

	if !response.OK {
		return nil, fmt.Errorf("ошибка получения информации о боте")
	}

	return &response.Result, nil
}

// StartPolling запускает polling для получения обновлений
func (b *bot) StartPolling(handler UpdateHandler) error {
	// Удаляем webhook если есть
	b.DeleteWebhook()
	
	b.polling = true
	
	go func() {
		for {
			select {
			case <-b.pollingStop:
				b.polling = false
				return
			default:
				updates, err := b.getUpdates()
				if err != nil {
					b.logger.Errorf("Ошибка получения обновлений: %v", err)
					time.Sleep(5 * time.Second)
					continue
				}
				
				for _, update := range updates {
					if err := handler(update); err != nil {
						b.logger.Errorf("Ошибка обработки обновления: %v", err)
					}
				}
				
				time.Sleep(1 * time.Second)
			}
		}
	}()
	
	b.logger.Info("Polling запущен")
	return nil
}

// StopPolling останавливает polling
func (b *bot) StopPolling() {
	if b.polling {
		b.pollingStop <- true
		b.logger.Info("Polling остановлен")
	}
}

// getUpdates получает обновления от Telegram
func (b *bot) getUpdates() ([]map[string]interface{}, error) {
	params := map[string]interface{}{
		"offset":  b.lastUpdateID + 1,
		"timeout": 10,
	}
	
	var response struct {
		OK     bool                     `json:"ok"`
		Result []map[string]interface{} `json:"result"`
	}
	
	err := b.makeRequestWithResponse("getUpdates", params, &response)
	if err != nil {
		return nil, err
	}
	
	if !response.OK {
		return nil, fmt.Errorf("ошибка получения обновлений")
	}
	
	// Обновляем lastUpdateID
	for _, update := range response.Result {
		if updateID, ok := update["update_id"].(float64); ok {
			if int64(updateID) > b.lastUpdateID {
				b.lastUpdateID = int64(updateID)
			}
		}
	}
	
	return response.Result, nil
}

// makeRequest выполняет запрос к Telegram API. Telegram сообщает об отказе
// полем ok=false с HTTP 200 или 4xx — такой ответ возвращается ошибкой с
// description, а не молчаливым успехом.
func (b *bot) makeRequest(method string, params map[string]interface{}) error {
	var response struct {
		OK          bool   `json:"ok"`
		ErrorCode   int    `json:"error_code"`
		Description string `json:"description"`
	}

	if err := b.makeRequestWithResponse(method, params, &response); err != nil {
		return err
	}
	if !response.OK {
		return fmt.Errorf("telegram %s отказал (error_code %d): %s", method, response.ErrorCode, response.Description)
	}
	return nil
}

// redactError убирает из ошибки токен бота. *url.Error несёт полный адрес
// запроса https://api.telegram.org/bot<TOKEN>/<method>, а ошибки уходят в
// журнал и Sentry — поэтому адрес отбрасываем, а токен на всякий случай
// заменяем и в оставшемся тексте.
func (b *bot) redactError(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		err = fmt.Errorf("%s: %w", urlErr.Op, urlErr.Err)
	}
	if b.token == "" || !strings.Contains(err.Error(), b.token) {
		return err
	}
	return errors.New(strings.ReplaceAll(err.Error(), b.token, "<redacted>"))
}

// makeRequestWithResponse выполняет запрос к Telegram API с получением ответа
func (b *bot) makeRequestWithResponse(method string, params map[string]interface{}, response interface{}) error {
	endpoint := fmt.Sprintf("%s/%s", b.apiURL, method)

	var body []byte
	var err error

	if params != nil {
		body, err = json.Marshal(params)
		if err != nil {
			return fmt.Errorf("ошибка сериализации параметров: %w", err)
		}
	}

	req, err := http.NewRequest("POST", endpoint, bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("ошибка создания запроса %s: %w", method, b.redactError(err))
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := b.client.Do(req)
	if err != nil {
		return fmt.Errorf("ошибка выполнения запроса %s: %w", method, b.redactError(err))
	}
	defer resp.Body.Close()

	if err := json.NewDecoder(resp.Body).Decode(response); err != nil {
		return fmt.Errorf("ошибка декодирования ответа: %w", err)
	}

	return nil
}
