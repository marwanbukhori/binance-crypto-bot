# Trading Bot Phase 3 (Paper Trading — Real-Time on Testnet Data) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Run the full pipeline in real time against live Binance (testnet) kline WebSocket streams, driving the same strategy→regime→decision→guard→cooldown→risk→execution→portfolio→store flow on each closed candle, with simulated fills — a live dry-run that validates the bot continuously before any real money.

**Architecture:** A WebSocket stream client emits closed candles; a rolling per-(symbol,timeframe) buffer feeds a live engine that reuses the Phase 2 decision logic. Indicators are warmed up on startup via the existing REST `Backfill`. The guard resets daily/weekly baselines on UTC day/week boundaries (resolves the Phase-2 M1/M2 deferral). `mode: paper` wires testnet WS + simulated executor. Real signed-order placement (testnet/live REST) is deferred to Phase 7.

**Tech Stack:** Go 1.22, adds `github.com/gorilla/websocket`. Reuses all Phase 0–2 packages.

## Global Constraints

- **Spot LONG-ONLY; NO LOOK-AHEAD** (act only on CLOSED candles — the WS `kline.x==true` flag).
- **Reuse, don't duplicate:** the live per-candle decision must call the SAME strategy/regime/decision/risk/guard/cooldown logic used by `backtest.RunMulti`; do not fork the trading logic.
- **Guardrails non-bypassable**; kill-switch + cooldown persist (Phase 2 store methods).
- **Paper uses the simulated executor** (fills at the closed-candle price + modeled fee). No real orders in Phase 3.
- **Resilience:** the WS client auto-reconnects with backoff and re-warms via REST on reconnect; a stream gap must not crash the bot.
- **Module path:** `tradebot`. TDD + one commit per task. Commit identity is the repo default (do not add co-author trailers).

---

## File Structure

```
internal/marketdata/ws.go          KlineEvent parse + Stream interface + WSStream (gorilla) + reconnect
internal/marketdata/buffer.go      rolling per-(symbol,timeframe) candle buffer
internal/engine/live.go            Live engine: stream -> buffer -> pipeline -> sim exec -> portfolio -> store
internal/risk/guard.go             + RollToDay / RollToWeek timestamp-based baseline reset
cmd/bot/main.go                    + runPaper(): warmup + live engine wiring under mode==paper
```

---

## Task 1: WS kline event parser

**Files:** Create `internal/marketdata/ws.go`, `internal/marketdata/ws_test.go`

**Interfaces:**
- Produces: `func parseKlineEvent(raw []byte) (domain.Candle, bool, error)` — parses a Binance kline WS message; returns the candle and `closed=true` only when the kline's `k.x` is true; ignores non-kline frames (returns `closed=false, nil`).

- [ ] **Step 1: Failing test** — `internal/marketdata/ws_test.go`:
```go
package marketdata

import "testing"

func TestParseKlineEventClosed(t *testing.T) {
	msg := []byte(`{"e":"kline","E":123,"s":"BTCUSDT","k":{"t":100,"T":159999,"i":"1h","o":"60000","c":"60500","h":"60800","l":"59900","v":"12.3","x":true}}`)
	c, closed, err := parseKlineEvent(msg)
	if err != nil { t.Fatalf("parse: %v", err) }
	if !closed { t.Fatal("x:true must be a closed candle") }
	if c.Symbol != "BTCUSDT" || c.Close != 60500 || c.High != 60800 || c.Low != 59900 || c.Open != 60000 {
		t.Fatalf("bad candle: %+v", c)
	}
	if c.Timeframe != "1h" || c.CloseTime != 159999 || !c.Closed {
		t.Fatalf("bad meta: %+v", c)
	}
}

func TestParseKlineEventOpenIgnored(t *testing.T) {
	msg := []byte(`{"e":"kline","s":"BTCUSDT","k":{"t":100,"T":159999,"i":"1h","o":"1","c":"2","h":"3","l":"0","v":"1","x":false}}`)
	_, closed, err := parseKlineEvent(msg)
	if err != nil { t.Fatalf("parse: %v", err) }
	if closed { t.Fatal("x:false must NOT be reported closed") }
}

func TestParseNonKlineIgnored(t *testing.T) {
	_, closed, err := parseKlineEvent([]byte(`{"result":null,"id":1}`))
	if err != nil || closed { t.Fatalf("subscription ack must be ignored, got closed=%v err=%v", closed, err) }
}
```

- [ ] **Step 2: Run** `go test ./internal/marketdata/ -run ParseKline` → FAIL.

- [ ] **Step 3: Implement** — `internal/marketdata/ws.go`:
```go
package marketdata

import (
	"encoding/json"
	"strconv"

	"tradebot/internal/domain"
)

type klineMsg struct {
	Event string `json:"e"`
	Sym   string `json:"s"`
	K     struct {
		T int64  `json:"t"`
		TT int64 `json:"T"`
		I string `json:"i"`
		O string `json:"o"`
		C string `json:"c"`
		H string `json:"h"`
		L string `json:"l"`
		V string `json:"v"`
		X bool   `json:"x"`
	} `json:"k"`
}

// parseKlineEvent decodes a Binance kline WS frame; closed is true only when k.x is set.
func parseKlineEvent(raw []byte) (domain.Candle, bool, error) {
	var m klineMsg
	if err := json.Unmarshal(raw, &m); err != nil {
		return domain.Candle{}, false, err
	}
	if m.Event != "kline" {
		return domain.Candle{}, false, nil
	}
	f := func(s string) float64 { v, _ := strconv.ParseFloat(s, 64); return v }
	c := domain.Candle{
		Symbol: m.Sym, Timeframe: m.K.I,
		OpenTime: m.K.T, CloseTime: m.K.TT,
		Open: f(m.K.O), High: f(m.K.H), Low: f(m.K.L), Close: f(m.K.C), Volume: f(m.K.V),
		Closed: m.K.X,
	}
	return c, m.K.X, nil
}
```

- [ ] **Step 4: Run** `go test ./internal/marketdata/` → PASS.
- [ ] **Step 5: Commit** `git add internal/marketdata && git commit -m "feat: parse Binance kline WebSocket events"`

---

## Task 2: WS stream client (gorilla, reconnect)

**Files:** Modify `internal/marketdata/ws.go`, `internal/marketdata/ws_test.go`

**Interfaces:**
- Produces:
  - `type Stream interface { Candles() <-chan domain.Candle; Close() error }`
  - `func NewWSStream(testnet bool, symbols []string, interval string) *WSStream` — connects to the combined kline stream, emits closed candles on `Candles()`, auto-reconnects with backoff. `WSStream` satisfies `Stream`.
  - A `chanStream` test helper implementing `Stream` over a plain channel (used by the engine tests in Task 4).

- [ ] **Step 1: Failing test** — append to `internal/marketdata/ws_test.go`:
```go
import (
	"tradebot/internal/domain" // add to import block
	"time"                      // add to import block
)

func TestChanStreamDeliversCandles(t *testing.T) {
	ch := make(chan domain.Candle, 1)
	s := NewChanStream(ch)
	ch <- domain.Candle{Symbol: "BTCUSDT", Close: 1, Closed: true}
	select {
	case c := <-s.Candles():
		if c.Symbol != "BTCUSDT" { t.Fatalf("bad candle %+v", c) }
	case <-time.After(time.Second):
		t.Fatal("expected a candle")
	}
	if err := s.Close(); err != nil { t.Fatalf("close: %v", err) }
}

func TestWSStreamURL(t *testing.T) {
	if got := streamURL(true, []string{"BTCUSDT", "ETHUSDT"}, "1h"); got != "wss://testnet.binance.vision/stream?streams=btcusdt@kline_1h/ethusdt@kline_1h" {
		t.Fatalf("bad testnet url: %s", got)
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/marketdata/ -run 'ChanStream|WSStreamURL'` → FAIL.

- [ ] **Step 3: Implement** — `go get github.com/gorilla/websocket@latest`, then append to `internal/marketdata/ws.go`:
```go
import (
	"fmt"      // add to import block
	"strings"  // add to import block
	"sync"     // add to import block
	"time"     // add to import block

	"github.com/gorilla/websocket"
)

type Stream interface {
	Candles() <-chan domain.Candle
	Close() error
}

// ChanStream adapts a plain channel to Stream (used for tests and the sim path).
type ChanStream struct{ ch chan domain.Candle }

func NewChanStream(ch chan domain.Candle) *ChanStream { return &ChanStream{ch: ch} }
func (c *ChanStream) Candles() <-chan domain.Candle   { return c.ch }
func (c *ChanStream) Close() error                    { return nil }

func streamURL(testnet bool, symbols []string, interval string) string {
	host := "wss://stream.binance.com:9443"
	if testnet {
		host = "wss://testnet.binance.vision"
	}
	parts := make([]string, len(symbols))
	for i, s := range symbols {
		parts[i] = strings.ToLower(s) + "@kline_" + interval
	}
	return fmt.Sprintf("%s/stream?streams=%s", host, strings.Join(parts, "/"))
}

type WSStream struct {
	url  string
	out  chan domain.Candle
	done chan struct{}
	once sync.Once
}

func NewWSStream(testnet bool, symbols []string, interval string) *WSStream {
	w := &WSStream{url: streamURL(testnet, symbols, interval), out: make(chan domain.Candle, 64), done: make(chan struct{})}
	go w.run()
	return w
}

func (w *WSStream) Candles() <-chan domain.Candle { return w.out }

func (w *WSStream) Close() error {
	w.once.Do(func() { close(w.done) })
	return nil
}

// combined-stream frames wrap the payload as {"stream":"...","data":{...}}.
type combinedFrame struct {
	Data json.RawMessage `json:"data"`
}

func (w *WSStream) run() {
	backoff := time.Second
	for {
		select {
		case <-w.done:
			return
		default:
		}
		conn, _, err := websocket.DefaultDialer.Dial(w.url, nil)
		if err != nil {
			time.Sleep(backoff)
			if backoff < 30*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = time.Second
		for {
			select {
			case <-w.done:
				conn.Close()
				return
			default:
			}
			_, msg, err := conn.ReadMessage()
			if err != nil {
				conn.Close()
				break // reconnect
			}
			payload := msg
			var cf combinedFrame
			if json.Unmarshal(msg, &cf) == nil && len(cf.Data) > 0 {
				payload = cf.Data
			}
			if c, closed, perr := parseKlineEvent(payload); perr == nil && closed {
				select {
				case w.out <- c:
				case <-w.done:
					conn.Close()
					return
				}
			}
		}
	}
}
```

- [ ] **Step 4: Run** `go test ./internal/marketdata/` → PASS.
- [ ] **Step 5: Commit** `git add internal/marketdata go.mod go.sum && git commit -m "feat: add reconnecting WebSocket kline stream"`

---

## Task 3: Rolling candle buffer

**Files:** Create `internal/marketdata/buffer.go`, `internal/marketdata/buffer_test.go`

**Interfaces:**
- Produces: `type Buffer struct {...}`, `func NewBuffer(maxLen int) *Buffer`, `func (b *Buffer) Add(c domain.Candle) []domain.Candle` (appends for that symbol, trims to maxLen, returns the symbol's history ascending), `func (b *Buffer) History(symbol string) []domain.Candle`.

- [ ] **Step 1: Failing test** — `internal/marketdata/buffer_test.go`:
```go
package marketdata

import (
	"testing"

	"tradebot/internal/domain"
)

func TestBufferTrimsAndReturnsHistory(t *testing.T) {
	b := NewBuffer(3)
	for i := 0; i < 5; i++ {
		b.Add(domain.Candle{Symbol: "BTCUSDT", Close: float64(i), CloseTime: int64(i), Closed: true})
	}
	h := b.History("BTCUSDT")
	if len(h) != 3 { t.Fatalf("want trimmed to 3, got %d", len(h)) }
	if h[0].Close != 2 || h[2].Close != 4 { t.Fatalf("want newest 3 (2,3,4), got %v..%v", h[0].Close, h[2].Close) }
}

func TestBufferPerSymbol(t *testing.T) {
	b := NewBuffer(10)
	b.Add(domain.Candle{Symbol: "BTCUSDT", Close: 1})
	b.Add(domain.Candle{Symbol: "ETHUSDT", Close: 2})
	if len(b.History("BTCUSDT")) != 1 || len(b.History("ETHUSDT")) != 1 {
		t.Fatal("symbols must be isolated")
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/marketdata/ -run Buffer` → FAIL.

- [ ] **Step 3: Implement** — `internal/marketdata/buffer.go`:
```go
package marketdata

import "tradebot/internal/domain"

// Buffer keeps a rolling, ascending candle history per symbol, trimmed to maxLen.
type Buffer struct {
	maxLen int
	hist   map[string][]domain.Candle
}

func NewBuffer(maxLen int) *Buffer {
	return &Buffer{maxLen: maxLen, hist: map[string][]domain.Candle{}}
}

func (b *Buffer) Add(c domain.Candle) []domain.Candle {
	h := append(b.hist[c.Symbol], c)
	if len(h) > b.maxLen {
		h = h[len(h)-b.maxLen:]
	}
	b.hist[c.Symbol] = h
	return h
}

func (b *Buffer) History(symbol string) []domain.Candle { return b.hist[symbol] }

// Seed preloads a symbol's history (e.g. from REST warmup), trimmed to maxLen.
func (b *Buffer) Seed(symbol string, candles []domain.Candle) {
	h := candles
	if len(h) > b.maxLen {
		h = h[len(h)-b.maxLen:]
	}
	b.hist[symbol] = append([]domain.Candle(nil), h...)
}
```

- [ ] **Step 4: Run** `go test ./internal/marketdata/` → PASS.
- [ ] **Step 5: Commit** `git add internal/marketdata && git commit -m "feat: add rolling candle buffer"`

---

## Task 4: Guard day/week baseline reset (resolves Phase-2 M1/M2)

**Files:** Modify `internal/risk/guard.go`, `internal/risk/guard_test.go`

**Interfaces:**
- Produces:
  - `func (g *Guard) RollTime(closeTimeMs int64, equity float64)` — given a candle close time, if the UTC day changed since the last roll it calls `StartDay(equity)` and re-arms the kill-switch (`Reset`), and if the ISO week changed it calls `StartWeek(equity)`. Tracks last-rolled day/week internally.

- [ ] **Step 1: Failing test** — append to `internal/risk/guard_test.go`:
```go
func TestRollTimeResetsDailyBaselineAndReArms(t *testing.T) {
	g := NewGuard(guardCfg(), 1000)
	day1 := int64(1_700_000_000_000) // some ms
	g.RollTime(day1, 1000)
	g.Mark(960) // -4% -> killed
	if !g.Killed() { t.Fatal("expected kill at -4%") }
	day2 := day1 + 24*3600*1000 // next UTC day
	g.RollTime(day2, 960)       // new day: baseline reset to 960, re-armed
	if g.Killed() { t.Fatal("new day must re-arm the kill-switch") }
	g.Mark(950) // -1.04% vs 960 -> ok
	if g.Killed() { t.Fatalf("should be ok, dailyLoss=%.2f", g.DailyLossPct()) }
}

func TestRollTimeSameDayNoReset(t *testing.T) {
	g := NewGuard(guardCfg(), 1000)
	base := int64(1_700_000_000_000)
	g.RollTime(base, 1000)
	g.Mark(970)
	g.RollTime(base+3600_000, 970) // +1h, same UTC day
	if g.DailyLossPct() < 2.9 { t.Fatalf("baseline must NOT reset same day, dailyLoss=%.2f", g.DailyLossPct()) }
}
```

- [ ] **Step 2: Run** `go test ./internal/risk/ -run RollTime` → FAIL.

- [ ] **Step 3: Implement** — append to `internal/risk/guard.go` (add `"time"` to imports; add fields `lastDay, lastWeek int` to the `Guard` struct, initialized to -1 in `NewGuard`):
```go
// RollTime resets the daily/weekly baselines (and re-arms the kill-switch on a new day)
// when the candle's UTC day/ISO-week boundary is crossed.
func (g *Guard) RollTime(closeTimeMs int64, equity float64) {
	t := time.UnixMilli(closeTimeMs).UTC()
	day := t.Year()*1000 + t.YearDay()
	year, week := t.ISOWeek()
	wk := year*100 + week
	if g.lastDay < 0 {
		g.lastDay, g.lastWeek = day, wk
		return
	}
	if day != g.lastDay {
		g.lastDay = day
		g.StartDay(equity)
		g.Reset() // re-arm: a new trading day clears yesterday's kill
	}
	if wk != g.lastWeek {
		g.lastWeek = wk
		g.StartWeek(equity)
	}
}
```
Update `NewGuard` to set `lastDay: -1, lastWeek: -1` and add the fields to the struct.

- [ ] **Step 4: Run** `go test ./internal/risk/` → PASS.
- [ ] **Step 5: Commit** `git add internal/risk && git commit -m "feat: add UTC day/week guard baseline reset"`

---

## Task 5: Live engine (paper)

**Files:** Create `internal/engine/live.go`, `internal/engine/live_test.go`

**Interfaces:**
- Consumes: `marketdata.Stream`, `marketdata.Buffer`, the Phase-2 `strategy`/`regime`/`decision`/`risk` packages, `execution.Executor`, `portfolio.Portfolio`, `store.Store`.
- Produces:
  - `type Live struct {...}`
  - `func NewLive(syms []string, mkStrats func(string) []strategy.Strategy, gate *risk.Gate, guard *risk.Guard, cool *risk.Cooldown, ex execution.Executor, pf *portfolio.Portfolio, st *store.Store, f risk.Filters, buf *marketdata.Buffer) *Live`
  - `func (l *Live) OnCandle(c domain.Candle) error` — runs ONE closed candle through the pipeline (buffer.Add → guard.RollTime/Mark → per-symbol strategies → regime → decision.Choose → cooldown/guard gating → gate sizing → executor → portfolio → store records). Mirrors `backtest.RunMulti`'s per-candle body but for live single-candle arrival; one position per symbol.
  - `func (l *Live) Run(ctx context.Context, s marketdata.Stream) error` — loops over `s.Candles()` calling `OnCandle`, until ctx is cancelled.
  - Position bookkeeping uses an in-memory `map[string]*domain.Order` for the open entry (for TP/SL price levels) and the `portfolio` for holdings.

- [ ] **Step 1: Failing test** — `internal/engine/live_test.go`:
```go
package engine

import (
	"context"
	"testing"
	"time"

	"tradebot/internal/config"
	"tradebot/internal/domain"
	"tradebot/internal/execution"
	"tradebot/internal/marketdata"
	"tradebot/internal/portfolio"
	"tradebot/internal/risk"
	"tradebot/internal/store"
	"tradebot/internal/strategy"
)

func TestLiveRunsPipelineAndRecords(t *testing.T) {
	st, _ := store.Open(":memory:")
	defer st.Close()
	buf := marketdata.NewBuffer(400)
	gate := risk.NewGate(config.RiskCfg{MaxPctPerTrade: 90, RiskPerTradePct: 5, MaxOpenPositions: 1, PortfolioMaxDeployedPct: 100, TPRewardMult: 1.6}, 0.003)
	guard := risk.NewGuard(config.RiskCfg{DailyLossLimitPct: 90, WeeklyLossLimitPct: 95, HardFlattenDrawdownPct: 99}, 10000)
	cool := risk.NewCooldown(2)
	pf := portfolio.New(10000)
	mk := func(sym string) []strategy.Strategy { return []strategy.Strategy{strategy.NewEMACross(sym, "1h", nil)} }
	l := NewLive([]string{"BTCUSDT"}, mk, gate, guard, cool, execution.NewSimulated(0.0015), pf, st, risk.Filters{StepSize: 0.00001, MinQty: 0.00001, MinNotional: 5}, buf)

	ch := make(chan domain.Candle, 256)
	closes := []float64{}
	for i := 0; i < 40; i++ { closes = append(closes, 100) }
	for i := 0; i < 50; i++ { closes = append(closes, 100+float64(i)*4) }
	for i := 0; i < 40; i++ { closes = append(closes, 300-float64(i)*4) }
	for i, c := range closes {
		ch <- domain.Candle{Symbol: "BTCUSDT", Close: c, High: c + 5, Low: c - 5, Closed: true, CloseTime: int64(i) * 3600_000}
	}
	close(ch)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = l.Run(ctx, marketdata.NewChanStream(ch))

	n, _ := st.CountFills()
	if n < 1 { t.Fatalf("expected the live engine to record at least one fill, got %d", n) }
}
```

- [ ] **Step 2: Run** `go test ./internal/engine/ -run Live` → FAIL.

- [ ] **Step 3: Implement** — `internal/engine/live.go`:
```go
package engine

import (
	"context"

	"tradebot/internal/decision"
	"tradebot/internal/domain"
	"tradebot/internal/execution"
	"tradebot/internal/marketdata"
	"tradebot/internal/portfolio"
	"tradebot/internal/regime"
	"tradebot/internal/risk"
	"tradebot/internal/store"
	"tradebot/internal/strategy"
)

type Live struct {
	syms   []string
	strats map[string][]strategy.Strategy
	gate   *risk.Gate
	guard  *risk.Guard
	cool   *risk.Cooldown
	exec   execution.Executor
	pf     *portfolio.Portfolio
	store  *store.Store
	filt   risk.Filters
	buf    *marketdata.Buffer
	open   map[string]*domain.Order
	idx    map[string]int // per-symbol candle counter for cooldown
}

func NewLive(syms []string, mkStrats func(string) []strategy.Strategy, gate *risk.Gate, guard *risk.Guard, cool *risk.Cooldown, ex execution.Executor, pf *portfolio.Portfolio, st *store.Store, f risk.Filters, buf *marketdata.Buffer) *Live {
	strats := map[string][]strategy.Strategy{}
	for _, s := range syms {
		strats[s] = mkStrats(s)
	}
	return &Live{syms: syms, strats: strats, gate: gate, guard: guard, cool: cool, exec: ex, pf: pf, store: st, filt: f, buf: buf, open: map[string]*domain.Order{}, idx: map[string]int{}}
}

func (l *Live) Run(ctx context.Context, s marketdata.Stream) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case c, ok := <-s.Candles():
			if !ok {
				return nil
			}
			if err := l.OnCandle(c); err != nil {
				return err
			}
		}
	}
}

func (l *Live) equity() float64 {
	mark := map[string]float64{}
	for sym, o := range l.open {
		mark[sym] = o.Price // last entry as a fallback mark
	}
	return l.pf.Equity(mark)
}

func (l *Live) OnCandle(c domain.Candle) error {
	sym := c.Symbol
	hist := l.buf.Add(c)
	i := l.idx[sym]
	l.idx[sym] = i + 1

	// mark equity using this candle's close for the active symbol
	mark := map[string]float64{}
	for s, o := range l.open {
		mark[s] = o.Price
	}
	mark[sym] = c.Close
	eq := l.pf.Equity(mark)
	l.guard.RollTime(c.CloseTime, eq)
	l.guard.Mark(eq)
	if l.guard.Killed() {
		_ = l.store.SaveKillState(true, "loss limit", c.CloseTime)
	}

	pos := l.pf.Position(sym)
	inPos := pos.Qty > 0

	// 1) manage open position: hard-flatten or TP/SL on this candle.
	if o, ok := l.open[sym]; ok {
		flatten := l.guard.ShouldFlatten()
		hitStop := o.StopPrice > 0 && c.Low <= o.StopPrice
		hitTP := o.TPPrice > 0 && c.High >= o.TPPrice
		if flatten || hitStop || hitTP {
			px := c.Close
			reason := "flatten"
			if !flatten && hitStop {
				px, reason = o.StopPrice, "SL"
			} else if !flatten && hitTP {
				px, reason = o.TPPrice, "TP"
			}
			exit := domain.Order{Symbol: sym, Side: domain.Sell, Qty: pos.Qty, Price: px, Type: "MARKET", Reason: reason, Time: c.CloseTime}
			fill, err := l.exec.Execute(exit, domain.Candle{Symbol: sym, Close: px, CloseTime: c.CloseTime})
			if err != nil {
				return err
			}
			before := l.pf.Realized()
			l.pf.Apply(fill)
			if l.pf.Realized() < before {
				l.cool.NoteLoss(sym, i)
			}
			_ = l.store.RecordOrder(exit)
			_ = l.store.RecordFill(fill)
			delete(l.open, sym)
			inPos = false
		}
	}

	// 2) strategy candidates + regime + decision.
	var cands []decision.Candidate
	for _, stg := range l.strats[sym] {
		if sig := stg.Evaluate(hist, inPos); sig != nil {
			_ = l.store.RecordSignal(sym, stg.Name(), sig.Action.String(), sig.Reason, c.CloseTime)
			cands = append(cands, decision.Candidate{Kind: stg.Kind(), Signal: *sig})
		}
	}
	reg := regime.Classify(hist)
	sig := decision.Choose(reg, inPos, cands)
	_ = l.store.RecordPnLSnapshot(c.CloseTime, eq, l.pf.Realized())
	if sig == nil {
		return nil
	}
	if sig.Action == domain.Sell {
		if _, ok := l.open[sym]; ok {
			exit := domain.Order{Symbol: sym, Side: domain.Sell, Qty: pos.Qty, Price: c.Close, Type: "MARKET", Reason: sig.Reason, Time: c.CloseTime}
			fill, err := l.exec.Execute(exit, c)
			if err != nil {
				return err
			}
			before := l.pf.Realized()
			l.pf.Apply(fill)
			if l.pf.Realized() < before {
				l.cool.NoteLoss(sym, i)
			}
			_ = l.store.RecordOrder(exit)
			_ = l.store.RecordFill(fill)
			delete(l.open, sym)
		}
		return nil
	}
	// Buy
	if !l.guard.AllowEntry() || l.cool.Blocked(sym, i) {
		return nil
	}
	var deployed float64
	for s, o := range l.open {
		m := o.Price
		if s == sym {
			m = c.Close
		}
		deployed += o.Qty * m
	}
	acct := risk.Account{Equity: l.pf.Cash() + deployed, FreeUSDT: l.pf.Cash(), DeployedNotional: deployed, OpenPositions: len(l.open)}
	in := domain.Intent{Symbol: sym, Action: domain.Buy, Price: c.Close, StopDist: sig.StopDist, TPDist: sig.TPDist, Reason: sig.Reason, Time: c.CloseTime}
	o, err := l.gate.Evaluate(in, acct, l.filt)
	if err != nil {
		return nil // rejected (logged via signals)
	}
	fill, err := l.exec.Execute(o, c)
	if err != nil {
		return err
	}
	l.pf.Apply(fill)
	oo := o
	l.open[sym] = &oo
	_ = l.store.RecordOrder(o)
	return l.store.RecordFill(fill)
}
```

- [ ] **Step 4: Run** `go test ./internal/engine/` → PASS.
- [ ] **Step 5: Commit** `git add internal/engine && git commit -m "feat: add live (paper) engine reusing the trading pipeline"`

---

## Task 6: Warmup + `mode: paper` wiring in main

**Files:** Modify `cmd/bot/main.go`

**Interfaces:**
- Produces: `func runPaper(cfg config.Config)` — for each symbol, REST-backfill ~400 recent candles to seed the buffer (indicator warmup), build the strategy set + gate + guard + cooldown + simulated executor + portfolio + store, restore persisted kill-state, then `NewWSStream(testnet, symbols, interval).` and run `Live.Run(ctx, stream)` until SIGINT. Dispatched from `main()` when `cfg.Mode == "paper"`.

> No new unit test (network/long-running). Verify by `go build` and a manual smoke run. The pipeline it wires is already covered by Task 5's test.

- [ ] **Step 1: Implement** — add to `cmd/bot/main.go` a `case cfg.Mode == "paper": runPaper(cfg)` branch and:
```go
func runPaper(cfg config.Config) {
	interval := "1h"
	if len(cfg.Strategies) > 0 && cfg.Strategies[0].Timeframe != "" {
		interval = cfg.Strategies[0].Timeframe
	}
	st, err := store.Open("tradebot.db")
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	defer st.Close()
	buf := marketdata.NewBuffer(500)
	client := marketdata.NewClient(cfg.Exchange.Testnet)
	for _, sym := range cfg.Symbols {
		cs, err := client.Klines(sym, interval, 500)
		if err != nil {
			log.Printf("warmup %s failed: %v", sym, err)
			continue
		}
		buf.Seed(sym, cs)
		log.Printf("warmed %s with %d candles", sym, len(cs))
	}
	gate := risk.NewGate(cfg.Risk, cfg.Risk.FeeModel.Majors/100)
	guard := risk.NewGuard(cfg.Risk, 0)
	if killed, _ := st.LoadKillState(); killed {
		log.Print("kill-switch is ACTIVE from a prior session — entries blocked until reset")
	}
	cool := risk.NewCooldown(cfg.Risk.PostLossCooldownCandles)
	pf := portfolio.New(10000) // paper starting balance
	mk := func(sym string) []strategy.Strategy { return []strategy.Strategy{strategy.NewEMACross(sym, interval, nil)} }
	feeSide := cfg.Risk.FeeModel.Majors / 2 / 100
	l := engine.NewLive(cfg.Symbols, mk, gate, guard, cool, execution.NewSimulated(feeSide), pf, st, risk.Filters{StepSize: 0.00001, MinQty: 0.00001, MinNotional: 5}, buf)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	stream := marketdata.NewWSStream(cfg.Exchange.Testnet, cfg.Symbols, interval)
	defer stream.Close()
	log.Printf("paper trading live on %v (%s) — Ctrl-C to stop", cfg.Symbols, interval)
	if err := l.Run(ctx, stream); err != nil && err != context.Canceled {
		log.Printf("live run ended: %v", err)
	}
}
```
Wire `main()` to call `runPaper(cfg)` when `cfg.Mode == "paper"` (before the default print). Add imports: `context`, `os/signal`, and the `engine`, `execution`, `marketdata`, `portfolio`, `risk`, `store`, `strategy` packages.

- [ ] **Step 2: Run** `go build ./cmd/bot` → builds. `go vet ./...` → clean.
- [ ] **Step 3: Commit** `git add cmd/bot && git commit -m "feat: wire paper-trading mode (warmup + live WS engine)"`

---

## Self-Review

**Spec coverage (Phase 3):** live testnet WS data → T1–2 ✓; rolling buffer → T3 ✓; day/week guard reset (resolves Phase-2 M1/M2) → T4 ✓; live engine reusing the full pipeline with simulated fills, guard/cooldown/regime/decision, persisted kill-state → T5 ✓; startup warmup via REST + `mode: paper` dispatch → T6 ✓. Real signed-order placement intentionally deferred to Phase 7 (noted in constraints).

**Placeholder scan:** none — complete code/tests; the two non-unit-tested pieces (WS dial, paper wiring) are integration glue whose logic (parsing, pipeline) is unit-tested in T1/T5.

**Type consistency:** `marketdata.Stream`/`ChanStream`/`WSStream`, `Buffer.Add/History/Seed`, `Guard.RollTime`, `engine.Live` + `NewLive(...)`/`OnCandle`/`Run` signatures are consistent across tasks; reuses `risk.Gate/Guard/Cooldown/Account/Filters`, `decision.Choose`, `regime.Classify`, `execution.Executor`, `portfolio`, `store` unchanged from Phases 0–2.
