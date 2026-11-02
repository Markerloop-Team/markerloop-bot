package services

import (
	"context"
	"errors"

	"telegram-bot-service/pkg/auth_client"
	"telegram-bot-service/pkg/i18n"
	"telegram-bot-service/pkg/telegram"
	"telegram-bot-service/pkg/user_client"
)

// SDL-742: пользователь запросил удаление аккаунта, и каскад ещё не доведён.
// Источник правды — user-service: на поиск такого пользователя он отвечает 423
// account_deletion_pending, auth-service на выдачу токенов — тоже 423. Бот на
// это пишет пользователю понятный отказ и останавливается: не заводит нового
// пользователя (иначе /start или вход на сайт через бота вернули бы человека в
// пустой аккаунт, пока старый удаляется) и не выдаёт токены.

// deletionPendingKey — ключ перевода в dictionary-service. Пока его нет в
// сидах, Localizer вернёт сам ключ — тогда берётся текст ниже.
const deletionPendingKey = "telegram.bot.auth.account_deletion_pending"

var deletionPendingFallback = map[i18n.Locale]string{
	i18n.LocaleRU: "Этот аккаунт удаляется по вашему запросу — войти в него нельзя. Когда удаление завершится, можно будет зарегистрироваться заново.",
	i18n.LocaleEN: "This account is being deleted at your request, so you can't sign in to it. Once the deletion is complete, you can sign up again.",
}

// isDeletionPending — отказ user-service или auth-service по SDL-742.
func isDeletionPending(err error) bool {
	return errors.Is(err, user_client.ErrAccountDeletionPending) || errors.Is(err, auth_client.ErrAccountDeletionPending)
}

// sendDeletionPending сообщает пользователю, что аккаунт в процессе удаления.
func (s *TelegramService) sendDeletionPending(ctx context.Context, telegramID int64, locale i18n.Locale) error {
	s.logger.WithContext(ctx).Infof("Вход закрыт: аккаунт в процессе удаления (SDL-742), tg_id=%d", telegramID)
	msg := s.localizer.Get(ctx, deletionPendingKey)
	if msg == "" || msg == deletionPendingKey {
		msg = deletionPendingFallback[locale]
		if msg == "" {
			msg = deletionPendingFallback[i18n.LocaleRU]
		}
	}
	return s.bot.SendMessage(telegramID, msg, &telegram.MessageOptions{ParseMode: "HTML"})
}
