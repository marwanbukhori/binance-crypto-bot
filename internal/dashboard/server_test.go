package dashboard

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIndexRendersWithToken(t *testing.T) {
	s, _, _ := newServer(t)
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "dash_token", Value: "secret"})
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("index status %d", rr.Code)
	}
	body := rr.Body.String()
	for _, want := range []string{"<html", "Equity", "Kill", "/api/stats", "/api/equity"} {
		if !strings.Contains(body, want) {
			t.Fatalf("index missing %q", want)
		}
	}
}

func TestIndexQueryTokenRedirectsAndSetsCookie(t *testing.T) {
	s, _, _ := newServer(t)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest("GET", "/?token=secret", nil))
	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", rr.Code)
	}
	setCookie := rr.Header().Get("Set-Cookie")
	if !strings.Contains(setCookie, "dash_token") {
		t.Fatalf("expected Set-Cookie with dash_token, got: %s", setCookie)
	}
}

func TestKillRejectsGet(t *testing.T) {
	s, _, _ := newServer(t)
	req := httptest.NewRequest("GET", "/api/kill", nil)
	req.AddCookie(&http.Cookie{Name: "dash_token", Value: "secret"})
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET /api/kill must be 405, got %d", rr.Code)
	}
}

func TestIndexUnauthorized(t *testing.T) {
	s, _, _ := newServer(t)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest("GET", "/", nil))
	if rr.Code != 401 {
		t.Fatalf("index without token must be 401, got %d", rr.Code)
	}
}
