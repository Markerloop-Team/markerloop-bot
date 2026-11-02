# 🧪 Локальное тестирование Telegram бота

## ⚠️ Важная проблема: Webhook vs Polling

### Проблема:
Если у бота установлен webhook, он **не будет получать обновления через polling**. Telegram отправляет обновления только через webhook, если он установлен.

### Решение для локального тестирования:

**Вариант 1: Удалить webhook и использовать polling (рекомендуется для локальной разработки)**

```bash
# Удалить webhook через Telegram API
curl -X POST "https://api.telegram.org/bot<YOUR_BOT_TOKEN>/deleteWebhook"
```

Бот автоматически запустит polling, если `TELEGRAM_WEBHOOK_URL` не указан в `.env`.

**Вариант 2: Использовать ngrok для локального webhook**

```bash
# Установить ngrok
brew install ngrok  # или скачать с ngrok.com

# Запустить туннель
ngrok http 8085

# Скопировать HTTPS URL (например: https://abc123.ngrok.io)
# Добавить в telegram-bot-service/.env:
TELEGRAM_WEBHOOK_URL=https://abc123.ngrok.io/api/v1/webhook/telegram

# Установить webhook
curl -X POST "https://api.telegram.org/bot<YOUR_BOT_TOKEN>/setWebhook?url=https://abc123.ngrok.io/api/v1/webhook/telegram"
```

---

## 🔍 Проверка работы бота

### 1. Проверьте, что webhook удален:

```bash
curl "https://api.telegram.org/bot<YOUR_BOT_TOKEN>/getWebhookInfo"
```

Должно вернуть: `"url": ""` (пустой URL означает, что webhook не установлен)

### 2. Проверьте логи:

```bash
tail -f telegram-bot-service/tmp/main.log | grep -i "polling\|обновление\|start"
```

Должны увидеть:
- `"Polling запущен"`
- `"Получено обновление"` при отправке `/start`

### 3. Отправьте `/start` в Telegram:

1. Откройте Telegram
2. Найдите вашего бота
3. Отправьте `/start`
4. Бот должен ответить

---

## 🐛 Если бот не отвечает:

### Проверка 1: Webhook удален?
```bash
curl "https://api.telegram.org/bot<YOUR_BOT_TOKEN>/getWebhookInfo"
```

### Проверка 2: Polling запущен?
Смотрите логи: `tail -f telegram-bot-service/tmp/main.log`

### Проверка 3: Токен правильный?
```bash
curl "https://api.telegram.org/bot<YOUR_BOT_TOKEN>/getMe"
```

Должен вернуть информацию о боте.

### Проверка 4: Сервис запущен?
```bash
curl http://localhost:8085/health
```

---

## 📝 Быстрая команда для удаления webhook:

```bash
# Замените YOUR_BOT_TOKEN на ваш токен
TOKEN=$(grep TELEGRAM_BOT_TOKEN telegram-bot-service/.env | cut -d= -f2)
curl -X POST "https://api.telegram.org/bot${TOKEN}/deleteWebhook"
```

После этого перезапустите сервис:
```bash
cd dev-infra
./scripts/services.sh stop
./scripts/services.sh start
```

Бот автоматически запустит polling и начнет получать обновления!

