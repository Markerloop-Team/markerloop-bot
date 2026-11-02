package routes

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	botmw "telegram-bot-service/internal/middleware"
)

const testServiceKey = "test-service-key"

// OAuth-токены и уведомления под X-Service-Key: без ключа и с чужим ключом —
// 401 до хендлера (nil-сервисы уронили бы тест паникой) и до дедупа: store
// пуст, и ни один ключ идемпотентности не должен в нём появиться.
func TestServiceRoutesRequireServiceKey(t *testing.T) {
	store := seededStore{keys: map[string]bool{}}
	r := newRoutedEngine(t, store)

	for _, path := range []string{
		"/api/v1/oauth/generate-token",
		"/api/v1/notifications/send",
		"/api/v1/notifications/broadcast",
		"/api/v1/notifications/deploy",
	} {
		for _, key := range []string{"", "wrong-key"} {
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
			req.Header.Set(botmw.IdempotencyHeader, "probe-key")
			if key != "" {
				req.Header.Set("X-Service-Key", key)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != http.StatusUnauthorized {
				t.Errorf("%s с ключом %q: ожидался 401, получен %d", path, key, w.Code)
			}
		}
	}
	if len(store.keys) != 0 {
		t.Errorf("запрос без ключа занял ключи идемпотентности: %v", store.keys)
	}
}
