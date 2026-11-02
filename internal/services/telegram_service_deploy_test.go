package services

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"

	"telegram-bot-service/pkg/telegram"
)

func TestFormatDeployNotification_Deployed(t *testing.T) {
	out := formatDeployNotification(DeployNotificationPayload{
		App:          "sauna-service",
		Status:       "Healthy",
		Revision:     "6024c8e5d4fa5ebbddf37ac8d7a94f3f1d61a9b9",
		EventType:    "on-deployed",
		RevisionURL:  "https://gitlab.com/markerloop/dev-infra/-/commit/6024c8e5",
		ArgocdURL:    "https://argocd.markerloop.com/applications/sauna-service",
		FinishedAt:   "2026-04-29T13:42:00Z",
		HealthStatus: "Healthy",
		SyncStatus:   "Synced",
	})

	for _, want := range []string{
		"🚀",
		"<b>sauna-service</b>",
		"Deployed",
		"Synced · Healthy",
		"2026-04-29 13:42 UTC",
		"<code>6024c8e5</code>",
		"<a href=\"https://gitlab.com/markerloop/dev-infra/-/commit/6024c8e5\">",
		"<a href=\"https://argocd.markerloop.com/applications/sauna-service\">ArgoCD</a>",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("rendered output missing %q\n---\n%s", want, out)
		}
	}
}

func TestFormatDeployNotification_SyncFailedWithMessage(t *testing.T) {
	out := formatDeployNotification(DeployNotificationPayload{
		App:       "gateway-service",
		EventType: "on-sync-failed",
		Message:   "rendered manifests contain a resource that already exists",
		StartedAt: "2026-04-29T13:42:00Z",
	})

	for _, want := range []string{
		"🔴",
		"<b>gateway-service</b>",
		"Sync failed",
		"<pre>rendered manifests",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("rendered output missing %q\n---\n%s", want, out)
		}
	}
}

func TestFormatDeployNotification_LegacyPayloadFallback(t *testing.T) {
	// Old notification-service shape: app/status/revision only.
	// EventType is empty; the formatter must still produce a sensible
	// headline based on Status alone.
	out := formatDeployNotification(DeployNotificationPayload{
		App:      "user-service",
		Status:   "Healthy",
		Revision: "abcdef12",
	})

	if !strings.Contains(out, "🚀") || !strings.Contains(out, "Deployed") {
		t.Fatalf("legacy payload should still resolve to Deployed; got:\n%s", out)
	}
	if !strings.Contains(out, "<code>abcdef12</code>") {
		t.Fatalf("legacy payload missing revision; got:\n%s", out)
	}
}

func TestFormatDeployNotification_HTMLEscaping(t *testing.T) {
	out := formatDeployNotification(DeployNotificationPayload{
		App:       "<evil>",
		EventType: "on-sync-failed",
		Message:   "exit code 1\n  cannot bind <port> to listener",
	})

	if strings.Contains(out, "<evil>") {
		t.Fatalf("app name not escaped: %s", out)
	}
	if !strings.Contains(out, "&lt;evil&gt;") {
		t.Fatalf("expected escaped app name; got:\n%s", out)
	}
	if !strings.Contains(out, "&lt;port&gt;") {
		t.Fatalf("expected escaped message; got:\n%s", out)
	}
}

func TestFormatDeployNotification_LongMessageTruncated(t *testing.T) {
	long := strings.Repeat("x", 1000)
	out := formatDeployNotification(DeployNotificationPayload{
		App:       "x",
		EventType: "on-sync-failed",
		Message:   long,
	})
	if strings.Count(out, "x") > 700 {
		t.Fatalf("long message not truncated; got len=%d", len(out))
	}
	if !strings.Contains(out, "…") {
		t.Fatalf("expected ellipsis on truncated message; got:\n%s", out)
	}
}

func TestFormatTimestamp(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"2026-04-29T13:42:00Z", "2026-04-29 13:42 UTC"},
		{"2026-04-29T13:42:00.123Z", "2026-04-29 13:42 UTC"},
		{"", ""},
		{"not-a-date", "not-a-date"},
	}
	for _, c := range cases {
		if got := formatTimestamp(c.in); got != c.want {
			t.Errorf("formatTimestamp(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// --- SDL-345: инвариант «0 из N — ошибка, частичный успех — не ошибка» ---

type fakeBot struct {
	telegram.Bot // паникует на невызываемых методах — тест их не трогает
	failFor      map[int64]bool
	calls        int
}

func (b *fakeBot) SendMessage(chatID int64, _ string, _ *telegram.MessageOptions) error {
	b.calls++
	if b.failFor[chatID] {
		return fmt.Errorf("telegram: 429")
	}
	return nil
}

func TestFanOutDeployCountsDeliveries(t *testing.T) {
	log := logrus.New()
	log.SetOutput(io.Discard)
	ctx := context.Background()

	t.Run("все упали — 0 доставок", func(t *testing.T) {
		bot := &fakeBot{failFor: map[int64]bool{1: true, 2: true}}
		if got := fanOutDeploy(ctx, bot, log, []int64{1, 2}, "msg"); got != 0 {
			t.Fatalf("delivered=%d, ожидался 0", got)
		}
		if bot.calls != 2 {
			t.Fatalf("попыток %d, ожидалось 2 (падение одного не прерывает рассылку)", bot.calls)
		}
	})

	t.Run("частичный успех считается", func(t *testing.T) {
		bot := &fakeBot{failFor: map[int64]bool{2: true}}
		if got := fanOutDeploy(ctx, bot, log, []int64{1, 2, 3}, "msg"); got != 2 {
			t.Fatalf("delivered=%d, ожидалось 2", got)
		}
	})
}
