package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCORSDefaultPolicyKeepsRequestsSameOrigin(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := CORS(nil)(next)
	req := httptest.NewRequest(http.MethodGet, "http://community.local/api/notices", nil)
	req.Header.Set("Origin", "https://untrusted.example")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected application response, got %d", rec.Code)
	}
	if origin := rec.Header().Get("Access-Control-Allow-Origin"); origin != "" {
		t.Fatalf("default policy granted cross-origin access to %q", origin)
	}
	if credentials := rec.Header().Get("Access-Control-Allow-Credentials"); credentials != "" {
		t.Fatalf("default policy granted credentialed cross-origin access: %q", credentials)
	}
}
