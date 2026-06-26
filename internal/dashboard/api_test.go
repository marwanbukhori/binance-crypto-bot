package dashboard

import (
	"net/http/httptest"
	"strings"
	"testing"

	"tradebot/internal/control"
	"tradebot/internal/store"
)

func newServer(t *testing.T) (*Server, *store.Store, *control.Controller) {
	st, _ := store.Open(":memory:")
	st.RecordTrade(store.Trade{TS: 1, Symbol: "BTCUSDT", Strategy: "ema_cross_trend", Reason: "TP", NetPnL: 5})
	ctrl := control.New()
	ctrl.SetStatus("equity 10005 | flat")
	return New(st, ctrl, "secret"), st, ctrl
}

func TestAuthRequired(t *testing.T) {
	s, _, _ := newServer(t)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest("GET", "/api/stats", nil))
	if rr.Code != 401 { t.Fatalf("missing token must be 401, got %d", rr.Code) }
}

func TestStatsWithToken(t *testing.T) {
	s, _, _ := newServer(t)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest("GET", "/api/stats?token=secret", nil))
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "\"Trades\":1") {
		t.Fatalf("stats failed: %d %s", rr.Code, rr.Body.String())
	}
}

func TestKillRequiresTokenAndTripsController(t *testing.T) {
	s, _, ctrl := newServer(t)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest("POST", "/api/kill?token=secret", nil))
	if rr.Code != 200 { t.Fatalf("kill status %d", rr.Code) }
	if !ctrl.KillRequested() { t.Fatal("POST /api/kill must trip the controller kill request") }
}
