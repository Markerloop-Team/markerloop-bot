package services

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"

	"telegram-bot-service/pkg/telegram"
	"telegram-bot-service/pkg/user_client"

	sdkI18n "gitlab.com/markerloop/go-sdk/pkg/i18n"
	"gitlab.com/markerloop/go-sdk/pkg/redis"
)

// SDL-738: аргумент /start — одноразовый токен входа и текст пользователя.
// В журнал уходят поток (login/deeplink/oauth/fallback), tg_id и отпечаток
// (длина + 8 hex sha256), но никогда сам аргумент.

const startProbe = "sdl738-probe-071433"

// fakeRedis — минимальный RESP2-сервер: GET отдаёт значение из карты или
// nil, остальные команды отвечают +OK / +PONG. Хватает go-redis для Get/Set.
type fakeRedis struct {
	ln     net.Listener
	mu     sync.Mutex
	values map[string]string
}

func startFakeRedis(t *testing.T, values map[string]string) *fakeRedis {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := &fakeRedis{ln: ln, values: values}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go s.handle(conn)
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return s
}

func (s *fakeRedis) handle(conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	for {
		args, err := readRESP(r)
		if err != nil {
			return
		}
		if len(args) == 0 {
			continue
		}
		var out string
		switch strings.ToUpper(args[0]) {
		case "HELLO":
			out = "-ERR unknown command 'HELLO'\r\n"
		case "PING":
			out = "+PONG\r\n"
		case "GET":
			s.mu.Lock()
			v, ok := s.values[args[1]]
			s.mu.Unlock()
			if ok {
				out = "$" + strconv.Itoa(len(v)) + "\r\n" + v + "\r\n"
			} else {
				out = "$-1\r\n"
			}
		default:
			out = "+OK\r\n"
		}
		if _, err := conn.Write([]byte(out)); err != nil {
			return
		}
	}
}

func readRESP(r *bufio.Reader) ([]string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return nil, err
	}
	line = strings.TrimRight(line, "\r\n")
	if !strings.HasPrefix(line, "*") {
		return strings.Fields(line), nil
	}
	n, _ := strconv.Atoi(line[1:])
	args := make([]string, 0, n)
	for i := 0; i < n; i++ {
		hdr, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		size, _ := strconv.Atoi(strings.TrimRight(hdr, "\r\n")[1:])
		buf := make([]byte, size+2)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		args = append(args, string(buf[:size]))
	}
	return args, nil
}

// silentBot — Bot, у которого отправка сообщений ничего не делает.
type silentBot struct{ telegram.Bot }

func (silentBot) SendMessage(int64, string, *telegram.MessageOptions) error        { return nil }
func (silentBot) SendKeyboard(int64, string, *telegram.InlineKeyboardMarkup) error { return nil }

func newStartLogService(t *testing.T, values map[string]string) (*TelegramService, *bytes.Buffer) {
	t.Helper()
	buf := &bytes.Buffer{}
	logger := logrus.New()
	logger.SetOutput(buf)
	logger.SetLevel(logrus.DebugLevel)
	logger.SetFormatter(&logrus.JSONFormatter{})

	fr := startFakeRedis(t, values)
	t.Setenv("REDIS_ADDR", fr.ln.Addr().String())
	// Недоступные адреса: dictionary и user-service отвечают отказом сразу.
	dead := "http://127.0.0.1:1"
	return &TelegramService{
		bot:           silentBot{},
		botUsername:   "testbot",
		redisService:  redis.NewRedisService(logger),
		userClient:    user_client.NewClient(dead, "", logger),
		localizer:     sdkI18n.NewLocalizer(sdkI18n.NewClient(dead, logger), nil, "ru", logger),
		logger:        logger,
		oauthTokenTTL: time.Minute,
	}, buf
}

func assertNoProbe(t *testing.T, out, secret string, want ...string) {
	t.Helper()
	for _, leak := range []string{secret, "leakprobe_alice"} {
		if strings.Contains(out, leak) {
			t.Fatalf("журнал содержит %q:\n%s", leak, out)
		}
	}
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Fatalf("в журнале нет %q:\n%s", w, out)
		}
	}
}

func TestStartArgNotLogged(t *testing.T) {
	ctx := context.Background()
	fp := tokenFingerprint(startProbe)
	cases := []struct {
		name, arg string
		values    map[string]string
		want      []string
	}{
		{"oauth invalid", startProbe, nil, []string{"Неверный или истекший токен: flow=oauth tg_id=424242", fp}},
		{"login invalid", "login_" + startProbe, nil, []string{"Неверный или истекший токен: flow=login tg_id=424242", fp}},
		{"deeplink invalid", "link_" + startProbe, nil, []string{"Неверный или истекший токен: flow=deeplink tg_id=424242", fp}},
		{"login used", "login_" + startProbe,
			map[string]string{"telegram_login:" + startProbe: `{"status":"completed"}`},
			[]string{"Токен уже использован: flow=login tg_id=424242", fp}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.values == nil {
				tc.values = map[string]string{}
			}
			s, buf := newStartLogService(t, tc.values)
			if err := s.HandleStartCommand(ctx, 424242, "leakprobe_alice", "Alisa", "Probe", "ru", tc.arg); err != nil {
				t.Fatalf("HandleStartCommand: %v", err)
			}
			assertNoProbe(t, buf.String(), startProbe, tc.want...)
		})
	}
}

// Fallback-токен (user-service недоступен) — тоже секрет: deeplink на вход.
func TestFallbackTokenNotLogged(t *testing.T) {
	s, buf := newStartLogService(t, map[string]string{})
	token, err := s.GenerateOAuthToken(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("GenerateOAuthToken: %v", err)
	}
	out := buf.String()
	assertNoProbe(t, out, token, "Использован fallback токен: flow=fallback", tokenFingerprint(token))
	if strings.Contains(out, "user-1") {
		t.Fatalf("журнал содержит user_id рядом с fallback токеном:\n%s", out)
	}
}

// Fallback-токен не выводится из user_id и времени: случайный, без user_id,
// пригоден как start-параметр Telegram и не похож на login_/link_.
func TestFallbackTokenRandom(t *testing.T) {
	s, _ := newStartLogService(t, map[string]string{})
	ctx := context.Background()
	a, err := s.GenerateOAuthToken(ctx, "user-1")
	if err != nil {
		t.Fatalf("GenerateOAuthToken: %v", err)
	}
	b, err := s.GenerateOAuthToken(ctx, "user-1")
	if err != nil {
		t.Fatalf("GenerateOAuthToken: %v", err)
	}
	if a == b {
		t.Fatalf("два вызова дали один токен %q", a)
	}
	for _, tok := range []string{a, b} {
		if strings.Contains(tok, "user-1") || strings.Contains(tok, ":") {
			t.Fatalf("токен выводится из user_id: %q", tok)
		}
		if len(tok) < 32 {
			t.Fatalf("токен короче 16 байт: %q", tok)
		}
	}
}

func TestTokenFingerprint(t *testing.T) {
	got := tokenFingerprint("abc")
	// sha256("abc") = ba7816bf…
	if got != "len=3 fp=ba7816bf" {
		t.Fatalf("tokenFingerprint(abc) = %q", got)
	}
}
