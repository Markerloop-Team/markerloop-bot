package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

// Config структура конфигурации сервиса
type Config struct {
	Server        ServerConfig
	Redis         RedisConfig
	Telegram      TelegramConfig
	Services      ServicesConfig
	OAuth         OAuthConfig
	HTTP          HTTPConfig
	Notifications NotificationsConfig
}

// NotificationsConfig — приём уведомлений от notification-service.
type NotificationsConfig struct {
	// IdempotencyTTL — окно, в котором повторная доставка с тем же
	// Idempotency-Key подавляется (SDL-345). Ретраи отправителя живут
	// секунды, час — с большим запасом.
	IdempotencyTTL time.Duration
}

// OAuthConfig конфигурация OAuth
type OAuthConfig struct {
	TokenTTL time.Duration
}

// HTTPConfig конфигурация HTTP клиента
type HTTPConfig struct {
	ClientTimeout time.Duration
}

// ServerConfig конфигурация сервера
type ServerConfig struct {
	Host         string
	Port         string
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	IdleTimeout  time.Duration
}

// RedisConfig конфигурация Redis
type RedisConfig struct {
	Addr     string
	Password string
	DB       int
}

// TelegramConfig конфигурация Telegram бота
type TelegramConfig struct {
	BotToken      string
	BotUsername   string // Username бота (без @), если не указан - будет получен через getMe
	WebhookURL    string
	WebhookSecret string
	WebAppURL     string // Публичный URL Mini App (кнопка меню и inline-клавиатуры)
}

// ServicesConfig конфигурация внешних сервисов
type ServicesConfig struct {
	Gateway      string
	Auth         string
	User         string
	Notification string
	Dictionary   string
	ServiceKey   string
}

// Load загружает конфигурацию из переменных окружения
func Load() *Config {
	// Загружаем .env файл если существует
	godotenv.Load()

	return &Config{
		Server: ServerConfig{
			Host:         getEnv("HOST", "0.0.0.0"),
			Port:         getEnv("PORT", "8085"),
			ReadTimeout:  getDurationEnv("READ_TIMEOUT", 30*time.Second),
			WriteTimeout: getDurationEnv("WRITE_TIMEOUT", 30*time.Second),
			IdleTimeout:  getDurationEnv("IDLE_TIMEOUT", 120*time.Second),
		},
		Redis: RedisConfig{
			Addr:     getEnvRequired("REDIS_ADDR"),
			Password: getEnv("REDIS_PASSWORD", ""),
			DB:       getIntEnv("REDIS_DB", 1),
		},
		Telegram: TelegramConfig{
			BotToken:      getEnv("TELEGRAM_BOT_TOKEN", "dummy-token-for-local-dev"), // Опционально для локальной разработки
			BotUsername:   getEnv("TELEGRAM_BOT_USERNAME", ""),                       // Опционально, можно получить через getMe
			WebhookURL:    getEnv("TELEGRAM_WEBHOOK_URL", ""),
			WebhookSecret: getEnv("TELEGRAM_WEBHOOK_SECRET", ""),
			// Оба контура задают WEBAPP_URL манифестом, поэтому дефолт в проде не
			// участвует — он отвечает только за запуск без ConfigMap. Продукт живёт
			// на апексе markerloop.com (SDL-607): /webapp — продуктовый путь, у
			// лендинга явный список путей, и webapp в него не входит. Прежний хост
			// админки с 24 Sep 2026 отвечает только 301 на апекс.
			WebAppURL: getEnv("WEBAPP_URL", "https://markerloop.com/webapp"),
		},
		Services: ServicesConfig{
			Gateway:      getEnvRequired("GATEWAY_URL"),
			Auth:         getEnvRequired("AUTH_SERVICE_URL"),
			User:         getEnvRequired("USER_SERVICE_URL"),
			Notification: getEnvRequired("NOTIFICATION_SERVICE_URL"),
			Dictionary:   getEnvRequired("DICTIONARY_SERVICE_URL"),
			ServiceKey:   getEnvRequired("SERVICE_KEY"),
		},
		OAuth: OAuthConfig{
			TokenTTL: getDurationEnv("OAUTH_TOKEN_TTL", 10*time.Minute),
		},
		HTTP: HTTPConfig{
			ClientTimeout: getDurationEnv("HTTP_CLIENT_TIMEOUT", 10*time.Second),
		},
		Notifications: NotificationsConfig{
			IdempotencyTTL: getDurationEnv("NOTIFY_IDEMPOTENCY_TTL", time.Hour),
		},
	}
}

// getEnv получает переменную окружения или возвращает значение по умолчанию
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getIntEnv получает переменную окружения как int или возвращает значение по умолчанию
func getIntEnv(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

// getDurationEnv получает переменную окружения как time.Duration или возвращает значение по умолчанию
func getDurationEnv(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if duration, err := time.ParseDuration(value); err == nil {
			return duration
		}
	}
	return defaultValue
}

// getEnvRequired получает обязательную переменную окружения или паникует
func getEnvRequired(key string) string {
	value := os.Getenv(key)
	if value == "" {
		panic(fmt.Sprintf("обязательная переменная окружения %s не установлена", key))
	}
	return value
}
