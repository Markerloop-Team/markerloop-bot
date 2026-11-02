package services

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

// --- SDL-715: «✅ регресс снова зелёный» после красного ---

type memStore struct {
	keys    map[string]string
	ttls    map[string]time.Duration
	failAll error // любой вызов возвращает эту ошибку
}

func newMemStore() *memStore {
	return &memStore{keys: map[string]string{}, ttls: map[string]time.Duration{}}
}

func (m *memStore) Exists(_ context.Context, keys ...string) (int64, error) {
	if m.failAll != nil {
		return 0, m.failAll
	}
	var n int64
	for _, k := range keys {
		if _, ok := m.keys[k]; ok {
			n++
		}
	}
	return n, nil
}

func (m *memStore) IncrWithTTL(_ context.Context, key string, ttl time.Duration) (int64, error) {
	if m.failAll != nil {
		return 0, m.failAll
	}
	if _, ok := m.keys[key]; ok {
		return 2, nil
	}
	m.keys[key] = "1"
	m.ttls[key] = ttl
	return 1, nil
}

func (m *memStore) Set(_ context.Context, key string, value interface{}, ttl time.Duration) error {
	if m.failAll != nil {
		return m.failAll
	}
	m.keys[key] = value.(string)
	m.ttls[key] = ttl
	return nil
}

func (m *memStore) Del(_ context.Context, keys ...string) error {
	if m.failAll != nil {
		return m.failAll
	}
	for _, k := range keys {
		delete(m.keys, k)
	}
	return nil
}

type sentLog struct {
	msgs []string
	err  error
}

func (s *sentLog) send(msg string) error {
	if s.err != nil {
		return s.err
	}
	s.msgs = append(s.msgs, msg)
	return nil
}

const pipeA = "https://gitlab.com/markerloop/dev-infra/-/pipelines/101"
const pipeB = "https://gitlab.com/markerloop/dev-infra/-/pipelines/102"

// Ровно то, что шлёт alert-regression-failed в dev-infra/.gitlab-ci.yml.
func failedEvent(url string) DeployNotificationPayload {
	return DeployNotificationPayload{
		App:         "full-regression",
		Status:      "failed",
		EventType:   EventRegressionFailed,
		Message:     "Регресс/канарейка красные (source: schedule). Пайплайн: " + url,
		RevisionURL: url,
	}
}

func passedEvent(url string) DeployNotificationPayload {
	return DeployNotificationPayload{
		App:         "full-regression",
		Status:      "success",
		EventType:   EventRegressionPassed,
		RevisionURL: url,
	}
}

func newTracker(store regressionStore) regressionTracker {
	log := logrus.New()
	log.SetOutput(io.Discard)
	return regressionTracker{store: store, logger: log}
}

const redKey = regressionRedKeyPrefix + "full-regression"
const claimKey = regressionClaimKeyPrefix + "full-regression"

func TestRegression_RedThenPassed_SendsExactlyOne(t *testing.T) {
	ctx := context.Background()
	store := newMemStore()
	tr := newTracker(store)
	out := &sentLog{}

	if _, err := dispatchDeployEvent(ctx, tr, failedEvent(pipeA), out.send); err != nil {
		t.Fatalf("failed event: %v", err)
	}
	sent, err := dispatchDeployEvent(ctx, tr, passedEvent(pipeB), out.send)
	if err != nil || !sent {
		t.Fatalf("passed after red: sent=%v err=%v, ожидалось sent=true", sent, err)
	}
	if len(out.msgs) != 2 {
		t.Fatalf("сообщений %d, ожидалось 2 (красное + снова зелёный)", len(out.msgs))
	}
	green := out.msgs[1]
	for _, want := range []string{"✅", "регресс снова зелёный", "full-regression", `<a href="` + pipeB + `">Пайплайн</a>`} {
		if !strings.Contains(green, want) {
			t.Fatalf("в сообщении нет %q:\n%s", want, green)
		}
	}
	if _, ok := store.keys[redKey]; ok {
		t.Fatalf("отметка red не снята после объявления")
	}
	if _, ok := store.keys[claimKey]; ok {
		t.Fatalf("claim не снят после объявления")
	}
}

func TestRegression_PassedThenPassed_Silent(t *testing.T) {
	ctx := context.Background()
	tr := newTracker(newMemStore())
	out := &sentLog{}

	for i := 0; i < 2; i++ {
		sent, err := dispatchDeployEvent(ctx, tr, passedEvent(pipeA), out.send)
		if err != nil || sent {
			t.Fatalf("passed #%d без красного: sent=%v err=%v, ожидалась тишина", i+1, sent, err)
		}
	}
	if len(out.msgs) != 0 {
		t.Fatalf("зелёный после зелёного прислал %d сообщений", len(out.msgs))
	}
}

func TestRegression_RedThenRed_AlertsAsBeforeAndKeepsMark(t *testing.T) {
	ctx := context.Background()
	store := newMemStore()
	tr := newTracker(store)
	out := &sentLog{}

	for _, url := range []string{pipeA, pipeB} {
		sent, err := dispatchDeployEvent(ctx, tr, failedEvent(url), out.send)
		if err != nil || !sent {
			t.Fatalf("failed event: sent=%v err=%v", sent, err)
		}
	}
	if len(out.msgs) != 2 {
		t.Fatalf("красных сообщений %d, ожидалось 2", len(out.msgs))
	}
	// Рендер красного не изменился: тот же formatDeployNotification, что до SDL-715.
	if want := formatDeployNotification(failedEvent(pipeB)); out.msgs[1] != want {
		t.Fatalf("рендер красного изменился:\n got: %s\nwant: %s", out.msgs[1], want)
	}
	if !strings.Contains(out.msgs[1], "🔴") {
		t.Fatalf("красное сообщение без 🔴:\n%s", out.msgs[1])
	}
	if store.keys[redKey] != pipeB {
		t.Fatalf("отметка red = %q, ожидался URL последнего красного %q", store.keys[redKey], pipeB)
	}
	if store.ttls[redKey] != 0 {
		t.Fatalf("отметка red с TTL %s, ожидалась без TTL", store.ttls[redKey])
	}
}

func TestRegression_RepeatedPassedEvent_NoSecondMessage(t *testing.T) {
	ctx := context.Background()
	tr := newTracker(newMemStore())
	out := &sentLog{}

	dispatchDeployEvent(ctx, tr, failedEvent(pipeA), out.send)
	out.msgs = nil

	// Повторная доставка того же события (тот же пайплайн).
	for i := 0; i < 3; i++ {
		if _, err := dispatchDeployEvent(ctx, tr, passedEvent(pipeB), out.send); err != nil {
			t.Fatalf("passed #%d: %v", i+1, err)
		}
	}
	if len(out.msgs) != 1 {
		t.Fatalf("повтор passed прислал %d сообщений, ожидалось ровно 1", len(out.msgs))
	}
}

func TestRegression_ConcurrentRecoveryClaimed_Silent(t *testing.T) {
	ctx := context.Background()
	store := newMemStore()
	tr := newTracker(store)
	out := &sentLog{}

	dispatchDeployEvent(ctx, tr, failedEvent(pipeA), out.send)
	out.msgs = nil
	store.keys[claimKey] = "1" // другая доставка уже объявляет

	sent, err := dispatchDeployEvent(ctx, tr, passedEvent(pipeB), out.send)
	if err != nil || sent || len(out.msgs) != 0 {
		t.Fatalf("при чужом claim: sent=%v err=%v msgs=%d, ожидалась тишина", sent, err, len(out.msgs))
	}
	if _, ok := store.keys[claimKey]; !ok {
		t.Fatalf("чужой claim снят — третья доставка прошла бы")
	}
	if _, ok := store.keys[redKey]; !ok {
		t.Fatalf("отметку red снял не владелец claim")
	}
}

func TestRegression_RedisDown_PassedSilent_FailedStillAlerts(t *testing.T) {
	ctx := context.Background()
	store := newMemStore()
	store.failAll = errors.New("redis: connection refused")
	tr := newTracker(store)
	out := &sentLog{}

	sent, err := dispatchDeployEvent(ctx, tr, failedEvent(pipeA), out.send)
	if err != nil || !sent || len(out.msgs) != 1 {
		t.Fatalf("красный при лежащем Redis обязан уйти: sent=%v err=%v msgs=%d", sent, err, len(out.msgs))
	}
	sent, err = dispatchDeployEvent(ctx, tr, passedEvent(pipeB), out.send)
	if err != nil || sent || len(out.msgs) != 1 {
		t.Fatalf("passed при лежащем Redis: sent=%v err=%v msgs=%d, ожидалась тишина без ошибки", sent, err, len(out.msgs))
	}
}

func TestRegression_RecoverySendFails_MarkStaysForRetry(t *testing.T) {
	ctx := context.Background()
	store := newMemStore()
	tr := newTracker(store)

	dispatchDeployEvent(ctx, tr, failedEvent(pipeA), (&sentLog{}).send)

	broken := &sentLog{err: errors.New("deploy notification: не доставлено ни одному из 1 подписчиков")}
	if sent, err := dispatchDeployEvent(ctx, tr, passedEvent(pipeB), broken.send); err == nil || sent {
		t.Fatalf("провал рассылки обязан всплыть ошибкой: sent=%v err=%v", sent, err)
	}
	if _, ok := store.keys[redKey]; !ok {
		t.Fatalf("отметка red снята при провале рассылки — восстановление потеряно")
	}
	if _, ok := store.keys[claimKey]; ok {
		t.Fatalf("claim не снят после провала — ретрай заглушён")
	}

	out := &sentLog{}
	if sent, err := dispatchDeployEvent(ctx, tr, passedEvent(pipeB), out.send); err != nil || !sent || len(out.msgs) != 1 {
		t.Fatalf("ретрай после провала: sent=%v err=%v msgs=%d, ожидалось одно сообщение", sent, err, len(out.msgs))
	}
}

func TestRegression_ScopedByApp(t *testing.T) {
	ctx := context.Background()
	tr := newTracker(newMemStore())
	out := &sentLog{}

	dispatchDeployEvent(ctx, tr, failedEvent(pipeA), out.send)
	out.msgs = nil
	other := passedEvent(pipeB)
	other.App = "other-suite"
	if sent, _ := dispatchDeployEvent(ctx, tr, other, out.send); sent || len(out.msgs) != 0 {
		t.Fatalf("зелёный другого app снял чужую отметку")
	}
}

func TestFormatRegressionRecovered_NoURL(t *testing.T) {
	out := formatRegressionRecovered(DeployNotificationPayload{App: "full-regression"})
	if strings.Contains(out, "<a ") {
		t.Fatalf("без revision_url ссылки быть не должно:\n%s", out)
	}
	if !strings.HasPrefix(out, "✅ <b>регресс снова зелёный</b>") {
		t.Fatalf("неожиданный заголовок:\n%s", out)
	}
}
