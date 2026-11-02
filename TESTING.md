# 🧪 Руководство по тестированию Telegram Bot Service

## 📋 Что нужно для тестирования

### 1. Обязательные данные:
- ✅ **TELEGRAM_BOT_TOKEN** - токен вашего бота (у вас уже есть)
- ✅ **TELEGRAM_BOT_USERNAME** - username бота (без @, например: `MarkerloopBot`)

### 2. Дополнительные настройки (опционально):
- `TELEGRAM_WEBHOOK_URL` - для production (пока можно оставить пустым)
- `SERVICE_KEY` - ключ для внутренних вызовов между сервисами
- `JWT_SECRET_KEY` - секрет для JWT токенов

### 3. Инфраструктура:
- ✅ PostgreSQL (запущен через docker-compose)
- ✅ Redis (запущен через docker-compose)
- ✅ Все микросервисы запущены (через `./scripts/services.sh start`)

---

## 🚀 Шаг 1: Настройка окружения

### 1.1. Создайте/обновите `.env` файл

```bash
cd telegram-bot-service
cp env.example .env
```

### 1.2. Заполните обязательные переменные в `.env`:

```env
# Telegram Bot
TELEGRAM_BOT_TOKEN=ваш_токен_от_BotFather
TELEGRAM_BOT_USERNAME=ваш_username_бота_без_собачки

# Service Key (можно использовать любой уникальный ключ)
SERVICE_KEY=test-service-key-12345

# JWT Secret (можно использовать любой ключ)
JWT_SECRET_KEY=test-jwt-secret-12345
```

### 1.3. Перезапустите сервис

```bash
cd ../dev-infra
./scripts/services.sh stop
./scripts/services.sh start
```

---

## 🧪 Шаг 2: Проверка работы бота

### 2.1. Проверьте health endpoint

```bash
curl http://localhost:8085/health
```

**Ожидаемый ответ:**
```json
{
  "status": "ok",
  "service": "telegram-bot-service"
}
```

### 2.2. Проверьте, что бот запущен

Откройте Telegram и найдите вашего бота по username. Отправьте ему команду `/start`.

**Что должно произойти:**
- Бот должен ответить приветственным сообщением
- Должна появиться кнопка "Авторизация"

---

## 📡 Шаг 3: Тестирование endpoints

### 3.1. Генерация OAuth Deep Link

**Endpoint:** `POST /api/v1/oauth/generate-token`

**Назначение:** Генерирует deep link для авторизации через Telegram

**Тестирование через curl:**
```bash
curl -X POST http://localhost:8085/api/v1/oauth/generate-token \
  -H "Content-Type: application/json" \
  -d '{
    "user_id": "123e4567-e89b-12d3-a456-426614174000"
  }'
```

**Ожидаемый ответ:**
```json
{
  "success": true,
  "data": {
    "deep_link": "https://t.me/YourBotUsername?start=TOKEN_HERE",
    "message": "Deep link для авторизации через Telegram сгенерирован."
  },
  "message": "Deep link сгенерирован"
}
```

**Что делать с deep_link:**
1. Скопируйте `deep_link` из ответа
2. Откройте его в браузере или отправьте себе в Telegram
3. Telegram откроет бота и автоматически отправит `/start TOKEN`
4. Бот должен привязать аккаунт

---

### 3.2. Отправка уведомления пользователю

**Endpoint:** `POST /api/v1/notifications/send`

**Назначение:** Отправляет уведомление конкретному пользователю

**Важно:** Пользователь должен быть привязан к Telegram аккаунту через OAuth flow.

**Тестирование через curl:**
```bash
curl -X POST http://localhost:8085/api/v1/notifications/send \
  -H "Content-Type: application/json" \
  -d '{
    "user_id": "123e4567-e89b-12d3-a456-426614174000",
    "title": "Тестовое уведомление",
    "message": "Это тестовое сообщение для проверки работы бота"
  }'
```

**Ожидаемый ответ:**
```json
{
  "success": true,
  "data": {
    "status": "sent"
  },
  "message": "Уведомление отправлено"
}
```

**Что проверить:**
- Пользователь должен получить сообщение в Telegram
- Сообщение должно содержать title и message

---

### 3.3. Отправка массовой рассылки

**Endpoint:** `POST /api/v1/notifications/broadcast`

**Назначение:** Отправляет уведомление всем пользователям, привязанным к Telegram

**Тестирование через curl:**
```bash
curl -X POST http://localhost:8085/api/v1/notifications/broadcast \
  -H "Content-Type: application/json" \
  -d '{
    "title": "Важное объявление",
    "message": "Это массовая рассылка всем пользователям"
  }'
```

**Ожидаемый ответ:**
```json
{
  "success": true,
  "data": {
    "status": "sent"
  },
  "message": "Рассылка отправлена"
}
```

---

### 3.4. Webhook от Telegram (для тестирования)

**Endpoint:** `POST /api/v1/webhook/telegram`

**Назначение:** Принимает обновления от Telegram Bot API

**Важно:** Этот endpoint обычно вызывается автоматически Telegram, но можно протестировать вручную.

**Тестирование через curl:**
```bash
curl -X POST http://localhost:8085/api/v1/webhook/telegram \
  -H "Content-Type: application/json" \
  -d '{
    "update_id": 123456789,
    "message": {
      "message_id": 1,
      "from": {
        "id": 123456789,
        "is_bot": false,
        "first_name": "Test",
        "last_name": "User",
        "username": "testuser",
        "language_code": "ru"
      },
      "chat": {
        "id": 123456789,
        "first_name": "Test",
        "last_name": "User",
        "username": "testuser",
        "type": "private"
      },
      "date": 1234567890,
      "text": "/start"
    }
  }'
```

**Ожидаемый ответ:**
```json
{
  "success": true,
  "data": {
    "status": "ok"
  },
  "message": "Webhook обработан"
}
```

---

## 🔄 Полный сценарий тестирования OAuth flow

### Шаг 1: Получить deep link
```bash
curl -X POST http://localhost:8085/api/v1/oauth/generate-token \
  -H "Content-Type: application/json" \
  -d '{"user_id": "YOUR_USER_ID"}'
```

### Шаг 2: Скопировать `deep_link` из ответа

### Шаг 3: Открыть deep link
- В браузере: просто откройте ссылку
- Или отправьте себе в Telegram и нажмите на неё

### Шаг 4: В Telegram
- Откроется бот
- Автоматически отправится `/start TOKEN`
- Бот привяжет аккаунт и покажет приветствие

### Шаг 5: Отправить тестовое уведомление
```bash
curl -X POST http://localhost:8085/api/v1/notifications/send \
  -H "Content-Type: application/json" \
  -d '{
    "user_id": "YOUR_USER_ID",
    "title": "Тест",
    "message": "Проверка работы бота"
  }'
```

### Шаг 6: Проверить Telegram
- Должно прийти сообщение с title и message

---

## 🛠️ Тестирование через Bruno

В проекте есть готовые запросы для Bruno в папке `bruno/telegram-bot-service/`:

1. Откройте Bruno
2. Импортируйте коллекцию из `bruno/telegram-bot-service/`
3. Настройте переменную `telegram-bot-service` = `http://localhost:8085`
4. Запустите запросы

---

## 🐛 Отладка

### Проверка логов
```bash
# Логи сервиса
tail -f telegram-bot-service/tmp/main.log

# Или через Air
tail -f telegram-bot-service/tmp/build-errors.log
```

### Проверка Redis
```bash
# Подключиться к Redis
docker exec -it markerloop-redis redis-cli

# Проверить OAuth токены
KEYS oauth_token:*

# Проверить токен
GET oauth_token:YOUR_TOKEN
```

### Проверка подключения к внешним сервисам
```bash
# Dictionary service
curl http://localhost:8084/api/health

# User service
curl http://localhost:8082/health
```

---

## ✅ Чеклист перед тестированием

- [ ] Telegram бот создан через @BotFather
- [ ] Токен бота добавлен в `.env`
- [ ] Username бота добавлен в `.env`
- [ ] Все сервисы запущены (`./scripts/services.sh start`)
- [ ] Redis запущен (`docker compose ps`)
- [ ] PostgreSQL запущен (`docker compose ps`)
- [ ] Health check работает (`curl http://localhost:8085/health`)
- [ ] Бот отвечает на `/start` в Telegram

---

## 📝 Примечания

1. **TELEGRAM_BOT_USERNAME** можно не указывать - бот автоматически получит его через `getMe` API
2. **Webhook** не обязателен для локального тестирования - можно использовать polling (но это не реализовано)
3. **SERVICE_KEY** и **JWT_SECRET_KEY** можно использовать любые для локальной разработки
4. Для production нужно настроить реальные webhook URL и секреты

---

## 🆘 Проблемы и решения

### Бот не отвечает на `/start`
- Проверьте, что токен правильный
- Проверьте логи: `tail -f telegram-bot-service/tmp/main.log`
- Убедитесь, что сервис запущен: `curl http://localhost:8085/health`

### Ошибка "username бота не установлен"
- Добавьте `TELEGRAM_BOT_USERNAME` в `.env`
- Или проверьте, что бот может получить username через `getMe`

### OAuth токен не работает
- Проверьте, что токен не истек (TTL 10 минут)
- Проверьте Redis: `docker exec -it markerloop-redis redis-cli KEYS oauth_token:*`
- Убедитесь, что user_id существует в user-service

### Уведомления не приходят
- Проверьте, что аккаунт привязан через OAuth flow
- Проверьте логи на ошибки
- Убедитесь, что external account существует в user-service

