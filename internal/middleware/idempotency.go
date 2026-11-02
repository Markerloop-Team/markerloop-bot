package middleware

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"gitlab.com/markerloop/go-sdk/pkg/response"
)

// IdempotencyHeader — заголовок, которым отправитель помечает доставку.
// Один ключ живёт на все ретраи одной и той же доставки (EP-56):
// notification-service генерирует его перед первой попыткой и повторяет
// в каждой следующей. Контракт продублирован тестом на стороне
// отправителя (SDL-346) и e2e-приёмкой (SDL-347).
const IdempotencyHeader = "Idempotency-Key"

// Префиксы отделяют ключи дедупа в Redis, который бот делит со своими
// сессиями (а в проде БД и с auth-service). Ключ дополнительно скоупится
// маршрутом: один и тот же Idempotency-Key на /send и /deploy — разные
// доставки, и подавлять вторую по следу первой нельзя.
const (
	doneKeyPrefix  = "notify:idem:done:"
	claimKeyPrefix = "notify:idem:claim:"
)

// maxIdempotencyKeyLen ограничивает ключ (в байтах): он попадает в Redis
// как часть имени ключа. Превышение НЕ отклоняет доставку — см. fail-open
// ниже.
const maxIdempotencyKeyLen = 128

// defaultIdempotencyTTL страхует от TTL<=0 из криво собранного конфига:
// нулевой TTL Redis трактует как «вечный ключ», то есть вечное подавление
// ретраев и утечка ключей.
const defaultIdempotencyTTL = time.Hour

// claimTTL — время жизни in-flight-клейма. Страхует от пода, убитого
// посреди рассылки: без Del клейм истечёт сам и ретрай пройдёт. Должен
// быть больше максимальной длительности рассылки (WriteTimeout сервера
// 30s), но не настолько, чтобы упавший под надолго глушил ретраи.
const claimTTL = 2 * time.Minute

// markTimeout — бюджет на пост-обработочные записи в Redis. Request-context
// к этому моменту может быть уже отменён (клиент отвалился по таймауту —
// ровно сценарий ретрая), поэтому используется отвязанный контекст.
const markTimeout = 5 * time.Second

// failOpenLogEvery — троттлинг логов деградации: Redis-outage при рассылке
// не должен давать строку на каждый запрос. Errorf (не Warnf) — осознанно:
// только Error доезжает до Sentry через хук go-sdk.
const failOpenLogEvery = time.Minute

// DedupStore — узкий срез API go-sdk redis.RedisService (по образцу
// refreshStore в auth-service): прод подставляет *redis.RedisService
// без адаптера, тесты — in-memory фейк.
type DedupStore interface {
	Exists(ctx context.Context, keys ...string) (int64, error)
	// IncrWithTTL атомарен (Lua INCR+PEXPIRE): первый инкремент возвращает 1
	// и ставит TTL. Это и есть «claim»: n==1 — мы первые, n>1 — доставка
	// этого ключа уже в полёте.
	IncrWithTTL(ctx context.Context, key string, expiration time.Duration) (int64, error)
	Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error
	Del(ctx context.Context, keys ...string) error
}

// Idempotency подавляет ПОВТОРНУЮ доставку уведомления с тем же
// Idempotency-Key. Семантика (EP-56, SDL-345; пересмотрена по code-review):
//
//   - done-ключ пишется ТОЛЬКО после успешного ответа хендлера (<300) —
//     ретрай после реального сбоя обработки обрабатывается заново;
//   - окно «между проверкой и отметкой» закрыто атомарным claim'ом
//     (IncrWithTTL): конкурентная доставка того же ключа получает
//     409 — отправитель обязан считать его retryable (контракт SDL-346);
//   - запрос без заголовка проходит как раньше — обратная совместимость,
//     порядок промоутов свободный (expand/contract, как SDL-323/330);
//   - любая деградация — fail-open: Redis недоступен или ключ длиннее
//     лимита → доставка идёт БЕЗ дедупа. Редкий дубль лучше потерянного
//     уведомления или 500 на ровном месте.
func Idempotency(store DedupStore, ttl time.Duration, logger *logrus.Logger) gin.HandlerFunc {
	if ttl <= 0 {
		ttl = defaultIdempotencyTTL
	}
	var lastFailLog atomic.Int64

	logFailOpen := func(format string, args ...interface{}) {
		now := time.Now().Unix()
		last := lastFailLog.Load()
		if now-last >= int64(failOpenLogEvery/time.Second) && lastFailLog.CompareAndSwap(last, now) {
			logger.Errorf(format, args...)
		}
	}

	return func(c *gin.Context) {
		key := c.GetHeader(IdempotencyHeader)
		if key == "" {
			c.Next()
			return
		}
		if len(key) > maxIdempotencyKeyLen {
			// Fail-open: чужой/сломанный отправитель не должен терять
			// доставку из-за формата ключа — она просто идёт без дедупа.
			logFailOpen("Idempotency: ключ длиннее %d байт — доставка без дедупа", maxIdempotencyKeyLen)
			c.Next()
			return
		}

		scope := c.FullPath() + ":" + key
		doneKey := doneKeyPrefix + scope
		claimKey := claimKeyPrefix + scope
		ctx := c.Request.Context()

		n, err := store.Exists(ctx, doneKey)
		if err != nil {
			logFailOpen("Idempotency: Redis недоступен (%v) — доставка без дедупа", err)
			c.Next()
			return
		}
		if n > 0 {
			// Уже доставляли: отвечаем успехом, НЕ трогая Telegram.
			response.Success(c, gin.H{"duplicate": true}, "Уже обработано (idempotency)")
			c.Abort()
			return
		}

		cnt, err := store.IncrWithTTL(ctx, claimKey, claimTTL)
		if err != nil {
			logFailOpen("Idempotency: claim не взят (%v) — доставка без дедупа", err)
			c.Next()
			return
		}
		if cnt > 1 {
			// Та же доставка уже в полёте (ретрай обогнал медленную рассылку).
			// 409 = «повтори позже»: исход первой попытки ещё неизвестен,
			// отвечать успехом нельзя — если она провалится, отправитель,
			// получивший 200, уже остановился бы и уведомление потерялось.
			response.Conflict(c, "Доставка с этим Idempotency-Key уже выполняется, повторите позже")
			c.Abort()
			return
		}

		// Claim наш. Снять его (и при успехе отметить done) обязаны при
		// ЛЮБОМ исходе, включая панику хендлера — иначе ретраи глушатся
		// до истечения claimTTL. Request-context к этому моменту может
		// быть отменён, поэтому записи идут на отвязанном контексте.
		defer func() {
			r := recover()

			mctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), markTimeout)
			defer cancel()

			if r == nil && c.Writer.Status() < 300 {
				if err := store.Set(mctx, doneKey, "1", ttl); err != nil {
					logFailOpen("Idempotency: done-ключ %s не записан (%v) — возможен дубль при ретрае", doneKey, err)
				}
			}
			if err := store.Del(mctx, claimKey); err != nil {
				logFailOpen("Idempotency: claim %s не снят (%v) — ретраи подавлены до %s", claimKey, err, claimTTL)
			}

			if r != nil {
				panic(r)
			}
		}()

		c.Next()
	}
}
