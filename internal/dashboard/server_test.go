package dashboard

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIndexRendersWithToken(t *testing.T) {
	s, _, _ := newServer(t)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest("GET", "/?token=secret", nil))
	if rr.Code != 200 { t.Fatalf("index status %d", rr.Code) }
	body := rr.Body.String()
	for _, want := range []string{"<html", "Equity", "Kill", "/api/stats", "/api/equity"} {
		if !strings.Contains(body, want) {
			t.Fatalf("index missing %q", want)
		}
	}
}

func TestIndexUnauthorized(t *testing.T) {
	s, _, _ := newServer(t)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest("GET", "/", nil))
	if rr.Code != 401 { t.Fatalf("index without token must be 401, got %d", rr.Code) }
}
