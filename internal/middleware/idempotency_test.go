package middleware

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// fakeStore — in-memory реализация DedupStore. Redis-семантику истечения
// TTL не эмулирует: тесты проверяют НАШУ логику (какие ключи, когда и с
// каким TTL пишутся и снимаются), а истечение окна — семантика Redis.
type fakeStore struct {
	mu       sync.Mutex
	keys     map[string]time.Duration // key → ttl, с которым записан
	counters map[string]int64         // для IncrWithTTL
	ops      []string                 // журнал операций для проверки порядка
	failAll  bool
}

func newFakeStore() *fakeStore {
	return &fakeStore{keys: map[string]time.Duration{}, counters: map[string]int64{}}
}

func (f *fakeStore) Exists(_ context.Context, keys ...string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failAll {
		return 0, errors.New("redis down")
	}
	var n int64
	for _, k := range keys {
		if _, ok := f.keys[k]; ok {
			n++
		}
	}
	return n, nil
}

func (f *fakeStore) IncrWithTTL(_ context.Context, key string, ttl time.Duration) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failAll {
		return 0, errors.New("redis down")
	}
	f.counters[key]++
	if f.counters[key] == 1 {
		f.keys[key] = ttl
	}
	f.ops = append(f.ops, "claim "+key)
	return f.counters[key], nil
}

func (f *fakeStore) Set(_ context.Context, key string, _ interface{}, ttl time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failAll {
		return errors.New("redis down")
	}
	f.keys[key] = ttl
	f.ops = append(f.ops, "set "+key)
	return nil
}

func (f *fakeStore) Del(_ context.Context, keys ...string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failAll {
		return errors.New("redis down")
	}
	for _, k := range keys {
		delete(f.keys, k)
		delete(f.counters, k)
		f.ops = append(f.ops, "del "+k)
	}
	return nil
}

func (f *fakeStore) has(key string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.keys[key]
	return ok
}

func newEngine(store DedupStore, handler gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	log := logrus.New()
	log.SetOutput(io.Discard)
	r := gin.New()
	r.POST("/deliver", Idempotency(store, time.Hour, log), handler)
	return r
}

func countingHandler(status int, calls *int) gin.HandlerFunc {
	return func(c *gin.Context) {
		*calls++
		c.JSON(status, gin.H{"success": status < 300})
	}
}

func post(r *gin.Engine, key string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/deliver", strings.NewReader(`{}`))
	if key != "" {
		req.Header.Set(IdempotencyHeader, key)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// Без заголовка дедуп не вмешивается — обратная совместимость со старым
// notification-service (порядок промоутов свободный).
func TestNoHeaderPassesThroughEveryTime(t *testing.T) {
	calls := 0
	r := newEngine(newFakeStore(), countingHandler(http.StatusOK, &calls))

	post(r, "")
	post(r, "")

	if calls != 2 {
		t.Fatalf("хендлер вызван %d раз, ожидалось 2", calls)
	}
}

// Повтор ключа после успеха подавляется: хендлер один раз, второй ответ —
// 200 с duplicate, Telegram не трогается.
func TestDuplicateKeySuppressedAfterSuccess(t *testing.T) {
	calls := 0
	r := newEngine(newFakeStore(), countingHandler(http.StatusOK, &calls))

	if w := post(r, "key-1"); w.Code != http.StatusOK {
		t.Fatalf("первая доставка: код %d", w.Code)
	}
	w := post(r, "key-1")

	if calls != 1 {
		t.Fatalf("хендлер вызван %d раз, ожидался 1", calls)
	}
	if w.Code != http.StatusOK {
		t.Fatalf("дубль: код %d, ожидался 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), "duplicate") {
		t.Errorf("дубль не помечен в ответе: %s", w.Body.String())
	}
}

func TestDifferentKeysBothProcessed(t *testing.T) {
	calls := 0
	r := newEngine(newFakeStore(), countingHandler(http.StatusOK, &calls))

	post(r, "key-1")
	post(r, "key-2")

	if calls != 2 {
		t.Fatalf("хендлер вызван %d раз, ожидалось 2", calls)
	}
}

// done пишется ТОЛЬКО по успеху, claim снимается всегда: после 500 ретрай
// с тем же ключом обрабатывается заново — ради этих повторов отправитель
// и ретраит.
func TestFailedDeliveryIsRetriable(t *testing.T) {
	calls := 0
	store := newFakeStore()
	r := newEngine(store, countingHandler(http.StatusInternalServerError, &calls))

	post(r, "key-1")

	if store.has("notify:idem:done:/deliver:key-1") {
		t.Fatal("done-ключ записан несмотря на 500")
	}
	if store.has("notify:idem:claim:/deliver:key-1") {
		t.Fatal("claim не снят после 500")
	}

	post(r, "key-1")
	if calls != 2 {
		t.Fatalf("хендлер вызван %d раз, ожидалось 2", calls)
	}
}

// Конкурентная доставка того же ключа (ретрай обогнал медленную рассылку):
// вторая получает 409 — retryable по контракту SDL-346, НЕ 200: исход
// первой ещё неизвестен, и «успех» остановил бы ретраи с риском потери.
func TestInFlightDuplicateGets409(t *testing.T) {
	store := newFakeStore()
	release := make(chan struct{})
	entered := make(chan struct{})
	slowHandler := func(c *gin.Context) {
		close(entered)
		<-release
		c.JSON(http.StatusOK, gin.H{"success": true})
	}
	r := newEngine(store, slowHandler)

	done := make(chan *httptest.ResponseRecorder)
	go func() { done <- post(r, "key-1") }()
	<-entered // первая доставка внутри хендлера, claim взят

	w2 := post(r, "key-1")
	if w2.Code != http.StatusConflict {
		t.Fatalf("in-flight дубль: код %d, ожидался 409", w2.Code)
	}

	close(release)
	if w1 := <-done; w1.Code != http.StatusOK {
		t.Fatalf("первая доставка: код %d", w1.Code)
	}
	// После завершения первой ретрай видит done → 200 duplicate.
	w3 := post(r, "key-1")
	if w3.Code != http.StatusOK || !strings.Contains(w3.Body.String(), "duplicate") {
		t.Fatalf("ретрай после завершения: код %d, тело %s", w3.Code, w3.Body.String())
	}
}

// Redis недоступен → fail-open: доставка идёт без дедупа, не 500 и не потеря.
func TestRedisDownFailsOpen(t *testing.T) {
	calls := 0
	store := newFakeStore()
	store.failAll = true
	r := newEngine(store, countingHandler(http.StatusOK, &calls))

	if w := post(r, "key-1"); w.Code != http.StatusOK {
		t.Fatalf("код %d, ожидался 200", w.Code)
	}
	if calls != 1 {
		t.Fatalf("хендлер вызван %d раз, ожидался 1", calls)
	}
}

// Ключ длиннее лимита — тоже fail-open (не 400): чужой отправитель не
// должен терять доставку из-за формата, она просто идёт без дедупа.
func TestOversizedKeyFailsOpen(t *testing.T) {
	calls := 0
	store := newFakeStore()
	r := newEngine(store, countingHandler(http.StatusOK, &calls))

	w := post(r, strings.Repeat("x", maxIdempotencyKeyLen+1))

	if w.Code != http.StatusOK {
		t.Fatalf("код %d, ожидался 200", w.Code)
	}
	if calls != 1 {
		t.Fatalf("хендлер вызван %d раз, ожидался 1", calls)
	}
	if len(store.keys) != 0 {
		t.Errorf("для oversized-ключа что-то записано в store: %v", store.keys)
	}
}

// Ключи скоупятся маршрутом и пишутся с ожидаемыми TTL; done ставится
// раньше, чем снимается claim (иначе окно, где не защищает ни тот, ни
// другой).
func TestKeyScopeTTLAndOrder(t *testing.T) {
	calls := 0
	store := newFakeStore()
	r := newEngine(store, countingHandler(http.StatusOK, &calls))

	post(r, "key-1")

	if ttl := store.keys["notify:idem:done:/deliver:key-1"]; ttl != time.Hour {
		t.Fatalf("done: ttl %v (ожидался 1h); ключи: %v", ttl, store.keys)
	}
	if store.has("notify:idem:claim:/deliver:key-1") {
		t.Fatal("claim не снят после успеха")
	}
	want := []string{
		"claim notify:idem:claim:/deliver:key-1",
		"set notify:idem:done:/deliver:key-1",
		"del notify:idem:claim:/deliver:key-1",
	}
	if len(store.ops) != len(want) {
		t.Fatalf("операции: %v", store.ops)
	}
	for i := range want {
		if store.ops[i] != want[i] {
			t.Fatalf("порядок операций: %v, ожидался %v", store.ops, want)
		}
	}
}

// TTL<=0 из криво собранного конфига не превращает ключи в вечные —
// middleware подставляет дефолт (нулевой TTL Redis трактует как «навсегда»).
func TestZeroTTLFallsBackToDefault(t *testing.T) {
	gin.SetMode(gin.TestMode)
	log := logrus.New()
	log.SetOutput(io.Discard)
	store := newFakeStore()
	r := gin.New()
	r.POST("/deliver", Idempotency(store, 0, log), countingHandler(http.StatusOK, new(int)))

	post(r, "key-1")

	if ttl := store.keys["notify:idem:done:/deliver:key-1"]; ttl != defaultIdempotencyTTL {
		t.Fatalf("done: ttl %v, ожидался дефолт %v", ttl, defaultIdempotencyTTL)
	}
}

// Паника хендлера не оставляет висящий claim (иначе ретраи глушатся до
// claimTTL) и не отмечает done; сама паника пробрасывается дальше —
// глобальный Recovery стоит снаружи этого middleware.
func TestPanicReleasesClaimAndPropagates(t *testing.T) {
	store := newFakeStore()
	r := newEngine(store, func(c *gin.Context) { panic("boom") })

	func() {
		defer func() {
			if recover() == nil {
				t.Error("паника не проброшена наружу")
			}
		}()
		post(r, "key-1")
	}()

	if store.has("notify:idem:claim:/deliver:key-1") {
		t.Fatal("claim не снят после паники")
	}
	if store.has("notify:idem:done:/deliver:key-1") {
		t.Fatal("done записан несмотря на панику")
	}
}
