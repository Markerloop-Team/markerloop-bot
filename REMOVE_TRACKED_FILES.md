# Удаление отслеживаемых файлов из Git

## Проблема
Файлы, которые должны быть в `.gitignore`, уже были закоммичены в Git и остались в истории.

## Решение

### 1. Удалить файлы из Git индекса (но оставить локально)

```bash
# Удалить .env файлы
git rm --cached .env .env.local .env.production .env.staging .env.* 2>/dev/null || true

# Удалить бинарные файлы
git rm --cached server main telegram-bot-service 2>/dev/null || true

# Удалить логи
git rm --cached *.log build-errors.log 2>/dev/null || true

# Удалить временные файлы
git rm --cached -r tmp/ temp/ 2>/dev/null || true

# Удалить build артефакты
git rm --cached -r dist/ build/ 2>/dev/null || true
```

### 2. Или использовать автоматический скрипт

```bash
# Создать скрипт
cat > remove_tracked.sh << 'EOF'
#!/bin/bash

# Файлы для удаления из Git индекса
FILES_TO_REMOVE=(
    ".env"
    ".env.local"
    ".env.production"
    ".env.staging"
    "server"
    "main"
    "telegram-bot-service"
    "*.log"
    "build-errors.log"
)

DIRS_TO_REMOVE=(
    "tmp"
    "temp"
    "dist"
    "build"
)

echo "Удаление файлов из Git индекса..."

# Удаляем файлы
for file in "${FILES_TO_REMOVE[@]}"; do
    if git ls-files --error-unmatch "$file" &>/dev/null; then
        echo "Удаляю: $file"
        git rm --cached "$file" 2>/dev/null || true
    fi
done

# Удаляем директории
for dir in "${DIRS_TO_REMOVE[@]}"; do
    if git ls-files --error-unmatch "$dir" &>/dev/null; then
        echo "Удаляю директорию: $dir/"
        git rm --cached -r "$dir" 2>/dev/null || true
    fi
done

echo "Готово! Теперь закоммитьте изменения:"
echo "git add .gitignore"
echo "git commit -m 'Add .gitignore and remove tracked files'"
EOF

chmod +x remove_tracked.sh
./remove_tracked.sh
```

### 3. Проверить перед коммитом

```bash
# Посмотреть какие файлы будут удалены
git status

# Убедиться что .gitignore добавлен
git add .gitignore

# Закоммитить
git commit -m "Add .gitignore and remove tracked files from Git history"
```

### 4. Для удаления из всей истории (если нужно)

Если нужно полностью удалить файлы из истории Git (например, если они содержат секреты):

```bash
# Использовать git filter-branch или BFG Repo-Cleaner
# ВНИМАНИЕ: Это переписывает историю!

# Вариант 1: git filter-branch
git filter-branch --force --index-filter \
  "git rm --cached --ignore-unmatch .env .env.local" \
  --prune-empty --tag-name-filter cat -- --all

# Вариант 2: BFG Repo-Cleaner (рекомендуется)
# https://rtyley.github.io/bfg-repo-cleaner/
bfg --delete-files .env
git reflog expire --expire=now --all
git gc --prune=now --aggressive
```

## ⚠️ Важные замечания

1. **`git rm --cached`** удаляет файлы только из Git индекса, но **не удаляет их локально**
2. После удаления файлы останутся в истории Git - если они содержат секреты, нужно использовать `git filter-branch` или BFG
3. **Обязательно сделайте backup** перед переписыванием истории!
4. Если файлы содержат секреты, **обязательно смените их** после удаления из Git!

