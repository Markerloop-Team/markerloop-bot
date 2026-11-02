package services

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"

	"telegram-bot-service/pkg/auth_client"
	"telegram-bot-service/pkg/telegram"
	"telegram-bot-service/pkg/user_client"

	sdkI18n "gitlab.com/markerloop/go-sdk/pkg/i18n"
	"gitlab.com/markerloop/go-sdk/pkg/redis"
)

// SDL-742 (ревью r2): пользователь запросил удаление аккаунта, каскад не
// доведён. user-service отвечает на поиск 423, auth-service на выдачу токенов —
// 423. Бот на это пишет понятный отказ и НЕ заводит пользователя заново, НЕ
// привязывает Telegram и НЕ выдаёт токены — ни на /start, ни на входе на сайт
// через бота (login_), ни через поиск по логину, когда внешний аккаунт потерян.

const tgID = int64(777)

// recordingBot запоминает отправленные тексты.
type recordingBot struct {
	telegram.Bot
	mu   sync.Mutex
	sent []string
}

func (b *recordingBot) SendMessage(_ int64, text string, _ *telegram.MessageOptions) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sent = append(b.sent, text)
	return nil
}

func (b *recordingBot) SendKeyboard(_ int64, text string, _ *telegram.InlineKeyboardMarkup) error {
	return b.SendMessage(0, text, nil)
}

// scenario — статусы, которыми отвечают заглушки.
type scenario struct {
	extLookup  int // POST /api/v1/internal/external-accounts/get-user
	createUser int // POST /api/v1/users
	byUsername int // GET /api/v1/users/username/:username
	issue      int // POST /api/v1/auth/internal/issue-tokens-for-user
}

// backend — заглушки user-service и auth-service со сценарием ответов.
type backend struct {
	scenario
	mu    sync.Mutex
	calls []string
}

func (b *backend) record(r *http.Request) {
	b.mu.Lock()
	b.calls = append(b.calls, r.Method+" "+r.URL.Path)
	b.mu.Unlock()
}

func (b *backend) called(prefix string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, c := range b.calls {
		if strings.HasPrefix(c, prefix) {
			return true
		}
	}
	return false
}

const lockedBody = `{"success":false,"message":"Аккаунт в процессе удаления","error":"account_deletion_pending"}`

func (b *backend) userService(w http.ResponseWriter, r *http.Request) {
	b.record(r)
	w.Header().Set("Content-Type", "application/json")
	reply := func(code int, ok string) {
		w.WriteHeader(code)
		switch code {
		case http.StatusLocked:
			_, _ = w.Write([]byte(lockedBody))
		case http.StatusConflict:
			_, _ = w.Write([]byte(`{"success":false,"message":"пользователь с таким username уже существует"}`))
		case http.StatusOK, http.StatusCreated:
			_, _ = w.Write([]byte(ok))
		default:
			_, _ = w.Write([]byte(`{"success":false,"error":"not_found"}`))
		}
	}
	user := `{"success":true,"data":{"id":"u-1","username":"tg_777"}}`
	switch {
	case r.URL.Path == "/api/v1/internal/external-accounts/get-user":
		reply(b.extLookup, user)
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/users":
		reply(b.createUser, user)
	case strings.HasPrefix(r.URL.Path, "/api/v1/users/username/"):
		reply(b.byUsername, user)
	default:
		reply(http.StatusCreated, `{"success":true,"data":{}}`)
	}
}

func (b *backend) authService(w http.ResponseWriter, r *http.Request) {
	b.record(r)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(b.issue)
	if b.issue == http.StatusLocked {
		_, _ = w.Write([]byte(lockedBody))
		return
	}
	_, _ = w.Write([]byte(`{"success":true,"data":{"user_id":"u-1","access_token":"a","refresh_token":"r","token_type":"Bearer","expires_in":3600}}`))
}

func newDeletionPendingService(t *testing.T, b *backend, redisValues map[string]string) (*TelegramService, *recordingBot) {
	t.Helper()
	logger := logrus.New()
	logger.SetLevel(logrus.PanicLevel)
	us := httptest.NewServer(http.HandlerFunc(b.userService))
	as := httptest.NewServer(http.HandlerFunc(b.authService))
	t.Cleanup(us.Close)
	t.Cleanup(as.Close)
	fr := startFakeRedis(t, redisValues)
	t.Setenv("REDIS_ADDR", fr.ln.Addr().String())
	bot := &recordingBot{}
	dead := "http://127.0.0.1:1" // dictionary-service недоступен: ключ перевода не найден
	return &TelegramService{
		bot:           bot,
		botUsername:   "testbot",
		redisService:  redis.NewRedisService(logger),
		authClient:    auth_client.NewClient(as.URL, "k", logger, 5*time.Second),
		userClient:    user_client.NewClient(us.URL, "k", logger),
		localizer:     sdkI18n.NewLocalizer(sdkI18n.NewClient(dead, logger), nil, "ru", logger),
		logger:        logger,
		oauthTokenTTL: time.Minute,
	}, bot
}

func TestDeletionPending_BotRefusesAndDoesNotRegister(t *testing.T) {
	const loginToken = "sdl742-login"
	pending := map[string]string{"telegram_login:" + loginToken: `{"status":"pending"}`}
	cases := []struct {
		name  string
		arg   string
		redis map[string]string
		sc    scenario
	}{
		{"/start: Telegram ID удаляемого аккаунта", "", nil,
			scenario{extLookup: http.StatusLocked, createUser: http.StatusCreated, byUsername: http.StatusOK, issue: http.StatusOK}},
		{"/start: внешний аккаунт потерян, логин удаляемого", "", nil,
			scenario{extLookup: http.StatusNotFound, createUser: http.StatusConflict, byUsername: http.StatusLocked, issue: http.StatusOK}},
		{"вход на сайт: Telegram ID удаляемого аккаунта", "login_" + loginToken, pending,
			scenario{extLookup: http.StatusLocked, createUser: http.StatusCreated, byUsername: http.StatusOK, issue: http.StatusOK}},
		{"вход на сайт: tg_{id} уже есть, он удаляется", "login_" + loginToken, pending,
			scenario{extLookup: http.StatusNotFound, createUser: http.StatusConflict, byUsername: http.StatusLocked, issue: http.StatusOK}},
		{"вход на сайт: auth-service отказал в токенах", "login_" + loginToken, pending,
			scenario{extLookup: http.StatusOK, createUser: http.StatusCreated, byUsername: http.StatusOK, issue: http.StatusLocked}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := &backend{scenario: tc.sc}
			if tc.redis == nil {
				tc.redis = map[string]string{}
			}
			s, bot := newDeletionPendingService(t, b, tc.redis)
			if err := s.HandleStartCommand(context.Background(), tgID, "gone", "Gone", "", "ru", tc.arg); err != nil {
				t.Fatalf("HandleStartCommand: %v", err)
			}
			if len(bot.sent) != 1 || bot.sent[0] != deletionPendingFallback["ru"] {
				t.Fatalf("бот ответил %q, ожидали один текст про удаление аккаунта", bot.sent)
			}
			if b.called("POST /api/v1/external-accounts") {
				t.Error("бот привязал Telegram к аккаунту, хотя вход закрыт")
			}
			if tc.sc.extLookup == http.StatusLocked && b.called("POST /api/v1/users") {
				t.Error("бот завёл нового пользователя вместо отказа")
			}
			if tc.sc.issue != http.StatusLocked && b.called("POST /api/v1/auth/internal/issue-tokens-for-user") {
				t.Error("бот запросил токены, хотя user-service уже отказал")
			}
		})
	}
}

// Без отказа поведение прежнее: новый Telegram ID регистрируется (регресс).
func TestDeletionPending_NewTelegramUserStillRegisters(t *testing.T) {
	b := &backend{scenario: scenario{extLookup: http.StatusNotFound, createUser: http.StatusCreated, byUsername: http.StatusOK, issue: http.StatusOK}}
	s, bot := newDeletionPendingService(t, b, map[string]string{})
	if err := s.HandleStartCommand(context.Background(), tgID, "fresh", "Fresh", "", "ru", ""); err != nil {
		t.Fatalf("HandleStartCommand: %v", err)
	}
	if !b.called("POST /api/v1/users") || !b.called("POST /api/v1/auth/internal/issue-tokens-for-user") {
		t.Errorf("новый пользователь не зарегистрирован: %v", b.calls)
	}
	for _, m := range bot.sent {
		if m == deletionPendingFallback["ru"] {
			t.Error("новому пользователю ответили про удаление аккаунта")
		}
	}
}
