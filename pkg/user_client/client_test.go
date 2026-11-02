package user_client

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sirupsen/logrus"

	"gitlab.com/markerloop/go-sdk/pkg/requestid"
)

// newTestClient поднимает фейковый user-service и возвращает клиент,
// направленный на него. Сервер записывает X-Request-Id каждого запроса.
func newTestClient(t *testing.T, status int, body string, gotHeader *string) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*gotHeader = r.Header.Get(requestid.Header)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	logger := logrus.New()
	logger.SetOutput(io.Discard)
	return NewClient(srv.URL, "test-service-key", logger), srv
}

func TestCreateUser_PropagatesRequestID(t *testing.T) {
	var gotHeader string
	client, _ := newTestClient(t, http.StatusCreated,
		`{"data":{"id":"u-1","username":"tg_42"},"message":"ok","success":true}`, &gotHeader)

	id := requestid.New()
	ctx := requestid.NewContext(context.Background(), id)

	user, err := client.CreateUser(ctx, &CreateUserRequest{Username: "tg_42", Password: "secret"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if user.ID != "u-1" {
		t.Fatalf("unexpected user id: %s", user.ID)
	}
	if gotHeader != id {
		t.Fatalf("user-service получил X-Request-Id=%q, ожидался %q", gotHeader, id)
	}
}

func TestGetUserByExternalAccount_PropagatesRequestID(t *testing.T) {
	var gotHeader string
	client, _ := newTestClient(t, http.StatusOK,
		`{"data":{"id":"u-2","username":"tg_43"},"message":"ok","success":true}`, &gotHeader)

	id := requestid.New()
	ctx := requestid.NewContext(context.Background(), id)

	user, err := client.GetUserByExternalAccount(ctx, "telegram", "43")
	if err != nil {
		t.Fatalf("GetUserByExternalAccount: %v", err)
	}
	if user.ID != "u-2" {
		t.Fatalf("unexpected user id: %s", user.ID)
	}
	if gotHeader != id {
		t.Fatalf("user-service получил X-Request-Id=%q, ожидался %q", gotHeader, id)
	}
}

func TestCreateUser_NoRequestIDInContext(t *testing.T) {
	// Без id в контексте заголовок не должен выставляться вовсе.
	var gotHeader string
	client, _ := newTestClient(t, http.StatusCreated,
		`{"data":{"id":"u-3","username":"tg_44"},"message":"ok","success":true}`, &gotHeader)

	if _, err := client.CreateUser(context.Background(), &CreateUserRequest{Username: "tg_44", Password: "secret"}); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if gotHeader != "" {
		t.Fatalf("ожидался пустой X-Request-Id, получен %q", gotHeader)
	}
}

// SDL-323: ручка поиска по внешней личности переехала в user-service под
// /internal (за ServiceToServiceAuth). Путь — строка, компилятор о промахе
// в ней не знает, а последствие тихое: бот получит 404/401 и решит, что
// пользователя с таким Telegram-id ещё нет, то есть заведёт дубликат
// вместо того, чтобы узнать существующего.
func TestGetUserByExternalAccount_CallsInternalPath(t *testing.T) {
	var gotPath, gotKey, gotMethod string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotKey, gotMethod = r.URL.Path, r.Header.Get("X-Service-Key"), r.Method
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":{"id":"u-9","username":"tg_9"},"message":"ok","success":true}`))
	}))
	t.Cleanup(srv.Close)

	logger := logrus.New()
	logger.SetOutput(io.Discard)
	client := NewClient(srv.URL, "test-service-key", logger)

	if _, err := client.GetUserByExternalAccount(context.Background(), "telegram", "9"); err != nil {
		t.Fatalf("GetUserByExternalAccount: %v", err)
	}

	if want := "/api/v1/internal/external-accounts/get-user"; gotPath != want {
		t.Errorf("путь %q, ожидался %q", gotPath, want)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("метод %q, ожидался POST", gotMethod)
	}
	if gotKey != "test-service-key" {
		t.Errorf("X-Service-Key %q, ожидался test-service-key", gotKey)
	}
}
