# Telegram Bot Service

Сервис Telegram бота для системы MarkerLoop. Обеспечивает интеграцию с Telegram для уведомлений, авторизации и тестирования API.

## 🚀 Возможности

- **Авторизация через Telegram** - привязка Telegram аккаунта к пользователю системы
- **Уведомления** - отправка уведомлений о деплоях, ошибках и событиях
- **API тестирование** - удобный интерфейс для тестирования API через бота
- **Webhook поддержка** - получение обновлений от Telegram
- **История запросов** - сохранение и просмотр истории API запросов

## 🏗️ Архитектура

```
Telegram Bot API → Webhook → Telegram Bot Service → Gateway → Microservices
                                    ↓
                               PostgreSQL + Redis
```

## 📦 Структура проекта

```
markerloop-bot/            # репозиторий markerloop-bot; go-модуль исторически называется telegram-bot-service
├── cmd/
│   └── server/          # Точка входа
├── internal/
│   ├── config/          # Конфигурация
│   ├── handlers/        # HTTP обработчики
│   ├── models/          # Модели данных
│   ├── routes/          # Маршрутизация
│   └── services/        # Бизнес-логика
├── pkg/
│   └── telegram/        # Telegram Bot API клиент
├── go.mod               # Зависимости Go
├── Dockerfile           # Docker образ
├── env.example          # Пример конфигурации
└── README.md            # Документация
```

## 🔧 Установка и запуск

### 1. Создание Telegram бота

1. Найдите [@BotFather](https://t.me/botfather) в Telegram
2. Отправьте команду /newbot
3. Следуйте инструкциям для создания бота
4. Сохраните полученный токен

### 2. Настройка webhook (опционально)

```bash
# Установка webhook
curl -X POST "https://api.telegram.org/bot<YOUR_BOT_TOKEN>/setWebhook" \
     -H "Content-Type: application/json" \
     -d '{"url": "https://your-domain.com/api/v1/webhook/telegram"}'

# Проверка webhook
curl "https://api.telegram.org/bot<YOUR_BOT_TOKEN>/getWebhookInfo"
```

### 3. Конфигурация

```bash
cp env.example .env
# Отредактируйте .env файл
```

### 4. Запуск

```bash
# Локально
go run main.go

# Или через Air (hot reload)
air

# Docker
docker build -t markerloop-bot .
docker run -p 8085:8085 --env-file .env markerloop-bot
```

## ⚙️ Конфигурация

### Основные настройки

```bash
# Сервер
PORT=8085
HOST=0.0.0.0

# Telegram Bot
TELEGRAM_BOT_TOKEN=your_bot_token_here
TELEGRAM_WEBHOOK_URL=https://your-domain.com/webhook/telegram

# База данных
DB_HOST=localhost
DB_PORT=5432
DB_USER=postgres
DB_PASSWORD=password
DB_NAME=telegram_bot

# Redis
REDIS_ADDR=localhost:6379
REDIS_PASSWORD=
REDIS_DB=1

# Внешние сервисы
GATEWAY_URL=http://localhost:8080
AUTH_SERVICE_URL=http://localhost:8081
USER_SERVICE_URL=http://localhost:8082
```

## 📡 API Endpoints

### Health Check
- `GET /health` - Проверка состояния сервиса

### Telegram Webhook
- `POST /api/v1/webhook/telegram` - Webhook для получения обновлений от Telegram

### Авторизация
- `POST /api/v1/telegram/link` - Привязка Telegram аккаунта
- `POST /api/v1/telegram/auth-code/:user_id` - Генерация кода авторизации

### Уведомления
- `POST /api/v1/notifications/send` - Отправка уведомления пользователю
- `POST /api/v1/notifications/broadcast` - Рассылка всем пользователям
- `POST /api/v1/notifications/deploy` - Рассылка подписчикам deploy-нотификаций (ArgoCD, регресс dev-infra)

Все три принимают необязательный заголовок `Idempotency-Key`: повтор той же доставки
отвечает `200 {"duplicate": true}` без сообщения (SDL-345).

#### Регресс dev-infra: красный и «снова зелёный» (SDL-715)

Эндпоинт deploy-нотификаций различает события по `event_type`:

| `event_type` | Что делает бот | Ответ |
|---|---|---|
| `regression-failed` | рассылает красный алерт как раньше и ставит в Redis отметку `notify:regression:red:<app>` (значение — `revision_url`, без TTL) | `200 {"status":"sent"}` |
| `regression-passed` | отметка есть → одно сообщение «✅ регресс снова зелёный» со ссылкой из `revision_url` и снятие отметки; отметки нет → без сообщения | `200 {"status":"sent"}` или `200 {"status":"skipped"}` |

Тело `regression-passed` (обязателен только `app`, он должен совпадать с `app` красного события):

```json
{
  "app": "full-regression",
  "status": "success",
  "event_type": "regression-passed",
  "revision_url": "<URL пайплайна>"
}
```

Почему так:
- отметка без TTL: красный регресс бывает долгим, а TTL короче него молча съел бы «снова зелёный»; ключ один на `app`, копиться нечему;
- Redis недоступен → «снова зелёный» пропускается (ответ всё равно 200): лучше промолчать, чем слать его на каждый зелёный прогон. Красный алерт при этом уходит как раньше;
- две одновременные доставки, увидевшие отметку, объявляют один раз (claim `notify:regression:recover-claim:<app>`, TTL 2 мин);
- провал рассылки «снова зелёный» → `500`, отметка остаётся, объявит следующий зелёный прогон или ретрай.

### API тестирование
- `POST /api/v1/api-test/test` - Тестирование API
- `GET /api/v1/api-test/history/:user_id` - История API запросов

## 🤖 Команды бота

- /start - Начать работу с ботом
- /help - Показать справку
- /link - Привязать аккаунт к системе
- /test - Тестировать API
- /history - История API запросов

## 🔗 Интеграция с системой

### 1. Привязка аккаунта

1. Пользователь получает код авторизации в веб-интерфейсе
2. Отправляет команду /link в бот
3. Вводит полученный код
4. Аккаунт привязывается к системе

### 2. Отправка уведомлений

```bash
# Отправка уведомления конкретному пользователю
curl -X POST "http://localhost:8085/api/v1/notifications/send" \
     -H "Content-Type: application/json" \
     -d '{
       "user_id": "user_id_here",
       "title": "Деплой завершен",
       "message": "Новая версия успешно развернута"
     }'

# Рассылка всем пользователям
curl -X POST "http://localhost:8085/api/v1/notifications/broadcast" \
     -H "Content-Type: application/json" \
     -d '{
       "title": "Техническое обслуживание",
       "message": "Система будет недоступна с 02:00 до 04:00"
     }'
```

### 3. Тестирование API

```bash
# Тестирование API через бота
curl -X POST "http://localhost:8085/api/v1/api-test/test" \
     -H "Content-Type: application/json" \
     -d '{
       "user_id": "user_id_here",
       "method": "GET",
       "url": "http://localhost:8080/api/v1/users",
       "headers": "{\"Authorization\": \"Bearer token_here\"}"
     }'
```

## 🗄️ База данных

Сервис использует PostgreSQL для хранения:
- Привязок Telegram аккаунтов к пользователям
- Уведомлений
- Истории API запросов

Redis используется для:
- Временного хранения кодов авторизации
- Кэширования

## 🔒 Безопасность

- Webhook проверяется по секретному ключу: при заданном `TELEGRAM_WEBHOOK_SECRET` запрос без верного заголовка `X-Telegram-Bot-Api-Secret-Token` получает 401; в режиме вебхука (`TELEGRAM_WEBHOOK_URL` задан, `TELEGRAM_USE_POLLING` не `true`) без секрета сервис не стартует на любом `ENV`. На старте бот сам регистрирует вебхук у Telegram вместе с `secret_token`, ручной `setWebhook` без секрета перестанет доставлять обновления до рестарта пода.
- Коды авторизации имеют ограниченное время жизни
- Все API запросы логируются
- Поддержка HTTPS для webhook

## 📊 Мониторинг

Сервис предоставляет метрики через /health endpoint и логирует все операции для мониторинга и отладки.
