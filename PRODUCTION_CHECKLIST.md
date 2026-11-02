# 🔍 Чеклист проверки Telegram Bot в Production

## 📋 Текущая проблема
После отправки команды `/start` бот не отвечает.

## ✅ Чеклист проверки

### 1. Проверка конфигурации Kubernetes

#### 1.1. Проверить Secret с webhook URL
```bash
kubectl get secret telegram-bot-service-secrets -n <namespace> -o yaml
```

**Проверить:**
- ✅ `TELEGRAM_WEBHOOK_URL` должен быть: `https://markerloop.com/api/v1/telegram/webhook`
- ✅ `TELEGRAM_BOT_TOKEN` должен быть реальным токеном бота
- ✅ `TELEGRAM_WEBHOOK_SECRET` должен быть сгенерирован (минимум 32 символа)
- ❌ НЕ должно быть placeholder значений типа `CHANGE-ME` или `your-domain.com`

#### 1.2. Проверить ConfigMap gateway-service
```bash
kubectl get configmap gateway-service-config -n <namespace> -o yaml
```

**Проблема:** В ConfigMap нет `TELEGRAM_BOT_SERVICE_URL` и `DICTIONARY_SERVICE_URL`

**Нужно добавить в ConfigMap:**
```yaml
TELEGRAM_BOT_SERVICE_URL: "http://telegram-bot-service:80"
DICTIONARY_SERVICE_URL: "http://dictionary-service:80"
```

#### 1.3. Проверить статус подов
```bash
kubectl get pods -n <namespace> | grep -E "telegram-bot|gateway"
```

**Проверить:**
- ✅ Оба пода должны быть в статусе `Running`
- ✅ Не должно быть `CrashLoopBackOff` или `Error`

### 2. Проверка webhook в Telegram

#### 2.1. Проверить установлен ли webhook
```bash
curl "https://api.telegram.org/bot<BOT_TOKEN>/getWebhookInfo"
```

**Проверить:**
- ✅ `url` должен быть: `https://markerloop.com/api/v1/telegram/webhook`
- ✅ `pending_update_count` - количество ожидающих обновлений
- ✅ `last_error_date` - если есть ошибка, будет указана дата

#### 2.2. Если webhook не установлен или неправильный
```bash
curl -X POST "https://api.telegram.org/bot<BOT_TOKEN>/setWebhook" \
  -H "Content-Type: application/json" \
  -d '{
    "url": "https://markerloop.com/api/v1/telegram/webhook",
    "secret_token": "<WEBHOOK_SECRET>"
  }'
```

### 3. Проверка доступности endpoints

#### 3.1. Проверить health check telegram-bot-service
```bash
kubectl exec -it <telegram-bot-pod> -n <namespace> -- curl http://localhost:8085/health
```

#### 3.2. Проверить health check через gateway
```bash
curl https://markerloop.com/api/v1/telegram/health
```

#### 3.3. Проверить webhook endpoint (должен вернуть 200 при POST от Telegram)
```bash
curl -X POST https://markerloop.com/api/v1/telegram/webhook \
  -H "Content-Type: application/json" \
  -H "X-Telegram-Bot-Api-Secret-Token: <WEBHOOK_SECRET>" \
  -d '{"update_id": 123, "message": {"message_id": 1, "chat": {"id": 123}, "text": "/start"}}'
```

### 4. Проверка логов

#### 4.1. Логи telegram-bot-service
```bash
kubectl logs -f <telegram-bot-pod> -n <namespace> --tail=100
```

**Что искать:**
- ✅ Сообщения о запуске сервера
- ✅ Сообщения о установке webhook
- ✅ Запросы от Telegram (должны быть логи при отправке /start)
- ❌ Ошибки подключения к Redis
- ❌ Ошибки подключения к user-service или dictionary-service
- ❌ Ошибки обработки webhook

#### 4.2. Логи gateway-service
```bash
kubectl logs -f <gateway-pod> -n <namespace> --tail=100
```

**Что искать:**
- ✅ Запросы к `/api/v1/telegram/webhook`
- ✅ Проксирование на telegram-bot-service
- ❌ Ошибки проксирования (503, 502, timeout)

### 5. Проверка после отправки /start

После того как пользователь отправит `/start`, проверить:

1. **Логи telegram-bot-service:**
   ```bash
   kubectl logs -f <telegram-bot-pod> -n <namespace> --tail=50
   ```
   
   Должны быть:
   - Лог о получении webhook
   - Лог о парсинге сообщения
   - Лог о вызове `handleStartCommand`
   - Лог об отправке ответа

2. **Логи gateway-service:**
   ```bash
   kubectl logs -f <gateway-pod> -n <namespace> --tail=50
   ```
   
   Должны быть:
   - Лог о получении POST запроса на `/api/v1/telegram/webhook`
   - Лог о проксировании на telegram-bot-service

## 🔧 Известные проблемы

### Проблема 1: Отсутствует TELEGRAM_BOT_SERVICE_URL в gateway ConfigMap
**Решение:** Добавить в `dev-infra/apps/gateway-service/base/configmap.yaml`:
```yaml
TELEGRAM_BOT_SERVICE_URL: "http://telegram-bot-service:80"
DICTIONARY_SERVICE_URL: "http://dictionary-service:80"
```

### Проблема 2: Webhook URL может быть placeholder
**Решение:** Убедиться, что в Secret установлен реальный URL:
```yaml
TELEGRAM_WEBHOOK_URL: "https://markerloop.com/api/v1/telegram/webhook"
```

### Проблема 3: Webhook не установлен в Telegram
**Решение:** Установить webhook через API или убедиться, что сервис устанавливает его при старте.

## 📝 Команды для быстрой проверки

```bash
# 1. Статус подов
kubectl get pods -n <namespace> | grep -E "telegram|gateway"

# 2. Логи telegram-bot-service (последние 100 строк)
kubectl logs <telegram-bot-pod> -n <namespace> --tail=100

# 3. Проверка webhook в Telegram
curl "https://api.telegram.org/bot<BOT_TOKEN>/getWebhookInfo"

# 4. Проверка health через gateway
curl https://markerloop.com/api/v1/telegram/health

# 5. Проверка конфигурации
kubectl get configmap gateway-service-config -n <namespace> -o yaml | grep TELEGRAM
kubectl get secret telegram-bot-service-secrets -n <namespace> -o yaml | grep WEBHOOK
```

## 🎯 Следующие шаги

1. ✅ Выполнить все проверки из чеклиста
2. ✅ Отправить команду `/start` в бот
3. ✅ Собрать логи обоих сервисов
4. ✅ Найти проблему в логах
5. ✅ Исправить проблему

