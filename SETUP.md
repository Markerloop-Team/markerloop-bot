# Настройка Telegram Bot Service

## 🚀 Быстрый старт

### 1. Создание Telegram бота

1. Найдите [@BotFather](https://t.me/botfather) в Telegram
2. Отправьте команду `/newbot`
3. Введите имя бота (например: "MarkerLoop Bot")
4. Введите username бота (например: "markerloop_bot")
5. Сохраните полученный токен

### 2. Настройка переменных окружения

```bash
cp env.example .env
```

Отредактируйте `.env` файл:

```bash
# Telegram Bot
TELEGRAM_BOT_TOKEN=1234567890:ABCdefGHIjklMNOpqrsTUVwxyz
TELEGRAM_WEBHOOK_URL=https://your-domain.com/api/v1/webhook/telegram

# База данных
DB_HOST=localhost
DB_PORT=5432
DB_USER=postgres
DB_PASSWORD=your_password
DB_NAME=telegram_bot

# Redis
REDIS_ADDR=localhost:6379
REDIS_PASSWORD=
REDIS_DB=1

# Внешние сервисы
GATEWAY_URL=http://localhost:8080
AUTH_SERVICE_URL=http://localhost:8081
USER_SERVICE_URL=http://localhost:8082
NOTIFICATION_SERVICE_URL=http://localhost:8083
```

### 3. Запуск сервиса

```bash
# Установка зависимостей
go mod tidy

# Запуск
go run main.go

# Или с hot reload (Air)
air
```

### 4. Настройка webhook (опционально)

```bash
# Установка webhook
curl -X POST "https://api.telegram.org/bot<YOUR_BOT_TOKEN>/setWebhook" \
     -H "Content-Type: application/json" \
     -d '{"url": "https://your-domain.com/api/v1/webhook/telegram"}'

# Проверка webhook
curl "https://api.telegram.org/bot<YOUR_BOT_TOKEN>/getWebhookInfo"
```

## 🔗 Интеграция с системой

### Привязка аккаунта пользователя

1. **В веб-интерфейсе системы:**
   ```bash
   # Генерация кода авторизации
   curl -X POST "http://localhost:8085/api/v1/telegram/auth-code/USER_ID"
   ```

2. **В Telegram боте:**
   - Отправьте команду `/link`
   - Введите полученный 6-значный код
   - Аккаунт будет привязан

### Отправка уведомлений

```bash
# Уведомление конкретному пользователю
curl -X POST "http://localhost:8085/api/v1/notifications/send" \
     -H "Content-Type: application/json" \
     -d '{
       "user_id": "USER_ID",
       "title": "Новое уведомление",
       "message": "Текст уведомления"
     }'

# Рассылка всем пользователям
curl -X POST "http://localhost:8085/api/v1/notifications/broadcast" \
     -H "Content-Type: application/json" \
     -d '{
       "title": "Системное уведомление",
       "message": "Планируется техническое обслуживание"
     }'
```

### Тестирование API через бота

```bash
# Тест API
curl -X POST "http://localhost:8085/api/v1/api-test/test" \
     -H "Content-Type: application/json" \
     -d '{
       "user_id": "USER_ID",
       "method": "GET",
       "url": "http://localhost:8080/api/v1/users",
       "headers": "{\"Authorization\": \"Bearer TOKEN\"}"
     }'
```

## 🔧 Интеграция с CI/CD

### GitLab CI

Добавьте в ваш `.gitlab-ci.yml`:

```yaml
variables:
  TELEGRAM_BOT_URL: "http://telegram-bot-service:8085"

notify_deploy_success:
  stage: notify
  script:
    - |
      curl -X POST "$TELEGRAM_BOT_URL/api/v1/notifications/broadcast" \
        -H "Content-Type: application/json" \
        -d '{
          "title": "✅ Деплой успешен",
          "message": "Сервис: $CI_PROJECT_NAME\nВерсия: $CI_COMMIT_SHORT_SHA"
        }'
  when: on_success
```

### GitHub Actions

```yaml
- name: Notify Telegram
  if: always()
  run: |
    curl -X POST "${{ secrets.TELEGRAM_BOT_URL }}/api/v1/notifications/broadcast" \
      -H "Content-Type: application/json" \
      -d '{
        "title": "🚀 GitHub Action завершена",
        "message": "Workflow: ${{ github.workflow }}\nСтатус: ${{ job.status }}"
      }'
```

## 📊 Мониторинг и логи

### Health Check

```bash
curl http://localhost:8085/health
```

### Логи

Сервис логирует все операции. Для просмотра логов:

```bash
# Docker
docker logs telegram-bot-service

# Локально
tail -f logs/telegram-bot-service.log
```

## 🛠️ Разработка

### Структура проекта

```
telegram-bot-service/
├── cmd/server/          # Точка входа
├── internal/
│   ├── config/          # Конфигурация
│   ├── handlers/        # HTTP обработчики
│   ├── models/          # Модели данных
│   ├── routes/          # Маршрутизация
│   └── services/        # Бизнес-логика
├── pkg/
│   ├── telegram/        # Telegram Bot API
│   └── webhook/         # Webhook уведомления
├── examples/            # Примеры интеграции
└── bruno/              # Bruno запросы для тестирования
```

### Добавление новых команд бота

1. Добавьте обработку в `internal/handlers/telegram_handler.go`
2. Обновите `handleMessage` функцию
3. Добавьте соответствующий метод в `TelegramService`

### Добавление новых типов уведомлений

1. Расширьте `WebhookNotification` в `pkg/webhook/webhook.go`
2. Добавьте новые методы в `WebhookService`
3. Обновите обработчики уведомлений

## 🔒 Безопасность

- Все webhook запросы должны проверяться по секретному ключу
- Коды авторизации имеют ограниченное время жизни (10 минут)
- Все API запросы логируются для аудита
- Используйте HTTPS для production webhook

## 📈 Масштабирование

- Сервис поддерживает горизонтальное масштабирование
- Используйте Redis для синхронизации между инстансами
- Настройте load balancer для webhook endpoint
- Мониторьте производительность через метрики

## 🐛 Отладка

### Частые проблемы

1. **Бот не отвечает:**
   - Проверьте токен бота
   - Убедитесь, что webhook настроен правильно
   - Проверьте логи сервиса

2. **Ошибки базы данных:**
   - Проверьте подключение к PostgreSQL
   - Убедитесь, что миграции выполнены
   - Проверьте права доступа

3. **Ошибки Redis:**
   - Проверьте подключение к Redis
   - Убедитесь, что Redis запущен
   - Проверьте конфигурацию

### Логи отладки

```bash
# Включить debug логи
export LOG_LEVEL=debug
go run main.go
```
