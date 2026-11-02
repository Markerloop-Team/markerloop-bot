#!/bin/bash

# Скрипт для удаления файлов из истории GitLab
# ⚠️ ВНИМАНИЕ: Это перезапишет историю Git!

set -e

echo "🔍 GITLAB CLEANUP SCRIPT"
echo "========================"
echo ""

# Цвета для вывода
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Проверка наличия BFG
if ! command -v bfg &> /dev/null; then
    echo -e "${YELLOW}⚠️  BFG не установлен${NC}"
    echo "Установите: brew install bfg"
    echo "Или скачайте: https://rtyley.github.io/bfg-repo-cleaner/"
    exit 1
fi

echo "📋 Файлы для удаления:"
echo "  1. .env (критично - содержит секреты)"
echo "  2. main (бинарный файл)"
echo "  3. tmp/main (бинарный файл)"
echo "  4. tmp/build-errors.log (лог файл)"
echo ""

read -p "Продолжить? (y/N): " -n 1 -r
echo
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    echo "Отменено"
    exit 1
fi

echo ""
echo "📦 Создание backup..."
BACKUP_DIR="/tmp/telegram-bot-service-backup-$(date +%Y%m%d-%H%M%S)"
mkdir -p "$BACKUP_DIR"
git clone --mirror . "$BACKUP_DIR/backup.git"
echo -e "${GREEN}✅ Backup создан: $BACKUP_DIR${NC}"
echo ""

echo "🗑️  Удаление файлов из истории..."
echo ""

# Удаляем .env
echo "1. Удаление .env..."
bfg --delete-files .env
echo -e "${GREEN}✅ .env удален${NC}"
echo ""

# Удаляем бинарные файлы
echo "2. Удаление бинарных файлов..."
bfg --delete-files main
bfg --delete-files tmp/main
echo -e "${GREEN}✅ Бинарные файлы удалены${NC}"
echo ""

# Удаляем логи
echo "3. Удаление логов..."
bfg --delete-files "tmp/build-errors.log"
echo -e "${GREEN}✅ Логи удалены${NC}"
echo ""

echo "🧹 Очистка и оптимизация..."
git reflog expire --expire=now --all
git gc --prune=now --aggressive
echo -e "${GREEN}✅ Очистка завершена${NC}"
echo ""

echo "📝 Следующие шаги:"
echo ""
echo "1. Проверьте изменения:"
echo "   git log --oneline"
echo ""
echo "2. Отправьте изменения (⚠️ ПЕРЕЗАПИШЕТ ИСТОРИЮ!):"
echo "   git push --force --all"
echo "   git push --force --tags"
echo ""
echo "3. ⚠️ ОБЯЗАТЕЛЬНО смените все секреты:"
echo "   - TELEGRAM_BOT_TOKEN"
echo "   - JWT_SECRET_KEY"
echo "   - SERVICE_KEY"
echo "   - TELEGRAM_WEBHOOK_SECRET"
echo ""
echo "4. Обновите окружения:"
echo "   - Локальные .env файлы"
echo "   - Kubernetes Secrets"
echo "   - CI/CD переменные"
echo ""
echo -e "${GREEN}✅ Готово!${NC}"

