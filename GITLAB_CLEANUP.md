# 🗑️ Очистка файлов из истории GitLab

## 🔍 Критичные файлы для удаления (содержат секреты)

### ⚠️ ОБЯЗАТЕЛЬНО удалить из истории:

1. **`.env`** - содержит реальные токены и секреты
   ```bash
   # Если в .env были реальные секреты - ОБЯЗАТЕЛЬНО удалить!
   # После удаления СМЕНИТЬ все секреты!
   ```

2. **Любые файлы с реальными токенами/паролями**
   - Если в коммитах были реальные `TELEGRAM_BOT_TOKEN`
   - Если были реальные `JWT_SECRET_KEY`, `SERVICE_KEY`
   - Если были реальные `TELEGRAM_WEBHOOK_SECRET`

## 📦 Файлы для удаления (не критично, но желательно)

### Можно удалить для экономии места:

1. **`main`** - бинарный файл (большой размер)
2. **`server`** - бинарный файл
3. **`tmp/main`** - временный бинарный файл
4. **`*.log`** - логи (обычно не содержат секретов)
5. **`tmp/build-errors.log`** - логи ошибок

## 🛠️ Способы удаления

### Вариант 1: BFG Repo-Cleaner (рекомендуется)

BFG - быстрый и безопасный инструмент для очистки Git истории.

```bash
# 1. Установить BFG (если нет)
# macOS:
brew install bfg

# или скачать:
# https://rtyley.github.io/bfg-repo-cleaner/

# 2. Клонировать репозиторий с --mirror
cd /tmp
git clone --mirror https://gitlab.com/markerloop/telegram-bot-service.git

# 3. Удалить файлы
cd telegram-bot-service.git

# Удалить .env из всей истории
bfg --delete-files .env

# Удалить бинарные файлы
bfg --delete-files main server

# Удалить логи
bfg --delete-files "*.log"

# 4. Очистить и оптимизировать
git reflog expire --expire=now --all
git gc --prune=now --aggressive

# 5. Отправить изменения (⚠️ ПЕРЕЗАПИСЫВАЕТ ИСТОРИЮ!)
git push --force
```

### Вариант 2: git filter-branch

```bash
# Удалить .env из всей истории
git filter-branch --force --index-filter \
  "git rm --cached --ignore-unmatch .env" \
  --prune-empty --tag-name-filter cat -- --all

# Удалить бинарные файлы
git filter-branch --force --index-filter \
  "git rm --cached --ignore-unmatch main server tmp/main" \
  --prune-empty --tag-name-filter cat -- --all

# Удалить логи
git filter-branch --force --index-filter \
  "git rm --cached --ignore-unmatch '*.log' tmp/build-errors.log" \
  --prune-empty --tag-name-filter cat -- --all

# Очистить
git reflog expire --expire=now --all
git gc --prune=now --aggressive

# Отправить (⚠️ ПЕРЕЗАПИСЫВАЕТ ИСТОРИЮ!)
git push --force --all
git push --force --tags
```

### Вариант 3: GitLab UI (только для последних коммитов)

Если файлы были добавлены недавно и в небольшом количестве коммитов:

1. Откройте GitLab → Repository → Files
2. Найдите файл (например, `.env`)
3. Нажмите "Delete" и создайте новый коммит

⚠️ Это не удалит файл из истории, только из текущей версии!

## ⚠️ ВАЖНЫЕ ПРЕДУПРЕЖДЕНИЯ

### Перед удалением:

1. **Сделайте backup репозитория:**
   ```bash
   git clone --mirror https://gitlab.com/markerloop/telegram-bot-service.git backup.git
   ```

2. **Уведомите команду** - удаление истории влияет на всех

3. **Смените все секреты** после удаления:
   - `TELEGRAM_BOT_TOKEN` - получить новый в @BotFather
   - `JWT_SECRET_KEY` - сгенерировать новый
   - `SERVICE_KEY` - сгенерировать новый
   - `TELEGRAM_WEBHOOK_SECRET` - сгенерировать новый (`openssl rand -hex 32`)

4. **Обновите все окружения:**
   - Локальные `.env` файлы
   - Kubernetes Secrets
   - CI/CD переменные

### После удаления:

1. **Все разработчики должны переклонировать репозиторий:**
   ```bash
   # Удалить локальный репозиторий
   rm -rf telegram-bot-service
   
   # Клонировать заново
   git clone https://gitlab.com/markerloop/telegram-bot-service.git
   ```

2. **Обновить все CI/CD пайплайны** - они могут ссылаться на старые коммиты

## 📋 Рекомендуемый план действий

### Для критичных файлов (.env с секретами):

```bash
# 1. Backup
git clone --mirror https://gitlab.com/markerloop/telegram-bot-service.git backup.git

# 2. Удалить .env
bfg --delete-files .env

# 3. Очистить
git reflog expire --expire=now --all
git gc --prune=now --aggressive

# 4. Push (с force)
git push --force

# 5. СМЕНИТЬ ВСЕ СЕКРЕТЫ!
# 6. Обновить все окружения
```

### Для некритичных файлов (бинарники, логи):

Можно удалить позже или оставить - они не содержат секретов, только занимают место.

## ✅ Чеклист безопасности

- [ ] Сделал backup репозитория
- [ ] Удалил `.env` из истории (если содержал секреты)
- [ ] Сменил `TELEGRAM_BOT_TOKEN`
- [ ] Сменил `JWT_SECRET_KEY`
- [ ] Сменил `SERVICE_KEY`
- [ ] Сменил `TELEGRAM_WEBHOOK_SECRET`
- [ ] Обновил локальные `.env` файлы
- [ ] Обновил Kubernetes Secrets
- [ ] Обновил CI/CD переменные
- [ ] Уведомил команду о необходимости переклонировать репозиторий

