# markerloop-bot

The Telegram bot of the Markerloop platform: users sign in to Markerloop through the bot, and other services deliver
notifications to Telegram through it (personal messages, broadcasts, deploy and regression alerts).

Markerloop is a shared backend (users, auth, files, collections, notifications, i18n) plus a page builder that an AI
agent drives through MCP. The bot holds no database of its own: accounts live in `user-service`, tokens come from
`auth-service`, bot texts come from `dictionary-service`, and short-lived state (sessions, login codes, dialog steps,
idempotency keys) lives in Redis.

## Responsibilities

- **Sign-in through Telegram.** The admin UI asks the bot for a one-time token and a `t.me/<bot>?start=<token>` deep
  link (`POST /api/v1/oauth/generate-token`). The user opens the link, `/start <token>` binds the Telegram account to
  the Markerloop user as an external account in `user-service` and completes the login. A first-time Telegram user is
  registered automatically.
- **Sign-in with a password in the chat.** `/auth` starts a short dialog against `auth-service`: username, then
  password; if there is no such account, the bot asks for an email and registers one.
- **Menu for signed-in users:** `/menu`, `/profile`, `/settings`, `/notifications`, `/logout`, and role-gated
  `/users` and `/stats`.
- **Account deletion in progress.** While `user-service` reports a pending deletion (HTTP 423), the bot refuses to
  sign the user in or to create a fresh account, so a half-deleted account cannot be revived through Telegram.
- **Notification delivery** for other services: to one user, to everyone, and to deploy subscribers.
- **Regression alerts with recovery.** A red regression run sends an alert and leaves a marker in Redis; the next
  green run of the same app sends a single "green again" message and clears the marker. Two concurrent deliveries
  announce the recovery once; if Redis is down the recovery message is skipped rather than sent on every green run.

## Security

- **Service-to-service key.** `/api/v1/oauth/*` and `/api/v1/notifications/*` require the `X-Service-Key` header
  (go-sdk `ServiceToServiceAuth`); without it or with a wrong one the answer is `401`, before any other work.
- **Webhook secret.** With `TELEGRAM_WEBHOOK_SECRET` set, a webhook call without the matching
  `X-Telegram-Bot-Api-Secret-Token` gets `401`. In webhook mode the service refuses to start without a secret, and on
  start it registers the webhook with Telegram together with that secret.
- **Idempotent delivery.** Notification endpoints accept an optional `Idempotency-Key`; a repeated delivery returns
  `200 {"duplicate": true}` without sending the message again, so retries from `notification-service` never double-post.
- **PII-safe logs.** Message text, tokens and passwords are not logged: commands are logged by name, tokens by
  fingerprint. Tests guard both.

## API

| Method | Path | Purpose |
|---|---|---|
| GET | `/health` | Health check |
| GET | `/swagger/*any` | Swagger UI |
| POST | `/api/v1/webhook/telegram` | Telegram updates (webhook mode) |
| POST | `/api/v1/oauth/generate-token` | One-time token and deep link for Telegram sign-in · service key |
| POST | `/api/v1/notifications/send` | Message to one user · service key |
| POST | `/api/v1/notifications/broadcast` | Message to all users · service key |
| POST | `/api/v1/notifications/deploy` | Deploy and regression events (`regression-failed`, `regression-passed`) · service key |

## Project structure

```
main.go               entry point and wiring
internal/
  config/             environment configuration
  handlers/           HTTP and Telegram update handlers
  middleware/         webhook secret, idempotency, session
  routes/             Gin routes
  services/           sign-in flows, menus, delivery, regression state
  startup/            webhook registration and start-up checks
pkg/
  telegram/           Telegram Bot API client
  auth_client/        auth-service client
  user_client/        user-service client
  dictionary_client/  bot texts from dictionary-service
  i18n/               locale selection
docs/                 generated Swagger
```

## Running locally

```bash
cp env.example .env      # fill in TELEGRAM_BOT_TOKEN, SERVICE_KEY and service URLs
go run main.go           # TELEGRAM_USE_POLLING=true avoids the need for a public webhook URL
```

Docker:

```bash
docker build -t markerloop-bot .
docker run -p 8085:8085 --env-file .env markerloop-bot
```

Configuration (see `env.example`): `PORT`, `TELEGRAM_BOT_TOKEN`, `TELEGRAM_BOT_USERNAME`, `TELEGRAM_WEBHOOK_URL`,
`TELEGRAM_WEBHOOK_SECRET`, `TELEGRAM_USE_POLLING`, `REDIS_ADDR`, `GATEWAY_URL`, `AUTH_SERVICE_URL`, `USER_SERVICE_URL`,
`NOTIFICATION_SERVICE_URL`, `DICTIONARY_SERVICE_URL`, `SERVICE_KEY`, `OAUTH_TOKEN_TTL`, `NOTIFY_IDEMPOTENCY_TTL`.

## Notes

- The Go module is still named `telegram-bot-service` for historical reasons; the repository is `markerloop-bot`.
- The service imports the shared library as `gitlab.com/markerloop/go-sdk` (private GitLab). The same library is
  published as [Markerloop-Team/go-sdk](https://github.com/Markerloop-Team/go-sdk); to build outside our CI, clone it
  and add `replace gitlab.com/markerloop/go-sdk => ../go-sdk` to `go.mod`.
- Code comments are partly in Russian: this is the production code of a Kazakhstan-based team, published as is.

Website: [markerloop.com](https://markerloop.com) · Code: [github.com/Markerloop-Team](https://github.com/Markerloop-Team)

---

Source published for review. © Markerloop. All rights reserved.
