package dictionary_client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"
)

// Client клиент для работы с dictionary-service
type Client struct {
	baseURL    string
	httpClient *http.Client
	logger     *logrus.Logger
}

// NewClient создает новый клиент dictionary-service
func NewClient(baseURL string, logger *logrus.Logger) *Client {
	return &Client{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
		logger:     logger,
	}
}

// LanguageCode код языка
type LanguageCode string

const (
	LanguageRU LanguageCode = "ru"
	LanguageKK LanguageCode = "kk"
	LanguageEN LanguageCode = "en"
)

// GetDictionaryRequest запрос на получение словаря
// Язык определяется из заголовка Accept-Language
type GetDictionaryRequest struct {
	Keys []string `json:"keys,omitempty"`
}

// DictionaryEntry запись словаря
type DictionaryEntry struct {
	Key    string            `json:"key"`
	Values map[string]string `json:"values"` // language -> translation
}

// APIResponse стандартный ответ API
type APIResponse struct {
	Success bool                       `json:"success"`
	Data    map[string]DictionaryEntry `json:"data"`
	Message string                     `json:"message"`
}

// GetDictionary получает словарь по ключам
func (c *Client) GetDictionary(language LanguageCode, keys []string) (map[string]string, error) {
	// baseURL уже содержит путь до gateway + /api/v1/dictionary
	// поэтому добавляем только /words
	url := fmt.Sprintf("%s/words", c.baseURL)

	reqBody := GetDictionaryRequest{
		Keys: keys, // Language больше не нужен, берется из заголовка
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("ошибка сериализации запроса: %w", err)
	}

	httpReq, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("ошибка создания запроса: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept-Language", string(language))

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ошибка выполнения запроса: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("ошибка чтения ответа: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		c.logger.Errorf("dictionary-service вернул статус %d: %s", resp.StatusCode, string(body))
		return nil, fmt.Errorf("dictionary-service вернул статус %d", resp.StatusCode)
	}

	var apiResp APIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("ошибка десериализации ответа: %w", err)
	}

	// Преобразуем DictionaryEntry в map[string]string
	result := make(map[string]string)
	for key, entry := range apiResp.Data {
		if translation, ok := entry.Values[string(language)]; ok {
			result[key] = translation
		} else if translation, ok := entry.Values[string(LanguageRU)]; ok {
			// Fallback на русский
			result[key] = translation
		} else {
			// Fallback на первый доступный язык
			for _, value := range entry.Values {
				result[key] = value
				break
			}
		}
	}

	return result, nil
}

// GetTranslation получает один перевод по ключу
func (c *Client) GetTranslation(language LanguageCode, key string) (string, error) {
	translations, err := c.GetDictionary(language, []string{key})
	if err != nil {
		return "", err
	}

	if translation, ok := translations[key]; ok {
		return translation, nil
	}

	// Если перевод не найден, возвращаем ключ
	return key, nil
}
