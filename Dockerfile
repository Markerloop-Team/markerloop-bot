# Используем официальный образ Go
FROM golang:1.23-alpine AS builder

# Принимаем токены как build arguments
ARG DEPLOY_TOKEN_USERNAME
ARG DEPLOY_TOKEN_PASSWORD

# Устанавливаем git и ca-certificates
RUN apk add --no-cache git ca-certificates

# Устанавливаем рабочую директорию
WORKDIR /app

# Настройка переменных окружения для приватных Go модулей
# GONOSUMDB критически важен - отключает проверку через публичную базу данных checksums
ENV GOPRIVATE=gitlab.com/markerloop/*
ENV GONOPROXY=gitlab.com/markerloop/*
ENV GONOSUMDB=gitlab.com/markerloop/*

# Настройка git credentials для доступа к приватному go-sdk
RUN git config --global url."https://${DEPLOY_TOKEN_USERNAME}:${DEPLOY_TOKEN_PASSWORD}@gitlab.com/".insteadOf "https://gitlab.com/"

# Копируем go.mod и go.sum
COPY go.mod go.sum ./

# Загружаем зависимости
RUN go mod download

# Копируем исходный код
COPY . .

# Обновляем go.sum с учетом всех импортов из исходного кода
RUN go mod tidy

# Собираем приложение
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o main ./cmd/server/main.go

# Финальный образ
FROM alpine:latest

# Устанавливаем ca-certificates для HTTPS запросов
RUN apk --no-cache add ca-certificates

WORKDIR /root/

# Копируем бинарный файл
COPY --from=builder /app/main .

# Открываем порт
EXPOSE 8085

# Запускаем приложение
CMD ["./main"]
