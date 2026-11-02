# Руководство по интеграции Telegram Bot Service

## 🎯 Обзор

Telegram Bot Service - это микросервис для интеграции Telegram бота с системой MarkerLoop. Он предоставляет:

- **Авторизацию через Telegram** - привязка Telegram аккаунта к пользователю системы
- **Уведомления** - отправка уведомлений о деплоях, ошибках и событиях
- **API тестирование** - удобный интерфейс для тестирования API через бота
- **Webhook поддержку** - получение обновлений от Telegram

## 🏗️ Архитектура интеграции

```
┌─────────────────┐    ┌──────────────────┐    ┌─────────────────┐
│   Telegram      │    │  Telegram Bot    │    │   Gateway       │
│   Bot API       │◄───┤  Service         │◄───┤   Service       │
└─────────────────┘    └──────────────────┘    └─────────────────┘
                                │                        │
                                ▼                        ▼
                       ┌──────────────────┐    ┌─────────────────┐
                       │   PostgreSQL     │    │  Microservices  │
                       │   + Redis        │    │  (Auth, User,   │
                       └──────────────────┘    │  Notification)  │
                                               └─────────────────┘
```

## 🚀 Быстрый старт

### 1. Создание Telegram бота

```bash
# 1. Найдите @BotFather в Telegram
# 2. Отправьте /newbot
# 3. Введите имя и username бота
# 4. Сохраните токен
```

### 2. Настройка сервиса

```bash
# Клонирование и настройка
cd telegram-bot-service
cp env.example .env

# Редактирование конфигурации
nano .env
```

### 3. Запуск

```bash
# Установка зависимостей
go mod tidy

# Запуск сервиса
go run main.go
```

## 🔗 Интеграция с существующими сервисами

### 1. Добавление в Gateway

Обновите конфигурацию Gateway для маршрутизации запросов к Telegram Bot Service:

```go
// gateway-service/internal/config/config.go
services := map[string]string{
    "auth":         cfg.Services.Auth.URL,
    "user":         cfg.Services.User.URL,
    "notification": cfg.Services.Notification.URL,
    "dictionary":   cfg.Services.Dictionary.URL,
    "telegram":     "http://localhost:8085", // Добавить эту строку
}
```

### 2. Обновление Bruno коллекции

Добавьте telegram-bot-service в environments:

```bru
vars {
  gateway-service: http://localhost:8080/api
  baseUrl: {{gateway-service}}/v1
  telegram-bot-service: http://localhost:8085
  // ... другие сервисы
}
```

### 3. Интеграция с Auth Service

Для привязки Telegram аккаунта к пользователю:

```go
// В auth-service добавьте endpoint для генерации кода
func (h *AuthHandler) GenerateTelegramAuthCode(c *gin.Context) {
    userID := c.Param("user_id")
    
    // Генерируем код через Telegram Bot Service
    resp, err := http.Post(
        "http://localhost:8085/api/v1/telegram/auth-code/"+userID,
        "application/json",
        nil,
    )
    
    // Обработка ответа...
}
```

## 📱 Использование бота

### Команды бота

- `/start` - Начать работу с ботом
- `/help` - Показать справку
- `/link` - Привязать аккаунт к системе
- `/test` - Тестировать API
- `/history` - История API запросов

### Процесс привязки аккаунта

1. **Пользователь в веб-интерфейсе:**
   - Переходит в настройки профиля
   - Нажимает "Привязать Telegram"
   - Получает 6-значный код

2. **Пользователь в Telegram:**
   - Отправляет `/link` боту
   - Вводит полученный код
   - Аккаунт привязывается

### Тестирование API

1. **Через кнопки в боте:**
   - Нажмите "🧪 Тест API"
   - Выберите готовый тест или создайте свой

2. **Через команды:**
   - Отправьте `/test`
   - Следуйте инструкциям

## 🔔 Система уведомлений

### Типы уведомлений

1. **Деплои:**
   ```bash
   curl -X POST "http://localhost:8085/api/v1/notifications/broadcast" \
        -H "Content-Type: application/json" \
        -d '{
          "title": "🚀 Деплой завершен",
          "message": "Сервис: auth-service\nВерсия: v1.2.3\nСтатус: success"
        }'
   ```

2. **Ошибки:**
   ```bash
   curl -X POST "http://localhost:8085/api/v1/notifications/broadcast" \
        -H "Content-Type: application/json" \
        -d '{
          "title": "❌ Ошибка в системе",
          "message": "Сервис: user-service\nОшибка: Database connection failed"
        }'
   ```

3. **Системные:**
   ```bash
   curl -X POST "http://localhost:8085/api/v1/notifications/broadcast" \
        -H "Content-Type: application/json" \
        -d '{
          "title": "🔧 Техническое обслуживание",
          "message": "Планируется перезагрузка серверов с 02:00 до 04:00 МСК"
        }'
   ```

### Интеграция с CI/CD

#### GitLab CI

```yaml
# .gitlab-ci.yml
notify_telegram:
  stage: notify
  script:
    - |
      curl -X POST "$TELEGRAM_BOT_URL/api/v1/notifications/broadcast" \
        -H "Content-Type: application/json" \
        -d '{
          "title": "🚀 Деплой $CI_PROJECT_NAME",
          "message": "Статус: $CI_JOB_STATUS\nВерсия: $CI_COMMIT_SHORT_SHA"
        }'
  when: always
```

#### GitHub Actions

```yaml
# .github/workflows/deploy.yml
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

## 🧪 Тестирование

### Bruno запросы

Используйте готовые Bruno запросы для тестирования:

1. **Health Check** - проверка состояния сервиса
2. **Generate Auth Code** - генерация кода авторизации
3. **Link Telegram Account** - привязка аккаунта
4. **Send Notification** - отправка уведомления
5. **Test API** - тестирование API
6. **Webhook Test** - тестирование webhook

### Локальное тестирование

```bash
# Запуск всех сервисов
cd dev-infra
make start-all

# Тестирование Telegram Bot Service
cd bruno
# Выполните запросы из папки telegram-bot-service
```

## 📊 Мониторинг

### Health Check

```bash
curl http://localhost:8085/health
```

### Логи

```bash
# Docker
docker logs telegram-bot-service

# Локально
tail -f logs/telegram-bot-service.log
```

### Метрики

- Количество привязанных аккаунтов
- Количество отправленных уведомлений
- Количество API запросов через бота
- Статус webhook

## 🔒 Безопасность

### Рекомендации

1. **Webhook:**
   - Используйте HTTPS для production
   - Проверяйте подпись webhook
   - Ограничьте доступ по IP

2. **Коды авторизации:**
   - Время жизни: 10 минут
   - Одноразовое использование
   - Хранение в Redis

3. **API:**
   - Логирование всех запросов
   - Rate limiting
   - Валидация входных данных

## 🚀 Развертывание

### Docker

```bash
# Сборка образа
docker build -t telegram-bot-service .

# Запуск контейнера
docker run -d \
  --name telegram-bot-service \
  -p 8085:8085 \
  --env-file .env \
  telegram-bot-service
```

### Kubernetes

```yaml
# k8s/telegram-bot-service.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: telegram-bot-service
spec:
  replicas: 2
  selector:
    matchLabels:
      app: telegram-bot-service
  template:
    metadata:
      labels:
        app: telegram-bot-service
    spec:
      containers:
      - name: telegram-bot-service
        image: telegram-bot-service:latest
        ports:
        - containerPort: 8085
        env:
        - name: TELEGRAM_BOT_TOKEN
          valueFrom:
            secretKeyRef:
              name: telegram-secrets
              key: bot-token
```

## 🛠️ Разработка

### Добавление новых команд

1. Обновите `handleMessage` в `telegram_handler.go`
2. Добавьте соответствующий метод в `TelegramService`
3. Обновите документацию

### Добавление новых типов уведомлений

1. Расширьте `WebhookNotification` в `webhook.go`
2. Добавьте новые методы в `WebhookService`
3. Обновите примеры интеграции

### Расширение API тестирования

1. Добавьте новые шаблоны в `TelegramService`
2. Обновите клавиатуру бота
3. Добавьте Bruno запросы

## 📚 Дополнительные ресурсы

- [Telegram Bot API](https://core.telegram.org/bots/api)
- [Go SDK документация](../go-sdk/README.md)
- [Bruno документация](https://usebruno.com/docs)
- [Примеры интеграции](./examples/)

## 🤝 Поддержка

При возникновении проблем:

1. Проверьте логи сервиса
2. Убедитесь в правильности конфигурации
3. Проверьте подключение к внешним сервисам
4. Обратитесь к команде разработки
