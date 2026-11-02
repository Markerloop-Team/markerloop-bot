# 🔧 Устранение проблем с генерацией OAuth токена

## Проблема: Endpoint `/api/v1/oauth/generate-token` не работает

### Симптомы:
- Ошибка 500 Internal Server Error
- Сообщение: "Ошибка генерации deep link"
- В логах: ошибка при вызове user-service

### Причина:
Endpoint `/api/v1/internal/external-accounts/generate-token` в user-service возвращает 404.

### Решение 1: Проверка SERVICE_KEY

1. **Убедитесь, что SERVICE_KEY одинаковый в обоих сервисах:**

```bash
# В user-service/.env
SERVICE_KEY=your_service_key_here

# В telegram-bot-service/.env
SERVICE_KEY=your_service_key_here
```

2. **Перезапустите оба сервиса:**

```bash
cd dev-infra
./scripts/services.sh stop
./scripts/services.sh start
```

### Решение 2: Проверка endpoint в user-service

Проверьте, что endpoint существует:

```bash
curl -X POST http://localhost:8082/api/v1/internal/external-accounts/generate-token \
  -H "Content-Type: application/json" \
  -H "X-Service-Key: your_service_key_here" \
  -d '{"user_id": "test-user", "provider": "telegram"}'
```

Если возвращает 404, значит endpoint не зарегистрирован или сервис не перезапущен.

### Решение 3: Временный обходной путь

Если user-service endpoint не работает, telegram-bot-service использует fallback:
- Генерирует токен локально
- Сохраняет в Redis
- Токен все равно работает для OAuth flow

### Проверка работы:

```bash
# 1. Генерация deep link
curl -X POST http://localhost:8085/api/v1/oauth/generate-token \
  -H "Content-Type: application/json" \
  -d '{"user_id": "YOUR_USER_ID"}'

# 2. Должен вернуть deep_link
{
  "success": true,
  "data": {
    "deep_link": "https://t.me/YourBot?start=TOKEN"
  }
}
```

### Логи для отладки:

```bash
# Логи telegram-bot-service
tail -f telegram-bot-service/tmp/main.log | grep -i "error\|token\|generate"

# Логи user-service
tail -f user-service/tmp/main.log | grep -i "generate\|token"
```

