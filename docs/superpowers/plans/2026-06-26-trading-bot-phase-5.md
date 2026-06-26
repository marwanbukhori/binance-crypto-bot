# Trading Bot Phase 5 (Embedded Web Dashboard) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`) syntax.

**Goal:** A web dashboard served by the Go binary itself: equity curve + P&L, bot status, a closed-trades ledger with after-fee performance stats, a decisions/signals feed, and kill/pause controls — behind token auth, auto-refreshing.

**Architecture:** The engine records closed round-trip trades (with strategy attribution and net P&L) into SQLite. A `dashboard` package serves a token-authenticated HTTP server with JSON API endpoints (reading the store) and server-rendered HTML pages that fetch those endpoints and render an inline-SVG equity chart. Control endpoints (kill/pause/resume) drive the existing `control.Controller`. All store/controller access from HTTP handlers is read-only or controller-mediated — no direct portfolio access (consistent with Phase 4's race-free rule).

**Tech Stack:** Go 1.22 stdlib `net/http` + `html/template`. No frontend framework, no new third-party deps. Reuses all prior packages.

## Global Constraints

- **No direct portfolio access from HTTP handlers.** Read trades/signals/equity from the SQLite store; read live status from `control.Controller.Status()`; mutate only via the controller (kill/pause). Same race-free rule as Phase 4.
- **Auth:** every route requires a token (`DASHBOARD_TOKEN` env, via config) — header `Authorization: Bearer <t>` OR `?token=<t>` query (so a browser link works). Missing/empty configured token → dashboard disabled (don't serve).
- **After-fee honesty:** performance stats and the trades ledger always show NET P&L (fees included); never a gross figure unlabeled.
- **Module path:** `tradebot`. TDD + one commit per task. No Claude attribution in commits.

---

## File Structure

```
internal/store/store.go            + trades table, RecordTrade, ListTrades, ListSignals, EquitySeries, PerfStats
internal/decision/decision.go      + Candidate.Name + Pick() returning the chosen candidate
internal/engine/live.go            record a closed trade (strategy, net, reason) on every position close
internal/dashboard/api.go          JSON API handlers + token auth middleware
internal/dashboard/server.go       routes, HTML pages (html/template), inline-SVG equity chart
internal/dashboard/server_test.go  httptest coverage
cmd/bot/main.go                    start the dashboard in runPaper when enabled
```

---

## Task 1: Store — trades + dashboard queries

**Files:** Modify `internal/store/store.go`, `internal/store/store_test.go`

**Interfaces:**
- Produces:
  - `type Trade struct { TS int64; Symbol, Strategy, Reason string; EntryPx, ExitPx, Qty, NetPnL float64 }`
  - `func (s *Store) RecordTrade(t Trade) error`
  - `func (s *Store) ListTrades(limit int) ([]Trade, error)` (newest first)
  - `type SignalRow struct { TS int64; Symbol, Strategy, Action, Reason string }`, `func (s *Store) ListSignals(limit int) ([]SignalRow, error)`
  - `type EquityPoint struct { TS int64; Equity float64 }`, `func (s *Store) EquitySeries(limit int) ([]EquityPoint, error)` (ascending)
  - `type Stats struct { Trades, Wins int; WinRate, NetPnL, Expectancy, Fees float64 }`, `func (s *Store) PerfStats() (Stats, error)` — computed from the `trades` table (NetPnL already net of fees; `Fees` summed from the `fills` table).

- [ ] **Step 1: Failing test** — append to `internal/store/store_test.go`:
```go
func TestTradesAndStats(t *testing.T) {
	s, _ := Open(":memory:")
	defer s.Close()
	s.RecordTrade(Trade{TS: 1, Symbol: "BTCUSDT", Strategy: "ema_cross_trend", Reason: "TP", EntryPx: 100, ExitPx: 104, Qty: 1, NetPnL: 3.5})
	s.RecordTrade(Trade{TS: 2, Symbol: "BTCUSDT", Strategy: "ema_cross_trend", Reason: "SL", EntryPx: 104, ExitPx: 102, Qty: 1, NetPnL: -2.2})
	ts, err := s.ListTrades(10)
	if err != nil || len(ts) != 2 { t.Fatalf("ListTrades=%d err=%v", len(ts), err) }
	if ts[0].TS != 2 { t.Fatalf("newest first expected, got TS=%d", ts[0].TS) }
	st, err := s.PerfStats()
	if err != nil { t.Fatalf("stats: %v", err) }
	if st.Trades != 2 || st.Wins != 1 { t.Fatalf("bad counts: %+v", st) }
	if st.WinRate < 0.49 || st.WinRate > 0.51 { t.Fatalf("winRate=%v want 0.5", st.WinRate) }
	if st.NetPnL < 1.29 || st.NetPnL > 1.31 { t.Fatalf("netPnL=%v want 1.3", st.NetPnL) }
}

func TestEquitySeriesAscending(t *testing.T) {
	s, _ := Open(":memory:")
	defer s.Close()
	s.RecordPnLSnapshot(2, 1010, 10)
	s.RecordPnLSnapshot(1, 1000, 0)
	eq, err := s.EquitySeries(10)
	if err != nil || len(eq) != 2 { t.Fatalf("equity=%d err=%v", len(eq), err) }
	if eq[0].TS != 1 || eq[1].TS != 2 { t.Fatalf("must be ascending: %+v", eq) }
}
```

- [ ] **Step 2: Run** `go test ./internal/store/ -run 'Trades|Equity'` → FAIL.

- [ ] **Step 3: Implement** — append the `trades` table to the schema string:
```sql
CREATE TABLE IF NOT EXISTS trades (
  id INTEGER PRIMARY KEY AUTOINCREMENT, ts INTEGER, symbol TEXT, strategy TEXT, reason TEXT,
  entry_px REAL, exit_px REAL, qty REAL, net_pnl REAL
);
```
Add the types + methods:
```go
type Trade struct {
	TS                       int64
	Symbol, Strategy, Reason string
	EntryPx, ExitPx, Qty, NetPnL float64
}

func (s *Store) RecordTrade(t Trade) error {
	_, err := s.db.Exec(`INSERT INTO trades(ts,symbol,strategy,reason,entry_px,exit_px,qty,net_pnl)
		VALUES(?,?,?,?,?,?,?,?)`, t.TS, t.Symbol, t.Strategy, t.Reason, t.EntryPx, t.ExitPx, t.Qty, t.NetPnL)
	return err
}

func (s *Store) ListTrades(limit int) ([]Trade, error) {
	rows, err := s.db.Query(`SELECT ts,symbol,strategy,reason,entry_px,exit_px,qty,net_pnl FROM trades ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Trade
	for rows.Next() {
		var t Trade
		if err := rows.Scan(&t.TS, &t.Symbol, &t.Strategy, &t.Reason, &t.EntryPx, &t.ExitPx, &t.Qty, &t.NetPnL); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

type SignalRow struct {
	TS                              int64
	Symbol, Strategy, Action, Reason string
}

func (s *Store) ListSignals(limit int) ([]SignalRow, error) {
	rows, err := s.db.Query(`SELECT ts,symbol,strategy,action,reason FROM signals ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SignalRow
	for rows.Next() {
		var r SignalRow
		if err := rows.Scan(&r.TS, &r.Symbol, &r.Strategy, &r.Action, &r.Reason); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

type EquityPoint struct {
	TS     int64
	Equity float64
}

func (s *Store) EquitySeries(limit int) ([]EquityPoint, error) {
	rows, err := s.db.Query(`SELECT ts,equity FROM (SELECT id,ts,equity FROM pnl_snapshots ORDER BY id DESC LIMIT ?) ORDER BY id ASC`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EquityPoint
	for rows.Next() {
		var p EquityPoint
		if err := rows.Scan(&p.TS, &p.Equity); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

type Stats struct {
	Trades, Wins                       int
	WinRate, NetPnL, Expectancy, Fees float64
}

func (s *Store) PerfStats() (Stats, error) {
	var st Stats
	err := s.db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(CASE WHEN net_pnl>0 THEN 1 ELSE 0 END),0), COALESCE(SUM(net_pnl),0) FROM trades`).
		Scan(&st.Trades, &st.Wins, &st.NetPnL)
	if err != nil {
		return st, err
	}
	if st.Trades > 0 {
		st.WinRate = float64(st.Wins) / float64(st.Trades)
		st.Expectancy = st.NetPnL / float64(st.Trades)
	}
	_ = s.db.QueryRow(`SELECT COALESCE(SUM(fee),0) FROM fills`).Scan(&st.Fees)
	return st, nil
}
```

- [ ] **Step 4: Run** `go test ./internal/store/` → PASS.
- [ ] **Step 5: Commit** `git add internal/store && git commit -m "feat: record trades and add dashboard store queries"`

---

## Task 2: Decision attribution (Pick) + engine trade recording

**Files:** Modify `internal/decision/decision.go`, `internal/decision/decision_test.go`, `internal/engine/live.go`

**Interfaces:**
- `decision`: add `Name string` to `Candidate`; add `func Pick(r regime.Regime, inPosition bool, cands []Candidate) *Candidate` — same selection rule as `Choose` but returns the chosen `*Candidate` (so callers learn the strategy name). `Choose` stays unchanged.
- `engine.Live`: track the opening strategy name per symbol (`openStrat map[string]string`), set when an entry executes; on every close (SL/TP/flatten/rule), compute net P&L (realized delta) and call `store.RecordTrade(...)` with the entry/exit price, qty, reason, and strategy. Build candidates with `Name: stg.Name()` and use `Pick` to choose.

- [ ] **Step 1: Failing test** — append to `internal/decision/decision_test.go`:
```go
func TestPickReturnsChosenCandidate(t *testing.T) {
	cands := []Candidate{
		{Kind: "trend", Name: "ema_cross_trend", Signal: domain.Signal{Action: domain.Buy, Reason: "t"}},
		{Kind: "reversion", Name: "rsi_bb_reversion", Signal: domain.Signal{Action: domain.Buy, Reason: "r"}},
	}
	got := Pick(regime.LowVolChop, false, cands)
	if got == nil || got.Name != "rsi_bb_reversion" {
		t.Fatalf("LowVolChop must pick reversion, got %+v", got)
	}
}
```
And in `internal/engine/live_test.go` append a check that a completed round-trip records a trade:
```go
func TestLiveRecordsTradeOnClose(t *testing.T) {
	l, _, st := newLive(t, true) // autonomous (helper from Phase 4 test)
	defer st.Close()
	feedUptrendThenFlat(l) // enters on the uptrend
	// drive a downtrend to force a rule/SL exit
	for i := 0; i < 60; i++ {
		p := 300 - float64(i)*5
		_ = l.OnCandle(domain.Candle{Symbol: "BTCUSDT", Close: p, High: p + 3, Low: p - 3, Closed: true, CloseTime: int64(1000+i) * 3600_000})
	}
	tr, err := st.ListTrades(10)
	if err != nil { t.Fatal(err) }
	if len(tr) < 1 { t.Fatalf("expected at least one recorded round-trip trade, got %d", len(tr)) }
	if tr[0].Strategy == "" { t.Fatal("trade must carry the opening strategy name") }
}
```

- [ ] **Step 2: Run** `go test ./internal/decision/ ./internal/engine/ -run 'Pick|RecordsTrade'` → FAIL.

- [ ] **Step 3: Implement.** In `decision.go` add `Name string` to `Candidate` and:
```go
// Pick is Choose but returns the chosen candidate (for strategy attribution).
func Pick(r regime.Regime, inPosition bool, cands []Candidate) *Candidate {
	if inPosition {
		for i := range cands {
			if cands[i].Signal.Action == domain.Sell {
				return &cands[i]
			}
		}
		return nil
	}
	for i := range cands {
		if cands[i].Signal.Action == domain.Buy && KindEnabled(r, cands[i].Kind) {
			return &cands[i]
		}
	}
	return nil
}
```
In `live.go`: add `openStrat map[string]string` and `openEntry map[string]float64` (or reuse `l.open[sym].Price` for entry price); build candidates with `Name: stg.Name()`; replace `decision.Choose` with `decision.Pick` (use `chosen.Signal` and `chosen.Name`). On entry, set `l.openStrat[sym] = chosen.Name`. Add a helper `recordClose(sym string, entry domain.Order, exitPx float64, ts int64, reason string, netBefore float64)` that computes `net := l.pf.Realized() - netBefore` and calls `l.store.RecordTrade(store.Trade{TS: ts, Symbol: sym, Strategy: l.openStrat[sym], Reason: reason, EntryPx: entry.Price, ExitPx: exitPx, Qty: entry.Qty, NetPnL: net})`. Call it at each close site (capture `netBefore := l.pf.Realized()` before `l.pf.Apply(exitFill)`).

> Keep `decision.Choose` as-is for `backtest.RunMulti`; only the live engine moves to `Pick`.

- [ ] **Step 4: Run** `go test ./...` → PASS.
- [ ] **Step 5: Commit** `git add internal/decision internal/engine && git commit -m "feat: attribute strategy and record round-trip trades in live engine"`

---

## Task 3: Dashboard JSON API + auth

**Files:** Create `internal/dashboard/api.go`, `internal/dashboard/api_test.go`

**Interfaces:**
- Consumes: `store.Store`, `control.Controller`.
- Produces:
  - `type Server struct {...}`, `func New(st *store.Store, ctrl *control.Controller, token string) *Server`
  - `func (s *Server) Handler() http.Handler` — a mux with: `GET /api/status`, `GET /api/trades`, `GET /api/signals`, `GET /api/equity`, `GET /api/stats`, `POST /api/kill`, `POST /api/pause`, `POST /api/resume`, and the HTML routes (Task 4). All wrapped in `authMiddleware` (Bearer header or `?token=`); a request with the wrong/no token → 401.

- [ ] **Step 1: Failing test** — `internal/dashboard/api_test.go`:
```go
package dashboard

import (
	"net/http"
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
```

- [ ] **Step 2: Run** `go test ./internal/dashboard/` → FAIL.

- [ ] **Step 3: Implement** — `internal/dashboard/api.go`:
```go
package dashboard

import (
	"encoding/json"
	"net/http"
	"strings"

	"tradebot/internal/control"
	"tradebot/internal/store"
)

type Server struct {
	store *store.Store
	ctrl  *control.Controller
	token string
}

func New(st *store.Store, ctrl *control.Controller, token string) *Server {
	return &Server{store: st, ctrl: ctrl, token: token}
}

func (s *Server) auth(r *http.Request) bool {
	if s.token == "" {
		return false
	}
	if h := r.Header.Get("Authorization"); strings.TrimPrefix(h, "Bearer ") == s.token {
		return true
	}
	return r.URL.Query().Get("token") == s.token
}

func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.auth(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/status", s.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"status": s.ctrl.Status(), "paused": s.ctrl.Paused()})
	}))
	mux.HandleFunc("/api/trades", s.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		ts, _ := s.store.ListTrades(200)
		writeJSON(w, ts)
	}))
	mux.HandleFunc("/api/signals", s.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		sg, _ := s.store.ListSignals(200)
		writeJSON(w, sg)
	}))
	mux.HandleFunc("/api/equity", s.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		eq, _ := s.store.EquitySeries(500)
		writeJSON(w, eq)
	}))
	mux.HandleFunc("/api/stats", s.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		st, _ := s.store.PerfStats()
		writeJSON(w, st)
	}))
	mux.HandleFunc("/api/kill", s.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		s.ctrl.RequestKill()
		writeJSON(w, map[string]string{"ok": "kill requested"})
	}))
	mux.HandleFunc("/api/pause", s.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		s.ctrl.Pause()
		writeJSON(w, map[string]string{"ok": "paused"})
	}))
	mux.HandleFunc("/api/resume", s.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		s.ctrl.Resume()
		writeJSON(w, map[string]string{"ok": "resumed"})
	}))
	mux.HandleFunc("/", s.authMiddleware(s.handleIndex)) // Task 4
	return mux
}
```

- [ ] **Step 4: Run** `go test ./internal/dashboard/` → FAIL (handleIndex undefined — added in Task 4). Temporarily add a stub `func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) }` at the end of api.go so this task compiles and its tests pass; Task 4 replaces it.

- [ ] **Step 5: Commit** `git add internal/dashboard && git commit -m "feat: add dashboard JSON API with token auth and controls"`

---

## Task 4: Dashboard HTML pages + equity chart

**Files:** Create `internal/dashboard/server.go`; remove the `handleIndex` stub from `api.go`; `internal/dashboard/server_test.go`

**Interfaces:**
- Produces: `func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request)` — renders a single-page dashboard (Go `html/template`) with: a status bar (from `/api/status`), KPI tiles (equity, net P&L, win rate from `/api/stats`), an inline-SVG equity sparkline (from `/api/equity`), a trades table (`/api/trades`), a signals/decisions table (`/api/signals`), and Kill / Pause / Resume buttons (POST to the control endpoints, passing the token). The page uses a small inline `<script>` that `fetch`es the JSON endpoints (with the token from the URL) every 5s and updates the DOM; the SVG polyline is built in JS from the equity points.

- [ ] **Step 1: Failing test** — `internal/dashboard/server_test.go`:
```go
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
```

- [ ] **Step 2: Run** `go test ./internal/dashboard/ -run Index` → FAIL.

- [ ] **Step 3: Implement** — delete the `handleIndex` stub in `api.go`, then create `internal/dashboard/server.go`:
```go
package dashboard

import (
	"html/template"
	"net/http"
)

var indexTmpl = template.Must(template.New("index").Parse(`<!DOCTYPE html>
<html lang="en"><head><meta charset="utf-8"><title>tradebot</title>
<style>
 body{font:14px/1.5 ui-sans-serif,system-ui,sans-serif;margin:0;background:#0b0e14;color:#d7dce5}
 header{display:flex;gap:16px;align-items:center;padding:12px 20px;background:#11151f;border-bottom:1px solid #1f2733}
 .pill{padding:2px 10px;border-radius:999px;background:#1b6e3a;color:#fff;font-weight:600}
 main{padding:20px;max-width:1100px;margin:0 auto}
 .tiles{display:grid;grid-template-columns:repeat(4,1fr);gap:12px;margin-bottom:18px}
 .tile{background:#11151f;border:1px solid #1f2733;border-radius:10px;padding:14px}
 .tile b{display:block;font-size:22px;margin-top:4px}
 table{width:100%;border-collapse:collapse;margin-top:8px;background:#11151f;border-radius:10px;overflow:hidden}
 th,td{padding:8px 10px;text-align:left;border-bottom:1px solid #1f2733;font-variant-numeric:tabular-nums}
 .pos{color:#3ad17a}.neg{color:# e0556b}
 button{background:#243044;color:#d7dce5;border:1px solid #33425c;border-radius:8px;padding:8px 14px;cursor:pointer}
 button.kill{background:#7a1f2b;border-color:#a3303f}
 svg{width:100%;height:120px;background:#11151f;border:1px solid #1f2733;border-radius:10px}
 h3{margin:18px 0 6px}
</style></head>
<body>
<header>
  <span class="pill" id="state">…</span>
  <span id="statusline" style="opacity:.85"></span>
  <span style="margin-left:auto;display:flex;gap:8px">
    <button onclick="ctl('pause')">Pause</button>
    <button onclick="ctl('resume')">Resume</button>
    <button class="kill" onclick="if(confirm('Trip the kill-switch?'))ctl('kill')">Kill</button>
  </span>
</header>
<main>
  <div class="tiles">
    <div class="tile">Equity<b id="equity">—</b></div>
    <div class="tile">Net P&amp;L (after fees)<b id="netpnl">—</b></div>
    <div class="tile">Win rate<b id="winrate">—</b></div>
    <div class="tile">Trades<b id="trades">—</b></div>
  </div>
  <h3>Equity</h3>
  <svg id="chart" viewBox="0 0 1000 120" preserveAspectRatio="none"><polyline id="curve" fill="none" stroke="#3ad17a" stroke-width="2"/></svg>
  <h3>Recent trades</h3>
  <table><thead><tr><th>time</th><th>symbol</th><th>strategy</th><th>reason</th><th>net</th></tr></thead><tbody id="tradeRows"></tbody></table>
  <h3>Recent decisions</h3>
  <table><thead><tr><th>time</th><th>symbol</th><th>strategy</th><th>action</th><th>reason</th></tr></thead><tbody id="sigRows"></tbody></table>
</main>
<script>
const TOK = new URLSearchParams(location.search).get('token') || '';
const q = p => fetch(p + (p.includes('?')?'&':'?') + 'token=' + encodeURIComponent(TOK)).then(r=>r.json());
const ctl = a => fetch('/api/'+a+'?token='+encodeURIComponent(TOK),{method:'POST'}).then(()=>refresh());
const fmt = n => (n>=0?'+':'') + Number(n).toFixed(2);
const t = ms => new Date(ms).toLocaleString();
async function refresh(){
  const [s,stat,eq,tr,sg] = await Promise.all([q('/api/status'),q('/api/stats'),q('/api/equity'),q('/api/trades'),q('/api/signals')]);
  document.getElementById('state').textContent = s.paused?'PAUSED':'RUNNING';
  document.getElementById('state').style.background = s.paused?'#7a5a1f':'#1b6e3a';
  document.getElementById('statusline').textContent = s.status||'';
  document.getElementById('netpnl').textContent = fmt(stat.NetPnL);
  document.getElementById('winrate').textContent = (stat.Trades?(100*stat.WinRate).toFixed(1):'0')+'%';
  document.getElementById('trades').textContent = stat.Trades||0;
  const pts = eq||[];
  document.getElementById('equity').textContent = pts.length?Number(pts[pts.length-1].Equity).toFixed(2):'—';
  if(pts.length>1){const xs=pts.map((_,i)=>i*1000/(pts.length-1));const ys=pts.map(p=>p.Equity);const lo=Math.min(...ys),hi=Math.max(...ys),rng=(hi-lo)||1;
    document.getElementById('curve').setAttribute('points', pts.map((p,i)=>xs[i]+','+(115-110*(p.Equity-lo)/rng)).join(' '));}
  document.getElementById('tradeRows').innerHTML = (tr||[]).map(x=>'<tr><td>'+t(x.TS)+'</td><td>'+x.Symbol+'</td><td>'+x.Strategy+'</td><td>'+x.Reason+'</td><td class="'+(x.NetPnL>=0?'pos':'neg')+'">'+fmt(x.NetPnL)+'</td></tr>').join('');
  document.getElementById('sigRows').innerHTML = (sg||[]).map(x=>'<tr><td>'+t(x.TS)+'</td><td>'+x.Symbol+'</td><td>'+x.Strategy+'</td><td>'+x.Action+'</td><td>'+x.Reason+'</td></tr>').join('');
}
refresh(); setInterval(refresh, 5000);
</script>
</body></html>`))

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = indexTmpl.Execute(w, nil)
}
```

- [ ] **Step 4: Run** `go test ./internal/dashboard/` → PASS. `go vet ./...` → clean.
- [ ] **Step 5: Commit** `git add internal/dashboard && git commit -m "feat: add dashboard HTML page with equity chart and controls"`

---

## Task 5: Config + main wiring

**Files:** Modify `internal/config/config.go`, `cmd/bot/main.go`

**Interfaces:**
- `config`: add `Dashboard struct { Enabled bool; Port int }` (`yaml:"dashboard"`) to `Config`.
- `cmd/bot/main.go runPaper`: when `cfg.Dashboard.Enabled && cfg.Secrets.DashboardToken != ""`, start `dashboard.New(st, ctrl, token).Handler()` on `cfg.Dashboard.Port` (default 8080) in a goroutine via `http.ListenAndServe`, logging the URL `http://localhost:<port>/?token=…`. The same `st` and `ctrl` already created for the engine/poller are reused.

> No new unit test (network bind / long-running). Verified by `go build`; the server logic is covered by Tasks 3–4.

- [ ] **Step 1: Implement** — add to `config.go`:
```go
	Dashboard struct {
		Enabled bool `yaml:"enabled"`
		Port    int  `yaml:"port"`
	} `yaml:"dashboard"`
```
Add to `runPaper` (after the controller/store exist, before `l.Run`):
```go
if cfg.Dashboard.Enabled && cfg.Secrets.DashboardToken != "" {
	port := cfg.Dashboard.Port
	if port == 0 {
		port = 8080
	}
	srv := dashboard.New(st, ctrl, cfg.Secrets.DashboardToken)
	go func() {
		addr := fmt.Sprintf(":%d", port)
		log.Printf("dashboard at http://localhost:%d/?token=%s", port, cfg.Secrets.DashboardToken)
		if err := http.ListenAndServe(addr, srv.Handler()); err != nil {
			log.Printf("dashboard server stopped: %v", err)
		}
	}()
}
```
Add imports `net/http`, `fmt` (if absent), and the `dashboard` package.

- [ ] **Step 2: Run** `go build ./cmd/bot && go test ./... && go vet ./...` → all pass.
- [ ] **Step 3: Commit** `git add internal/config cmd/bot && git commit -m "feat: serve the dashboard from paper-trading mode"`

---

## Self-Review

**Spec coverage (Phase 5):** trades ledger + after-fee stats + equity series store queries → T1 ✓; strategy attribution + live trade recording → T2 ✓; JSON API + token auth + control endpoints → T3 ✓; HTML dashboard (status, KPI tiles, equity SVG chart, trades + decisions tables, kill/pause/resume) → T4 ✓; config + main wiring → T5 ✓. The richer Strategy-Scorecard-by-regime and Monte-Carlo Projection-cone pages from spec §9 are intentionally deferred to Phase 6 (the learning layer produces per-regime scores and the bootstrap), noted here.

**Placeholder scan:** none — complete code/tests; the one stub (`handleIndex` in T3) is explicitly temporary and removed in T4.

**Type consistency:** `store.Trade`/`SignalRow`/`EquityPoint`/`Stats` + the new store methods, `decision.Candidate.Name` + `Pick`, `dashboard.Server`/`New`/`Handler`, and `config.Dashboard` are used consistently across tasks. Reuses `control.Controller` (Status/Paused/RequestKill/Pause/Resume) and `store.Store` unchanged otherwise.
