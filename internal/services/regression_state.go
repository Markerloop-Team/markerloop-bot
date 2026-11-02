package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

// SDL-715: «✅ регресс снова зелёный» после красного.
//
// Оба события приходят в тот же POST /api/v1/notifications/deploy, что и
// ArgoCD-нотификации; различает их только event_type:
//
//   - regression-failed — красный регресс (джоб alert-regression-failed
//     dev-infra). Рендер и рассылка — как раньше; дополнительно бот ставит
//     отметку «последний результат красный».
//   - regression-passed — зелёный прогон. Есть отметка → одно сообщение
//     «снова зелёный» и снятие отметки. Нет отметки → тишина, 200.
//
// Отметку ставит сам бот по regression-failed, поэтому CI достаточно слать
// новое событие, ничего не помечая (у джобов dev-infra нет ни API GitLab,
// ни прав хранить состояние в кластере).
const (
	EventRegressionFailed = "regression-failed"
	EventRegressionPassed = "regression-passed"
)

// Ключи живут в Redis рядом с notify:idem:* (SDL-345) и скоупятся по app:
// passed снимает отметку только своего же регресса. CI шлёт оба события с
// app=full-regression.
const (
	regressionRedKeyPrefix   = "notify:regression:red:"
	regressionClaimKeyPrefix = "notify:regression:recover-claim:"
)

// Отметка red хранится БЕЗ TTL (expiration 0), осознанно:
//   - ключ один на app — утечки нет, копиться нечему;
//   - красный регресс бывает долгим (SDL-132: schemathesis был красным
//     неделями). Любой TTL короче такого окна молча съедал бы «снова
//     зелёный» ровно там, где он ценнее всего;
//   - потерять отметку (рестарт Redis без persistence, eviction) безопасно:
//     это тишина, а не спам.
const regressionRedTTL = 0

// recoverClaimTTL страхует claim от пода, убитого посреди рассылки
// (тот же расчёт, что claimTTL в middleware идемпотентности).
const recoverClaimTTL = 2 * time.Minute

// regressionWriteTimeout — бюджет на записи после рассылки: request-context
// к этому моменту может быть уже отменён.
const regressionWriteTimeout = 5 * time.Second

// regressionStore — узкий срез go-sdk redis.RedisService; прод подставляет
// *redis.RedisService без адаптера, тесты — in-memory фейк.
type regressionStore interface {
	Exists(ctx context.Context, keys ...string) (int64, error)
	IncrWithTTL(ctx context.Context, key string, expiration time.Duration) (int64, error)
	Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error
	Del(ctx context.Context, keys ...string) error
}

type regressionTracker struct {
	store  regressionStore
	logger *logrus.Logger
}

// markRed ставит отметку «последний регресс красный». Сбой Redis не роняет
// доставку красного алерта: он уходит как раньше, а следующий зелёный
// просто промолчит.
func (t regressionTracker) markRed(ctx context.Context, p DeployNotificationPayload) {
	value := p.RevisionURL
	if value == "" {
		value = "1"
	}
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), regressionWriteTimeout)
	defer cancel()
	if err := t.store.Set(wctx, regressionRedKeyPrefix+p.App, value, regressionRedTTL); err != nil {
		t.logger.WithContext(ctx).Errorf(
			"SDL-715: отметка red для %s не записана (%v) — следующий зелёный прогон промолчит", p.App, err)
	}
}

// recover шлёт «снова зелёный», если последний результат был красным, и
// снимает отметку. Возвращает announced=false, когда сообщение не отправлено
// намеренно (отметки нет, Redis недоступен, другая доставка уже объявляет).
//
// Сбой Redis = тишина: лучше пропустить «снова зелёный», чем слать его на
// каждый зелёный прогон. Ответ при этом 200 — красный джоб alert в зелёном
// пайплайне был бы ложной тревогой.
//
// Ошибку возвращает только провал самой рассылки: отметка при этом остаётся,
// и следующий зелёный прогон (или ретрай) объявит восстановление.
func (t regressionTracker) recover(ctx context.Context, p DeployNotificationPayload, send func(msg string) error) (bool, error) {
	log := t.logger.WithContext(ctx)
	redKey := regressionRedKeyPrefix + p.App
	claimKey := regressionClaimKeyPrefix + p.App

	n, err := t.store.Exists(ctx, redKey)
	if err != nil {
		log.Errorf("SDL-715: состояние регресса %s не прочитано (%v) — «снова зелёный» пропущен", p.App, err)
		return false, nil
	}
	if n == 0 {
		log.Infof("SDL-715: %s зелёный после зелёного — без сообщения", p.App)
		return false, nil
	}

	// Claim закрывает гонку двух доставок, одновременно увидевших red:
	// объявляет только первая. Дедуп одной и той же доставки по
	// Idempotency-Key остаётся на middleware маршрута.
	c, err := t.store.IncrWithTTL(ctx, claimKey, recoverClaimTTL)
	if err != nil {
		log.Errorf("SDL-715: claim %s не взят (%v) — «снова зелёный» пропущен", claimKey, err)
		return false, nil
	}
	if c > 1 {
		log.Infof("SDL-715: восстановление %s уже объявляет другая доставка", p.App)
		return false, nil
	}

	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), regressionWriteTimeout)
	defer cancel()
	defer func() {
		if err := t.store.Del(wctx, claimKey); err != nil {
			log.Errorf("SDL-715: claim %s не снят (%v) — истечёт через %s", claimKey, err, recoverClaimTTL)
		}
	}()

	if err := send(formatRegressionRecovered(p)); err != nil {
		return false, err
	}
	if err := t.store.Del(wctx, redKey); err != nil {
		log.Errorf("SDL-715: отметка red %s не снята (%v) — следующий зелёный повторит сообщение", p.App, err)
	}
	return true, nil
}

// dispatchDeployEvent — единая точка маршрутизации deploy-событий. sent=false
// только у regression-passed, промолчавшего намеренно.
func dispatchDeployEvent(ctx context.Context, t regressionTracker, p DeployNotificationPayload, send func(msg string) error) (bool, error) {
	switch p.EventType {
	case EventRegressionPassed:
		return t.recover(ctx, p, send)
	case EventRegressionFailed:
		t.markRed(ctx, p)
	}
	if err := send(formatDeployNotification(p)); err != nil {
		return false, err
	}
	return true, nil
}

// formatRegressionRecovered рендерит сообщение «снова зелёный» (HTML
// parse_mode). Ссылка на пайплайн — из revision_url, как у regression-failed.
func formatRegressionRecovered(p DeployNotificationPayload) string {
	var b strings.Builder
	fmt.Fprintf(&b, "✅ <b>регресс снова зелёный</b>\n<i>%s</i>\n", htmlEscape(p.App))
	if p.RevisionURL != "" {
		fmt.Fprintf(&b, "🔗 <a href=\"%s\">Пайплайн</a>\n", htmlEscape(p.RevisionURL))
	}
	if msg := strings.TrimSpace(p.Message); msg != "" {
		if len(msg) > 600 {
			msg = msg[:600] + "…"
		}
		fmt.Fprintf(&b, "\n<pre>%s</pre>", htmlEscape(msg))
	}
	return strings.TrimRight(b.String(), "\n")
}
