# Trading Bot Phase 2 (Full Strategy Library + Regime + Complete Guardrails) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Complete the trading brain: add RSI/MACD/Bollinger indicators, the three remaining strategies (rsi_bb_reversion, donchian_breakout, macd_momentum), a market-regime classifier, a decision aggregator (regime gating + same-symbol netting), and the full risk guardrails (daily/weekly loss limits + kill-switch, hard-flatten, candle-aligned cooldown), plus store extensions.

**Architecture:** Builds directly on the Phase 0–1 packages. New indicators extend `internal/indicators`. Each new strategy implements the existing `strategy.Strategy` interface (now with a `Kind()` method). A new `internal/regime` package classifies the market; a new `internal/decision` package gates and selects signals; `internal/risk/guard.go` adds the loss/kill-switch/flatten/cooldown guard. The backtest/engine stay the integration point.

**Tech Stack:** Go 1.22, same deps as Phase 0–1 (no new third-party libraries).

## Global Constraints

- **Spot, LONG-ONLY.** Quantity ≥ 0; Sell only reduces to cash. No shorting.
- **NO LOOK-AHEAD.** All indicators (signals + ATR stops) use CLOSED candles only. Signals on candle close.
- **Canonical constants are PINNED** (non-optimizable): EMA 9/21, ADX/ATR/RSI 14, MACD 12/26/9, BB 20/2.0, SMA200, EMA100.
- **Volatility-aware TP:** `tp_dist = tp_reward_mult × stop_dist` (default 1.6), gated to post-fee R:R ≥ 1.3 (already enforced in `risk.Gate`).
- **Regime gating:** in **Low-Vol-Chop**, disable trend/breakout/momentum strategies; leave only `rsi_bb_reversion` active.
- **Guardrails are non-bypassable.** Daily-loss breach trips the kill-switch (halts NEW entries; exits still allowed); −6% drawdown → hard-flatten; post-loss cooldown is **candle-aligned** (N=2 closed candles). Kill-switch/cooldown state persists in SQLite.
- **Module path:** `tradebot`. TDD + one commit per task.
- **Strategy/regime fixtures:** a strategy's multi-gate entry is hard to trigger deterministically. If a provided test fixture does not fire the entry under the real indicator dynamics, adjust ONLY the test fixture (the candle series) — never the strategy logic, the assertion intent, or the gate conditions — to construct a scenario that genuinely satisfies every gate. The strategy/classifier code must implement the spec's gates exactly.

---

## File Structure

```
internal/indicators/indicators.go   + RSI, MACD, Bollinger (extend existing file)
internal/strategy/strategy.go        + Kind() on the Strategy interface
internal/strategy/ema_cross.go       + Kind() = "trend"
internal/strategy/rsi_bb.go          rsi_bb_reversion
internal/strategy/donchian.go        donchian_breakout
internal/strategy/macd.go            macd_momentum
internal/regime/regime.go            Regime enum + Classify()
internal/decision/decision.go        Kind gating per regime + Choose()
internal/risk/guard.go               Guard: loss limits, kill-switch, hard-flatten, cooldown
internal/store/store.go              + RecordSignal, RecordPnLSnapshot, Save/LoadKillState
```

---

## Task 1: Indicator — RSI (Wilder)

**Files:** Modify `internal/indicators/indicators.go`, `internal/indicators/indicators_test.go`

**Interfaces:**
- Produces: `func RSI(vals []float64, period int) []float64` — Wilder-smoothed RSI in [0,100]; indices < period are NaN.

- [ ] **Step 1: Failing test** — append to `internal/indicators/indicators_test.go`:
```go
func TestRSIAllGainsIs100(t *testing.T) {
	vals := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	r := RSI(vals, 14)
	if !math.IsNaN(r[13]) { t.Fatal("RSI before period must be NaN at index 13") }
	if r[15] < 99.9 { t.Fatalf("all-gains RSI must be ~100, got %v", r[15]) }
}

func TestRSIMidRange(t *testing.T) {
	vals := []float64{44, 44.34, 44.09, 44.15, 43.61, 44.33, 44.83, 45.10, 45.42, 45.84, 46.08, 45.89, 46.03, 45.61, 46.28, 46.28}
	r := RSI(vals, 14)
	if math.IsNaN(r[15]) || r[15] <= 0 || r[15] >= 100 {
		t.Fatalf("RSI out of range: %v", r[15])
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/indicators/ -run RSI` → FAIL (RSI undefined).

- [ ] **Step 3: Implement** — append to `internal/indicators/indicators.go`:
```go
// RSI is Wilder's Relative Strength Index in [0,100]; indices < period are NaN.
func RSI(vals []float64, period int) []float64 {
	out := make([]float64, len(vals))
	for i := range out {
		out[i] = math.NaN()
	}
	if period <= 0 || len(vals) <= period {
		return out
	}
	var gain, loss float64
	for i := 1; i <= period; i++ {
		ch := vals[i] - vals[i-1]
		if ch >= 0 {
			gain += ch
		} else {
			loss -= ch
		}
	}
	avgGain := gain / float64(period)
	avgLoss := loss / float64(period)
	rsi := func(g, l float64) float64 {
		if l == 0 {
			return 100
		}
		rs := g / l
		return 100 - 100/(1+rs)
	}
	out[period] = rsi(avgGain, avgLoss)
	for i := period + 1; i < len(vals); i++ {
		ch := vals[i] - vals[i-1]
		g, l := 0.0, 0.0
		if ch >= 0 {
			g = ch
		} else {
			l = -ch
		}
		avgGain = (avgGain*float64(period-1) + g) / float64(period)
		avgLoss = (avgLoss*float64(period-1) + l) / float64(period)
		out[i] = rsi(avgGain, avgLoss)
	}
	return out
}
```

- [ ] **Step 4: Run** `go test ./internal/indicators/` → PASS.
- [ ] **Step 5: Commit** `git add internal/indicators && git commit -m "feat: add RSI (Wilder) indicator"`

---

## Task 2: Indicator — MACD

**Files:** Modify `internal/indicators/indicators.go`, `internal/indicators/indicators_test.go`

**Interfaces:**
- Produces: `func MACD(vals []float64, fast, slow, signal int) (macd, sig, hist []float64)` — `macd = EMA(fast) − EMA(slow)`; `sig = EMA(macd, signal)` computed over the valid macd tail; `hist = macd − sig`. All length `len(vals)`, NaN before warm-up.

- [ ] **Step 1: Failing test** — append:
```go
func TestMACDCrossSign(t *testing.T) {
	vals := make([]float64, 60)
	for i := range vals { vals[i] = 100 + float64(i) } // steady uptrend
	macd, sig, hist := MACD(vals, 12, 26, 9)
	last := len(vals) - 1
	if math.IsNaN(macd[last]) || math.IsNaN(sig[last]) || math.IsNaN(hist[last]) {
		t.Fatal("MACD should be defined at the end of a long series")
	}
	if macd[last] <= 0 {
		t.Fatalf("uptrend MACD line should be positive, got %v", macd[last])
	}
	if math.Abs((macd[last]-sig[last])-hist[last]) > 1e-9 {
		t.Fatal("hist must equal macd - sig")
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/indicators/ -run MACD` → FAIL.

- [ ] **Step 3: Implement** — append:
```go
// MACD returns the MACD line, signal line, and histogram. NaN before warm-up.
func MACD(vals []float64, fast, slow, signal int) (macd, sig, hist []float64) {
	n := len(vals)
	macd = make([]float64, n)
	sig = make([]float64, n)
	hist = make([]float64, n)
	for i := range vals {
		macd[i], sig[i], hist[i] = math.NaN(), math.NaN(), math.NaN()
	}
	if n <= slow {
		return
	}
	emaF := EMA(vals, fast)
	emaS := EMA(vals, slow)
	for i := slow - 1; i < n; i++ {
		if !math.IsNaN(emaF[i]) && !math.IsNaN(emaS[i]) {
			macd[i] = emaF[i] - emaS[i]
		}
	}
	// Signal EMA over the valid macd tail (from index slow-1).
	start := slow - 1
	tail := macd[start:]
	sigTail := EMA(tail, signal)
	for i := range sigTail {
		if !math.IsNaN(sigTail[i]) {
			sig[start+i] = sigTail[i]
			hist[start+i] = macd[start+i] - sig[start+i]
		}
	}
	return
}
```

- [ ] **Step 4: Run** `go test ./internal/indicators/` → PASS.
- [ ] **Step 5: Commit** `git add internal/indicators && git commit -m "feat: add MACD indicator"`

---

## Task 3: Indicator — Bollinger Bands

**Files:** Modify `internal/indicators/indicators.go`, `internal/indicators/indicators_test.go`

**Interfaces:**
- Produces: `func Bollinger(vals []float64, period int, k float64) (mid, upper, lower []float64)` — `mid = SMA`; `upper/lower = mid ± k·population-stddev` over the window. NaN before `period-1`.

- [ ] **Step 1: Failing test** — append:
```go
func TestBollingerBandsAroundMean(t *testing.T) {
	vals := []float64{10, 12, 11, 13, 12, 14, 13, 15, 14, 16}
	mid, up, lo := Bollinger(vals, 5, 2.0)
	i := len(vals) - 1
	if math.IsNaN(mid[i]) || math.IsNaN(up[i]) || math.IsNaN(lo[i]) {
		t.Fatal("bands undefined at end")
	}
	if !(lo[i] < mid[i] && mid[i] < up[i]) {
		t.Fatalf("expected lo<mid<up, got %v %v %v", lo[i], mid[i], up[i])
	}
	// symmetric around mid
	if math.Abs((up[i]-mid[i])-(mid[i]-lo[i])) > 1e-9 {
		t.Fatal("bands must be symmetric around mid")
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/indicators/ -run Bollinger` → FAIL.

- [ ] **Step 3: Implement** — append:
```go
// Bollinger returns the middle (SMA), upper, and lower bands using population stddev.
func Bollinger(vals []float64, period int, k float64) (mid, upper, lower []float64) {
	n := len(vals)
	mid = SMA(vals, period)
	upper = make([]float64, n)
	lower = make([]float64, n)
	for i := range vals {
		upper[i], lower[i] = math.NaN(), math.NaN()
	}
	for i := period - 1; i < n; i++ {
		if math.IsNaN(mid[i]) {
			continue
		}
		var sumsq float64
		for j := i - period + 1; j <= i; j++ {
			d := vals[j] - mid[i]
			sumsq += d * d
		}
		sd := math.Sqrt(sumsq / float64(period))
		upper[i] = mid[i] + k*sd
		lower[i] = mid[i] - k*sd
	}
	return
}
```

- [ ] **Step 4: Run** `go test ./internal/indicators/` → PASS.
- [ ] **Step 5: Commit** `git add internal/indicators && git commit -m "feat: add Bollinger Bands indicator"`

---

## Task 4: Strategy interface gains Kind(); rsi_bb_reversion

**Files:** Modify `internal/strategy/strategy.go`, `internal/strategy/ema_cross.go`; Create `internal/strategy/rsi_bb.go`, `internal/strategy/rsi_bb_test.go`

**Interfaces:**
- Modify `Strategy` to add `Kind() string` (one of "trend","reversion","breakout","momentum").
- Add `func (s *EMACross) Kind() string { return "trend" }`.
- Produces: `func NewRSIBBReversion(symbol, timeframe string, p map[string]float64) *RSIBBReversion` implementing `Strategy` with `Kind()=="reversion"`.

- [ ] **Step 1: Failing test** — `internal/strategy/rsi_bb_test.go`:
```go
package strategy

import (
	"testing"

	"tradebot/internal/domain"
)

func TestRSIBBKindAndWarmup(t *testing.T) {
	s := NewRSIBBReversion("BTCUSDT", "1h", nil)
	if s.Kind() != "reversion" || s.Name() != "rsi_bb_reversion" {
		t.Fatalf("bad identity: %s/%s", s.Name(), s.Kind())
	}
	if s.Warmup() < 200 {
		t.Fatalf("warmup must cover SMA200, got %d", s.Warmup())
	}
}

func TestRSIBBNoSignalBeforeWarmup(t *testing.T) {
	s := NewRSIBBReversion("BTCUSDT", "1h", nil)
	cs := make([]domain.Candle, 50)
	for i := range cs { cs[i] = domain.Candle{Close: 100, High: 101, Low: 99, Closed: true} }
	if sig := s.Evaluate(cs, false); sig != nil {
		t.Fatalf("expected nil before warmup, got %+v", sig)
	}
}

func TestRSIBBBuysOversoldInRange(t *testing.T) {
	s := NewRSIBBReversion("BTCUSDT", "1h", nil)
	// 220 candles: an uptrend baseline (close>SMA200) that oscillates, ending with a
	// sharp oversold dip below the lower band but still above SMA200.
	cs := make([]domain.Candle, 0, 260)
	price := 100.0
	for i := 0; i < 210; i++ {
		price += 0.5
		cs = append(cs, domain.Candle{Symbol: "BTCUSDT", Close: price, High: price + 2, Low: price - 2, Closed: true, CloseTime: int64(i)})
	}
	// sharp dip (oversold) over a few candles
	for i := 0; i < 6; i++ {
		price -= 6
		cs = append(cs, domain.Candle{Symbol: "BTCUSDT", Close: price, High: price + 1, Low: price - 1, Closed: true, CloseTime: int64(210 + i)})
	}
	var got *domain.Signal
	for i := s.Warmup(); i <= len(cs); i++ {
		if sig := s.Evaluate(cs[:i], false); sig != nil && sig.Action == domain.Buy {
			got = sig
			break
		}
	}
	if got == nil {
		t.Fatal("expected an oversold BUY while price is still above SMA200")
	}
	if got.StopDist <= 0 {
		t.Fatalf("BUY must carry an ATR stop, got %v", got.StopDist)
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/strategy/ -run RSIBB` → FAIL.

- [ ] **Step 3: Implement** — add to `internal/strategy/strategy.go` the `Kind() string` method in the interface:
```go
type Strategy interface {
	Name() string
	Kind() string // "trend" | "reversion" | "breakout" | "momentum"
	Symbol() string
	Timeframe() string
	Warmup() int
	Evaluate(history []domain.Candle, inPosition bool) *domain.Signal
}
```
Add to `internal/strategy/ema_cross.go`: `func (s *EMACross) Kind() string { return "trend" }`.

Create `internal/strategy/rsi_bb.go`:
```go
package strategy

import (
	"fmt"
	"math"

	"tradebot/internal/domain"
	"tradebot/internal/indicators"
)

type RSIBBReversion struct {
	symbol, timeframe                                   string
	rsiPeriod, bbPeriod, smaTrend, adxPeriod, atrPeriod int
	bbStdev, oversold, exitRSI, adxMax, minEdgePct      float64
	slATRMult, tpRewardMult                             float64
}

func NewRSIBBReversion(symbol, timeframe string, p map[string]float64) *RSIBBReversion {
	get := func(k string, d float64) float64 {
		if v, ok := p[k]; ok {
			return v
		}
		return d
	}
	return &RSIBBReversion{
		symbol: symbol, timeframe: timeframe,
		rsiPeriod: 14, bbPeriod: 20, smaTrend: 200, adxPeriod: 14, atrPeriod: 14,
		bbStdev:   get("bb_stdev", 2.0),
		oversold:  get("oversold", 30),
		exitRSI:   get("exit_rsi", 60),
		adxMax:    get("adx_max", 25),
		minEdgePct: get("min_edge_pct", 1.2),
		slATRMult: get("sl_atr_mult", 2.0),
		tpRewardMult: get("tp_reward_mult", 1.6),
	}
}

func (s *RSIBBReversion) Name() string      { return "rsi_bb_reversion" }
func (s *RSIBBReversion) Kind() string      { return "reversion" }
func (s *RSIBBReversion) Symbol() string    { return s.symbol }
func (s *RSIBBReversion) Timeframe() string { return s.timeframe }
func (s *RSIBBReversion) Warmup() int       { return s.smaTrend + 1 }

func (s *RSIBBReversion) Evaluate(h []domain.Candle, inPosition bool) *domain.Signal {
	if len(h) < s.Warmup() {
		return nil
	}
	closes := make([]float64, len(h))
	for i, c := range h {
		closes[i] = c.Close
	}
	rsi := indicators.RSI(closes, s.rsiPeriod)
	mid, _, lower := indicators.Bollinger(closes, s.bbPeriod, s.bbStdev)
	sma := indicators.SMA(closes, s.smaTrend)
	adx := indicators.ADX(h, s.adxPeriod)
	atr := indicators.ATR(h, s.atrPeriod)
	i := len(h) - 1
	if math.IsNaN(rsi[i]) || math.IsNaN(mid[i]) || math.IsNaN(lower[i]) || math.IsNaN(sma[i]) || math.IsNaN(adx[i]) || math.IsNaN(atr[i]) {
		return nil
	}
	c := h[i].Close
	if inPosition {
		if c >= mid[i] || rsi[i] >= s.exitRSI {
			return &domain.Signal{Symbol: s.symbol, Action: domain.Sell, Time: h[i].CloseTime, Reason: "reversion mean/RSI exit"}
		}
		return nil
	}
	// entry: oversold + below lower band + above SMA200 (hard gate) + not strongly trending + min edge.
	edgePct := (mid[i] - c) / c * 100
	if rsi[i] <= s.oversold && c <= lower[i] && c > sma[i] && adx[i] < s.adxMax && edgePct >= s.minEdgePct {
		stop := s.slATRMult * atr[i]
		return &domain.Signal{
			Symbol: s.symbol, Action: domain.Buy, Time: h[i].CloseTime,
			StopDist: stop, TPDist: s.tpRewardMult * stop,
			Reason: fmt.Sprintf("RSI %.1f<=%.0f + <lowerBB + >SMA200 + edge %.2f%%", rsi[i], s.oversold, edgePct),
		}
	}
	return nil
}
```

- [ ] **Step 4: Run** `go test ./internal/strategy/ ./internal/engine/ ./internal/backtest/` → PASS (interface change must not break existing strategy users).
- [ ] **Step 5: Commit** `git add internal/strategy && git commit -m "feat: add Kind() and rsi_bb_reversion strategy"`

---

## Task 5: Strategy — donchian_breakout

**Files:** Create `internal/strategy/donchian.go`, `internal/strategy/donchian_test.go`

**Interfaces:** `func NewDonchianBreakout(symbol, timeframe string, p map[string]float64) *DonchianBreakout` (`Kind()=="breakout"`).

- [ ] **Step 1: Failing test** — `internal/strategy/donchian_test.go`:
```go
package strategy

import (
	"testing"

	"tradebot/internal/domain"
)

func TestDonchianBreakoutBuysNewHigh(t *testing.T) {
	s := NewDonchianBreakout("BTCUSDT", "4h", nil)
	if s.Kind() != "breakout" { t.Fatalf("kind=%s", s.Kind()) }
	cs := make([]domain.Candle, 0, 60)
	// 40 candles ranging tightly, then a breakout candle with volatility expansion.
	for i := 0; i < 40; i++ {
		cs = append(cs, domain.Candle{Symbol: "BTCUSDT", Close: 100, High: 101, Low: 99, Closed: true, CloseTime: int64(i)})
	}
	// breakout: a big high-range candle closing above the prior 20-high (101).
	cs = append(cs, domain.Candle{Symbol: "BTCUSDT", Close: 115, High: 118, Low: 100, Closed: true, CloseTime: 40})
	var got *domain.Signal
	for i := s.Warmup(); i <= len(cs); i++ {
		if sig := s.Evaluate(cs[:i], false); sig != nil && sig.Action == domain.Buy {
			got = sig
		}
	}
	if got == nil { t.Fatal("expected a breakout BUY") }
	if got.StopDist <= 0 { t.Fatalf("BUY needs an ATR stop, got %v", got.StopDist) }
}

func TestDonchianExitsOnChannelLow(t *testing.T) {
	s := NewDonchianBreakout("BTCUSDT", "4h", nil)
	cs := make([]domain.Candle, 0, 60)
	for i := 0; i < 40; i++ {
		cs = append(cs, domain.Candle{Symbol: "BTCUSDT", Close: 100 + float64(i), High: 102 + float64(i), Low: 98 + float64(i), Closed: true, CloseTime: int64(i)})
	}
	// a candle closing below the prior-10 low should signal SELL when in position.
	cs = append(cs, domain.Candle{Symbol: "BTCUSDT", Close: 120, High: 121, Low: 119, Closed: true, CloseTime: 40})
	sig := s.Evaluate(cs, true)
	if sig == nil || sig.Action != domain.Sell {
		t.Fatalf("expected SELL on channel-low exit, got %+v", sig)
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/strategy/ -run Donchian` → FAIL.

- [ ] **Step 3: Implement** — `internal/strategy/donchian.go`:
```go
package strategy

import (
	"math"

	"tradebot/internal/domain"
	"tradebot/internal/indicators"
)

type DonchianBreakout struct {
	symbol, timeframe                     string
	entryLookback, exitLookback, atrP, expN int
	slATRMult, tpPct                      float64
}

func NewDonchianBreakout(symbol, timeframe string, p map[string]float64) *DonchianBreakout {
	get := func(k string, d float64) float64 {
		if v, ok := p[k]; ok {
			return v
		}
		return d
	}
	return &DonchianBreakout{
		symbol: symbol, timeframe: timeframe,
		entryLookback: int(get("entry_lookback", 20)),
		exitLookback:  int(get("exit_lookback", 10)),
		atrP:          14,
		expN:          20,
		slATRMult:     get("sl_atr_mult", 2.5),
		tpPct:         get("tp_pct", 4.0),
	}
}

func (s *DonchianBreakout) Name() string      { return "donchian_breakout" }
func (s *DonchianBreakout) Kind() string      { return "breakout" }
func (s *DonchianBreakout) Symbol() string    { return s.symbol }
func (s *DonchianBreakout) Timeframe() string { return s.timeframe }
func (s *DonchianBreakout) Warmup() int       { return s.entryLookback + s.expN + 1 }

func (s *DonchianBreakout) Evaluate(h []domain.Candle, inPosition bool) *domain.Signal {
	if len(h) < s.Warmup() {
		return nil
	}
	i := len(h) - 1
	atr := indicators.ATR(h, s.atrP)
	if math.IsNaN(atr[i]) {
		return nil
	}
	if inPosition {
		// exit: close below the lowest close of the prior exitLookback candles.
		low := math.Inf(1)
		for j := i - s.exitLookback; j < i; j++ {
			if h[j].Close < low {
				low = h[j].Close
			}
		}
		if h[i].Close < low {
			return &domain.Signal{Symbol: s.symbol, Action: domain.Sell, Time: h[i].CloseTime, Reason: "10-bar channel-low exit"}
		}
		return nil
	}
	// entry: new entryLookback-high close + ATR expansion vs avg of last expN.
	high := math.Inf(-1)
	for j := i - s.entryLookback; j < i; j++ {
		if h[j].Close > high {
			high = h[j].Close
		}
	}
	var sumATR float64
	cnt := 0
	for j := i - s.expN; j < i; j++ {
		if !math.IsNaN(atr[j]) {
			sumATR += atr[j]
			cnt++
		}
	}
	avgATR := math.Inf(1)
	if cnt > 0 {
		avgATR = sumATR / float64(cnt)
	}
	if h[i].Close > high && atr[i] > avgATR {
		stop := s.slATRMult * atr[i]
		return &domain.Signal{
			Symbol: s.symbol, Action: domain.Buy, Time: h[i].CloseTime,
			StopDist: stop, TPDist: h[i].Close * s.tpPct / 100,
			Reason: "20-bar breakout + ATR expansion",
		}
	}
	return nil
}
```

- [ ] **Step 4: Run** `go test ./internal/strategy/` → PASS.
- [ ] **Step 5: Commit** `git add internal/strategy && git commit -m "feat: add donchian_breakout strategy"`

---

## Task 6: Strategy — macd_momentum

**Files:** Create `internal/strategy/macd.go`, `internal/strategy/macd_test.go`

**Interfaces:** `func NewMACDMomentum(symbol, timeframe string, p map[string]float64) *MACDMomentum` (`Kind()=="momentum"`). Holds a `lastExitCandle` debounce so a recross cannot round-trip within one candle.

- [ ] **Step 1: Failing test** — `internal/strategy/macd_test.go`:
```go
package strategy

import (
	"testing"

	"tradebot/internal/domain"
)

func TestMACDMomentumIdentity(t *testing.T) {
	s := NewMACDMomentum("BTCUSDT", "1h", nil)
	if s.Name() != "macd_momentum" || s.Kind() != "momentum" {
		t.Fatalf("bad identity %s/%s", s.Name(), s.Kind())
	}
	if s.Warmup() < 100 { t.Fatalf("warmup must cover EMA100, got %d", s.Warmup()) }
}

func TestMACDMomentumBuysOnUptrendCross(t *testing.T) {
	s := NewMACDMomentum("BTCUSDT", "1h", nil)
	// long flat baseline then an accelerating uptrend -> MACD crosses up above EMA100 with ADX.
	cs := make([]domain.Candle, 0, 200)
	for i := 0; i < 120; i++ {
		cs = append(cs, domain.Candle{Symbol: "BTCUSDT", Close: 100, High: 101, Low: 99, Closed: true, CloseTime: int64(i)})
	}
	for i := 0; i < 60; i++ {
		p := 100 + float64(i)*float64(i)*0.05 // accelerating
		cs = append(cs, domain.Candle{Symbol: "BTCUSDT", Close: p, High: p + 1, Low: p - 1, Closed: true, CloseTime: int64(120 + i)})
	}
	var got *domain.Signal
	for i := s.Warmup(); i <= len(cs); i++ {
		if sig := s.Evaluate(cs[:i], false); sig != nil && sig.Action == domain.Buy {
			got = sig
			break
		}
	}
	if got == nil { t.Fatal("expected a momentum BUY on the accelerating uptrend") }
	if got.StopDist <= 0 { t.Fatalf("BUY needs ATR stop, got %v", got.StopDist) }
}
```

- [ ] **Step 2: Run** `go test ./internal/strategy/ -run MACDMomentum` → FAIL.

- [ ] **Step 3: Implement** — `internal/strategy/macd.go`:
```go
package strategy

import (
	"math"

	"tradebot/internal/domain"
	"tradebot/internal/indicators"
)

type MACDMomentum struct {
	symbol, timeframe                       string
	fast, slow, signal, trendEMA, rsiP, adxP, atrP int
	rsiMax, adxMin, slATRMult, tpRewardMult float64
	lastExitTime                            int64
}

func NewMACDMomentum(symbol, timeframe string, p map[string]float64) *MACDMomentum {
	get := func(k string, d float64) float64 {
		if v, ok := p[k]; ok {
			return v
		}
		return d
	}
	return &MACDMomentum{
		symbol: symbol, timeframe: timeframe,
		fast: 12, slow: 26, signal: 9, trendEMA: 100, rsiP: 14, adxP: 14, atrP: 14,
		rsiMax:    get("rsi_max", 75),
		adxMin:    get("adx_min", 20),
		slATRMult: get("sl_atr_mult", 2.0),
		tpRewardMult: get("tp_reward_mult", 1.6),
		lastExitTime: -1,
	}
}

func (s *MACDMomentum) Name() string      { return "macd_momentum" }
func (s *MACDMomentum) Kind() string      { return "momentum" }
func (s *MACDMomentum) Symbol() string    { return s.symbol }
func (s *MACDMomentum) Timeframe() string { return s.timeframe }
func (s *MACDMomentum) Warmup() int       { return s.trendEMA + 1 }

func (s *MACDMomentum) Evaluate(h []domain.Candle, inPosition bool) *domain.Signal {
	if len(h) < s.Warmup() {
		return nil
	}
	closes := make([]float64, len(h))
	for i, c := range h {
		closes[i] = c.Close
	}
	macd, sig, hist := indicators.MACD(closes, s.fast, s.slow, s.signal)
	ema := indicators.EMA(closes, s.trendEMA)
	rsi := indicators.RSI(closes, s.rsiP)
	adx := indicators.ADX(h, s.adxP)
	atr := indicators.ATR(h, s.atrP)
	i := len(h) - 1
	if i < 1 || math.IsNaN(macd[i]) || math.IsNaN(macd[i-1]) || math.IsNaN(sig[i]) || math.IsNaN(sig[i-1]) || math.IsNaN(hist[i]) || math.IsNaN(hist[i-1]) || math.IsNaN(ema[i]) || math.IsNaN(rsi[i]) || math.IsNaN(adx[i]) || math.IsNaN(atr[i]) {
		return nil
	}
	crossUp := macd[i-1] <= sig[i-1] && macd[i] > sig[i]
	crossDown := macd[i-1] >= sig[i-1] && macd[i] < sig[i]
	c := h[i].Close
	if inPosition {
		if crossDown {
			s.lastExitTime = h[i].CloseTime
			return &domain.Signal{Symbol: s.symbol, Action: domain.Sell, Time: h[i].CloseTime, Reason: "MACD recross exit"}
		}
		return nil
	}
	// 1-candle debounce after an exit.
	if s.lastExitTime >= 0 && h[i-1].CloseTime == s.lastExitTime {
		return nil
	}
	histRising := hist[i] > hist[i-1]
	if crossUp && histRising && c > ema[i] && adx[i] >= s.adxMin && rsi[i] <= s.rsiMax && hist[i] > 0 {
		stop := s.slATRMult * atr[i]
		return &domain.Signal{
			Symbol: s.symbol, Action: domain.Buy, Time: h[i].CloseTime,
			StopDist: stop, TPDist: s.tpRewardMult * stop,
			Reason: "MACD cross up + hist rising + >EMA100 + ADX ok",
		}
	}
	return nil
}
```

- [ ] **Step 4: Run** `go test ./internal/strategy/` → PASS.
- [ ] **Step 5: Commit** `git add internal/strategy && git commit -m "feat: add macd_momentum strategy"`

---

## Task 7: Market-regime classifier

**Files:** Create `internal/regime/regime.go`, `internal/regime/regime_test.go`

**Interfaces:**
- Produces: `type Regime int` with `Unknown, TrendingUp, TrendingDown, Ranging, HighVol, LowVolChop` + `String()`; `func Classify(h []domain.Candle) Regime`.
- Logic (closed candles): compute ADX(14), normalized Bollinger(20,2) bandwidth `bw = (upper-lower)/mid`, and SMA(50) for direction. Rules: `ADX<20 AND bw < lowVolBW(0.015)` → LowVolChop; `ADX≥25` → TrendingUp if close>SMA50 else TrendingDown; `bw > highVolBW(0.08)` → HighVol; else Ranging. Not enough data (or NaN indicators) → Unknown. (Normalized absolute thresholds are symbol-independent and deterministic; the learning layer may later replace them with adaptive percentiles.)

- [ ] **Step 1: Failing test** — `internal/regime/regime_test.go`:
```go
package regime

import (
	"testing"

	"tradebot/internal/domain"
)

func mk(closes []float64) []domain.Candle {
	cs := make([]domain.Candle, len(closes))
	for i, c := range closes {
		cs[i] = domain.Candle{Close: c, High: c + 1, Low: c - 1, Closed: true, CloseTime: int64(i)}
	}
	return cs
}

func TestClassifyTrendingUp(t *testing.T) {
	closes := make([]float64, 160)
	for i := range closes { closes[i] = 100 + float64(i)*2 } // strong steady uptrend
	if r := Classify(mk(closes)); r != TrendingUp {
		t.Fatalf("want TrendingUp, got %s", r)
	}
}

func TestClassifyLowVolChop(t *testing.T) {
	closes := make([]float64, 160)
	for i := range closes {
		// tiny oscillation, no trend -> low ADX, narrow bands
		if i%2 == 0 { closes[i] = 100.05 } else { closes[i] = 99.95 }
	}
	if r := Classify(mk(closes)); r != LowVolChop {
		t.Fatalf("want LowVolChop, got %s", r)
	}
}

func TestClassifyUnknownWhenShort(t *testing.T) {
	if r := Classify(mk([]float64{1, 2, 3})); r != Unknown {
		t.Fatalf("want Unknown, got %s", r)
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/regime/` → FAIL.

- [ ] **Step 3: Implement** — `internal/regime/regime.go`:
```go
package regime

import (
	"math"

	"tradebot/internal/domain"
	"tradebot/internal/indicators"
)

type Regime int

const (
	Unknown Regime = iota
	TrendingUp
	TrendingDown
	Ranging
	HighVol
	LowVolChop
)

func (r Regime) String() string {
	switch r {
	case TrendingUp:
		return "TrendingUp"
	case TrendingDown:
		return "TrendingDown"
	case Ranging:
		return "Ranging"
	case HighVol:
		return "HighVol"
	case LowVolChop:
		return "LowVolChop"
	default:
		return "Unknown"
	}
}

const (
	lowVolBW  = 0.015 // normalized BB width below this = low volatility
	highVolBW = 0.08  // normalized BB width above this = high volatility
)

// Classify labels the market regime from the closed-candle history.
func Classify(h []domain.Candle) Regime {
	if len(h) < 60 {
		return Unknown
	}
	closes := make([]float64, len(h))
	for i, c := range h {
		closes[i] = c.Close
	}
	adx := indicators.ADX(h, 14)
	mid, up, lo := indicators.Bollinger(closes, 20, 2.0)
	sma := indicators.SMA(closes, 50)
	i := len(h) - 1
	if math.IsNaN(adx[i]) || math.IsNaN(mid[i]) || math.IsNaN(sma[i]) || math.IsNaN(up[i]) || math.IsNaN(lo[i]) || mid[i] == 0 {
		return Unknown
	}
	bw := (up[i] - lo[i]) / mid[i]
	switch {
	case adx[i] < 20 && bw < lowVolBW:
		return LowVolChop
	case adx[i] >= 25:
		if closes[i] > sma[i] {
			return TrendingUp
		}
		return TrendingDown
	case bw > highVolBW:
		return HighVol
	default:
		return Ranging
	}
}
```

- [ ] **Step 4: Run** `go test ./internal/regime/` → PASS.
- [ ] **Step 5: Commit** `git add internal/regime && git commit -m "feat: add market-regime classifier"`

---

## Task 8: Decision aggregator (regime gating + same-symbol netting)

**Files:** Create `internal/decision/decision.go`, `internal/decision/decision_test.go`

**Interfaces:**
- Produces:
  - `func KindEnabled(r regime.Regime, kind string) bool` — in `LowVolChop`, only `"reversion"` is enabled; all kinds enabled otherwise (Unknown is permissive only for reversion to stay conservative: return true for reversion, false for others when Unknown? — keep simple: Unknown enables all).
  - `type Candidate struct { Kind string; Signal domain.Signal }`
  - `func Choose(r regime.Regime, inPosition bool, cands []Candidate) *domain.Signal` — if `inPosition`, return the first Sell candidate (exits always allowed regardless of regime); else return the first Buy candidate whose `KindEnabled(r, kind)` is true; nil if none.

- [ ] **Step 1: Failing test** — `internal/decision/decision_test.go`:
```go
package decision

import (
	"testing"

	"tradebot/internal/domain"
	"tradebot/internal/regime"
)

func TestLowVolChopDisablesTrend(t *testing.T) {
	if KindEnabled(regime.LowVolChop, "trend") {
		t.Fatal("trend must be disabled in LowVolChop")
	}
	if !KindEnabled(regime.LowVolChop, "reversion") {
		t.Fatal("reversion must stay enabled in LowVolChop")
	}
}

func TestChooseSkipsDisabledKind(t *testing.T) {
	cands := []Candidate{
		{Kind: "trend", Signal: domain.Signal{Action: domain.Buy, Reason: "trend buy"}},
		{Kind: "reversion", Signal: domain.Signal{Action: domain.Buy, Reason: "rev buy"}},
	}
	got := Choose(regime.LowVolChop, false, cands)
	if got == nil || got.Reason != "rev buy" {
		t.Fatalf("expected reversion buy in LowVolChop, got %+v", got)
	}
}

func TestChooseExitAlwaysAllowed(t *testing.T) {
	cands := []Candidate{{Kind: "trend", Signal: domain.Signal{Action: domain.Sell, Reason: "exit"}}}
	got := Choose(regime.LowVolChop, true, cands)
	if got == nil || got.Action != domain.Sell {
		t.Fatalf("exit must be allowed in any regime, got %+v", got)
	}
}

func TestChooseNoStackWhenInPosition(t *testing.T) {
	cands := []Candidate{{Kind: "trend", Signal: domain.Signal{Action: domain.Buy, Reason: "buy"}}}
	if got := Choose(regime.TrendingUp, true, cands); got != nil {
		t.Fatalf("must not open a second position while in one, got %+v", got)
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/decision/` → FAIL.

- [ ] **Step 3: Implement** — `internal/decision/decision.go`:
```go
package decision

import (
	"tradebot/internal/domain"
	"tradebot/internal/regime"
)

// KindEnabled reports whether a strategy of the given kind may OPEN a position in the regime.
// In LowVolChop only mean-reversion is allowed; trend/breakout/momentum are disabled there.
func KindEnabled(r regime.Regime, kind string) bool {
	if r == regime.LowVolChop {
		return kind == "reversion"
	}
	return true
}

type Candidate struct {
	Kind   string
	Signal domain.Signal
}

// Choose selects one action for a symbol. Exits are always allowed (any regime).
// Entries are gated by regime and blocked entirely while already in a position
// (same-symbol netting: never stack a second long).
func Choose(r regime.Regime, inPosition bool, cands []Candidate) *domain.Signal {
	if inPosition {
		for _, c := range cands {
			if c.Signal.Action == domain.Sell {
				s := c.Signal
				return &s
			}
		}
		return nil
	}
	for _, c := range cands {
		if c.Signal.Action == domain.Buy && KindEnabled(r, c.Kind) {
			s := c.Signal
			return &s
		}
	}
	return nil
}
```

- [ ] **Step 4: Run** `go test ./internal/decision/` → PASS.
- [ ] **Step 5: Commit** `git add internal/decision && git commit -m "feat: add decision aggregator with regime gating and netting"`

---

## Task 9: Risk guard — loss limits + kill-switch

**Files:** Create `internal/risk/guard.go`, `internal/risk/guard_test.go`

**Interfaces:**
- Produces:
  - `type Guard struct {...}`, `func NewGuard(cfg config.RiskCfg, startEquity float64) *Guard`
  - `func (g *Guard) StartDay(equity float64)` / `func (g *Guard) StartWeek(equity float64)` — set the period baselines.
  - `func (g *Guard) Mark(equity float64)` — update current equity; trips the kill-switch if daily loss ≥ `daily_loss_limit_pct` or weekly ≥ `weekly_loss_limit_pct`.
  - `func (g *Guard) AllowEntry() bool` — false when killed.
  - `func (g *Guard) ShouldFlatten() bool` — true when daily drawdown ≥ `hard_flatten_drawdown_pct`.
  - `func (g *Guard) Killed() bool`, `func (g *Guard) Reset()` (manual re-arm), `func (g *Guard) DailyLossPct() float64`.

- [ ] **Step 1: Failing test** — `internal/risk/guard_test.go`:
```go
package risk

import (
	"testing"

	"tradebot/internal/config"
)

func guardCfg() config.RiskCfg {
	return config.RiskCfg{DailyLossLimitPct: 3, WeeklyLossLimitPct: 8, HardFlattenDrawdownPct: 6}
}

func TestKillSwitchTripsOnDailyLoss(t *testing.T) {
	g := NewGuard(guardCfg(), 1000)
	g.StartDay(1000)
	g.StartWeek(1000)
	g.Mark(980) // -2% : ok
	if g.Killed() || !g.AllowEntry() {
		t.Fatal("should not be killed at -2%")
	}
	g.Mark(969) // -3.1% : trip
	if !g.Killed() || g.AllowEntry() {
		t.Fatalf("should be killed at -3.1%%, dailyLoss=%.2f", g.DailyLossPct())
	}
}

func TestHardFlattenThreshold(t *testing.T) {
	g := NewGuard(guardCfg(), 1000)
	g.StartDay(1000)
	g.StartWeek(1000)
	g.Mark(945) // -5.5%
	if g.ShouldFlatten() {
		t.Fatal("should not flatten at -5.5%")
	}
	g.Mark(939) // -6.1%
	if !g.ShouldFlatten() {
		t.Fatal("should flatten at -6.1%")
	}
}

func TestResetReArms(t *testing.T) {
	g := NewGuard(guardCfg(), 1000)
	g.StartDay(1000); g.StartWeek(1000)
	g.Mark(900)
	if !g.Killed() { t.Fatal("expected killed") }
	g.Reset()
	if g.Killed() || !g.AllowEntry() { t.Fatal("Reset must re-arm") }
}
```

- [ ] **Step 2: Run** `go test ./internal/risk/ -run Guard` (and Kill/Flatten/Reset) → FAIL.

- [ ] **Step 3: Implement** — `internal/risk/guard.go`:
```go
package risk

import "tradebot/internal/config"

// Guard enforces daily/weekly loss limits, the kill-switch, and the hard-flatten ceiling.
type Guard struct {
	dailyLimit, weeklyLimit, flattenLimit float64
	dayStart, weekStart, cur              float64
	killed                                bool
}

func NewGuard(cfg config.RiskCfg, startEquity float64) *Guard {
	return &Guard{
		dailyLimit:   cfg.DailyLossLimitPct,
		weeklyLimit:  cfg.WeeklyLossLimitPct,
		flattenLimit: cfg.HardFlattenDrawdownPct,
		dayStart:     startEquity,
		weekStart:    startEquity,
		cur:          startEquity,
	}
}

func (g *Guard) StartDay(equity float64)  { g.dayStart = equity }
func (g *Guard) StartWeek(equity float64) { g.weekStart = equity }

func lossPct(start, cur float64) float64 {
	if start <= 0 {
		return 0
	}
	return (start - cur) / start * 100
}

func (g *Guard) DailyLossPct() float64  { return lossPct(g.dayStart, g.cur) }
func (g *Guard) WeeklyLossPct() float64 { return lossPct(g.weekStart, g.cur) }

func (g *Guard) Mark(equity float64) {
	g.cur = equity
	if g.DailyLossPct() >= g.dailyLimit || g.WeeklyLossPct() >= g.weeklyLimit {
		g.killed = true
	}
}

func (g *Guard) AllowEntry() bool    { return !g.killed }
func (g *Guard) Killed() bool        { return g.killed }
func (g *Guard) ShouldFlatten() bool { return g.DailyLossPct() >= g.flattenLimit }
func (g *Guard) Reset()              { g.killed = false }
```

- [ ] **Step 4: Run** `go test ./internal/risk/` → PASS.
- [ ] **Step 5: Commit** `git add internal/risk && git commit -m "feat: add risk guard (loss limits, kill-switch, hard-flatten)"`

---

## Task 10: Candle-aligned post-loss cooldown

**Files:** Create `internal/risk/cooldown.go`, `internal/risk/cooldown_test.go`

**Interfaces:**
- Produces:
  - `type Cooldown struct {...}`, `func NewCooldown(candles int) *Cooldown`
  - `func (c *Cooldown) RecordLoss(symbol string, closeTime int64)` — mark a losing exit at a candle close time.
  - `func (c *Cooldown) Allowed(symbol string, candleIndex int, lossIndex int) bool` — simpler index form below.
  - Concretely, track per-symbol the candle index of the last loss; `func (c *Cooldown) Block(symbol string, lastLossIdx, curIdx int) bool` returns true while `curIdx - lastLossIdx < candles`. Use a per-symbol stored index.

  Final API:
  - `func (c *Cooldown) NoteLoss(symbol string, idx int)`
  - `func (c *Cooldown) Blocked(symbol string, idx int) bool` — true if `idx - lastLoss[symbol] < N` (and a loss was recorded).

- [ ] **Step 1: Failing test** — `internal/risk/cooldown_test.go`:
```go
package risk

import "testing"

func TestCooldownBlocksNCandles(t *testing.T) {
	c := NewCooldown(2)
	if c.Blocked("BTCUSDT", 10) {
		t.Fatal("no loss recorded yet -> not blocked")
	}
	c.NoteLoss("BTCUSDT", 10)
	if !c.Blocked("BTCUSDT", 11) { t.Fatal("idx 11 within 2 candles of loss@10 -> blocked") }
	if !c.Blocked("BTCUSDT", 12) { t.Fatal("idx 12: 12-10=2, still < ...? boundary") }
	if c.Blocked("BTCUSDT", 13) { t.Fatal("idx 13: 13-10=3 >= 2... not blocked") }
}

func TestCooldownPerSymbol(t *testing.T) {
	c := NewCooldown(2)
	c.NoteLoss("BTCUSDT", 5)
	if c.Blocked("ETHUSDT", 6) {
		t.Fatal("ETH has no loss -> not blocked")
	}
}
```

> Cooldown semantics: block while `idx - lastLoss <= N` (so N=2 blocks the loss candle's next two candles; idx 11 and 12 blocked, 13 allowed). Match the asserts above.

- [ ] **Step 2: Run** `go test ./internal/risk/ -run Cooldown` → FAIL.

- [ ] **Step 3: Implement** — `internal/risk/cooldown.go`:
```go
package risk

// Cooldown blocks new entries on a symbol for N closed candles after a losing exit.
type Cooldown struct {
	n        int
	lastLoss map[string]int
}

func NewCooldown(candles int) *Cooldown {
	return &Cooldown{n: candles, lastLoss: map[string]int{}}
}

func (c *Cooldown) NoteLoss(symbol string, idx int) { c.lastLoss[symbol] = idx }

// Blocked is true while idx is within n candles after the last loss (inclusive).
func (c *Cooldown) Blocked(symbol string, idx int) bool {
	last, ok := c.lastLoss[symbol]
	if !ok {
		return false
	}
	return idx-last <= c.n && idx > last
}
```

- [ ] **Step 4: Run** `go test ./internal/risk/` → PASS.
- [ ] **Step 5: Commit** `git add internal/risk && git commit -m "feat: add candle-aligned post-loss cooldown"`

---

## Task 11: Store extensions (signals, P&L snapshots, kill-state)

**Files:** Modify `internal/store/store.go`, `internal/store/store_test.go`

**Interfaces:**
- Produces:
  - `func (s *Store) RecordSignal(sym, strat, action, reason string, ts int64) error`
  - `func (s *Store) RecordPnLSnapshot(ts int64, equity, realized float64) error`
  - `func (s *Store) SaveKillState(killed bool, reason string, ts int64) error` / `func (s *Store) LoadKillState() (bool, error)` — persists/restores the kill-switch so a restart can't bypass it.

- [ ] **Step 1: Failing test** — append to `internal/store/store_test.go`:
```go
func TestKillStatePersists(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil { t.Fatalf("open: %v", err) }
	defer s.Close()
	killed, err := s.LoadKillState()
	if err != nil || killed { t.Fatalf("fresh state must be not-killed, got %v err=%v", killed, err) }
	if err := s.SaveKillState(true, "daily loss", 123); err != nil { t.Fatalf("save: %v", err) }
	killed, err = s.LoadKillState()
	if err != nil || !killed { t.Fatalf("expected killed after save, got %v err=%v", killed, err) }
}

func TestRecordSignalAndSnapshot(t *testing.T) {
	s, _ := Open(":memory:")
	defer s.Close()
	if err := s.RecordSignal("BTCUSDT", "ema_cross_trend", "BUY", "cross", 1); err != nil { t.Fatal(err) }
	if err := s.RecordPnLSnapshot(2, 1000, 5.5); err != nil { t.Fatal(err) }
}
```

- [ ] **Step 2: Run** `go test ./internal/store/` → FAIL.

- [ ] **Step 3: Implement** — extend the schema constant in `internal/store/store.go` (append these tables to the `schema` string):
```sql
CREATE TABLE IF NOT EXISTS signals (
  id INTEGER PRIMARY KEY AUTOINCREMENT, ts INTEGER, symbol TEXT, strategy TEXT, action TEXT, reason TEXT
);
CREATE TABLE IF NOT EXISTS pnl_snapshots (
  id INTEGER PRIMARY KEY AUTOINCREMENT, ts INTEGER, equity REAL, realized REAL
);
CREATE TABLE IF NOT EXISTS kill_state (
  id INTEGER PRIMARY KEY CHECK (id=1), killed INTEGER, reason TEXT, ts INTEGER
);
```
Add methods:
```go
func (s *Store) RecordSignal(sym, strat, action, reason string, ts int64) error {
	_, err := s.db.Exec(`INSERT INTO signals(ts,symbol,strategy,action,reason) VALUES(?,?,?,?,?)`,
		ts, sym, strat, action, reason)
	return err
}

func (s *Store) RecordPnLSnapshot(ts int64, equity, realized float64) error {
	_, err := s.db.Exec(`INSERT INTO pnl_snapshots(ts,equity,realized) VALUES(?,?,?)`, ts, equity, realized)
	return err
}

func (s *Store) SaveKillState(killed bool, reason string, ts int64) error {
	k := 0
	if killed {
		k = 1
	}
	_, err := s.db.Exec(
		`INSERT INTO kill_state(id,killed,reason,ts) VALUES(1,?,?,?)
		 ON CONFLICT(id) DO UPDATE SET killed=excluded.killed, reason=excluded.reason, ts=excluded.ts`,
		k, reason, ts)
	return err
}

func (s *Store) LoadKillState() (bool, error) {
	var k int
	err := s.db.QueryRow(`SELECT killed FROM kill_state WHERE id=1`).Scan(&k)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	return k == 1, nil
}
```
(Ensure `database/sql` is imported — it already is.)

- [ ] **Step 4: Run** `go test ./internal/store/` → PASS.
- [ ] **Step 5: Commit** `git add internal/store && git commit -m "feat: persist signals, P&L snapshots, and kill-switch state"`

---

## Task 12: Multi-symbol backtest integration

**Files:** Create `internal/backtest/multi.go`, `internal/backtest/multi_test.go`

**Interfaces:**
- Produces:
  - `type StratFactory func(symbol string) []strategy.Strategy` — builds the per-symbol strategy set.
  - `func RunMulti(symbols []string, candles map[string][]domain.Candle, mkStrats StratFactory, g *risk.Gate, guard *risk.Guard, cool *risk.Cooldown, f risk.Filters, startCash, feeRate float64) Report` — replays multiple symbols bar-by-bar (aligned by index), classifying regime per symbol, running each symbol's strategies, aggregating via `decision.Choose`, gating with the guard (skip entries when `!guard.AllowEntry()`, flatten when `guard.ShouldFlatten()`) and the cooldown, sizing via the gate, with pessimistic intrabar exits — producing a combined `Report`.

> This is the integration task: it ties together strategies + regime + decision + guard + cooldown + sizing + exit-precedence. Keep one position per symbol (long-only netting). Reuse `CheckExit` and the per-symbol equity accounting from `Run`.

- [ ] **Step 1: Failing test** — `internal/backtest/multi_test.go`:
```go
package backtest

import (
	"testing"

	"tradebot/internal/config"
	"tradebot/internal/domain"
	"tradebot/internal/risk"
	"tradebot/internal/strategy"
)

func rampSym(sym string, n int) []domain.Candle {
	cs := make([]domain.Candle, 0, n)
	price := 100.0
	for i := 0; i < 60; i++ { cs = append(cs, domain.Candle{Symbol: sym, Close: 100, High: 101, Low: 99, Closed: true, CloseTime: int64(i)}) }
	for i := 0; i < n-120; i++ { price = 100 + float64(i)*3; cs = append(cs, domain.Candle{Symbol: sym, Close: price, High: price + 3, Low: price - 3, Closed: true, CloseTime: int64(60 + i)}) }
	for i := 0; i < 60; i++ { p := cs[len(cs)-1].Close - float64(i)*3; cs = append(cs, domain.Candle{Symbol: sym, Close: p, High: p + 3, Low: p - 3, Closed: true, CloseTime: int64(len(cs))}) }
	return cs
}

func TestRunMultiProducesReport(t *testing.T) {
	syms := []string{"BTCUSDT"}
	candles := map[string][]domain.Candle{"BTCUSDT": rampSym("BTCUSDT", 240)}
	mk := func(sym string) []strategy.Strategy {
		return []strategy.Strategy{strategy.NewEMACross(sym, "1h", nil)}
	}
	g := risk.NewGate(config.RiskCfg{MaxPctPerTrade: 90, RiskPerTradePct: 5, MaxOpenPositions: 2, PortfolioMaxDeployedPct: 100, TPRewardMult: 1.6}, 0.003)
	guard := risk.NewGuard(config.RiskCfg{DailyLossLimitPct: 50, WeeklyLossLimitPct: 80, HardFlattenDrawdownPct: 90}, 10000)
	cool := risk.NewCooldown(2)
	rep := RunMulti(syms, candles, mk, g, guard, cool, risk.Filters{StepSize: 0.00001, MinQty: 0.00001, MinNotional: 5}, 10000, 0.0015)
	if rep.FinalEquity <= 0 {
		t.Fatalf("final equity must be positive, got %v", rep.FinalEquity)
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/backtest/ -run RunMulti` → FAIL.

- [ ] **Step 3: Implement** — `internal/backtest/multi.go`:
```go
package backtest

import (
	"tradebot/internal/decision"
	"tradebot/internal/domain"
	"tradebot/internal/regime"
	"tradebot/internal/risk"
	"tradebot/internal/strategy"
)

type StratFactory func(symbol string) []strategy.Strategy

type posState struct {
	order    domain.Order
	entryIdx int
}

// RunMulti replays multiple symbols through strategies + regime + decision + guard + cooldown.
func RunMulti(symbols []string, candles map[string][]domain.Candle, mkStrats StratFactory,
	g *risk.Gate, guard *risk.Guard, cool *risk.Cooldown, f risk.Filters, startCash, feeRate float64) Report {

	cash := startCash
	strats := map[string][]strategy.Strategy{}
	open := map[string]*posState{}
	maxLen := 0
	for _, sym := range symbols {
		strats[sym] = mkStrats(sym)
		if len(candles[sym]) > maxLen {
			maxLen = len(candles[sym])
		}
	}
	var rep Report
	peak := startCash

	markEquity := func(idx int) float64 {
		eq := cash
		for sym, ps := range open {
			cs := candles[sym]
			if idx-1 < len(cs) {
				eq += ps.order.Qty * cs[idx-1].Close
			}
		}
		return eq
	}
	closeTrade := func(sym string, ps *posState, exitPx float64, ts int64, reason string) {
		proceeds := ps.order.Qty*exitPx - feeRate*ps.order.Qty*exitPx
		entryCost := ps.order.Qty*ps.order.Price + feeRate*ps.order.Qty*ps.order.Price
		net := proceeds - entryCost
		cash += proceeds
		rep.Trades = append(rep.Trades, Trade{EntryTime: ps.order.Time, ExitTime: ts, EntryPx: ps.order.Price, ExitPx: exitPx, Qty: ps.order.Qty, NetPnL: net, ExitReason: reason})
		rep.NumTrades++
		rep.NetPnL += net
		if net > 0 {
			rep.Wins++
		}
		delete(open, sym)
	}

	for i := 1; i <= maxLen; i++ {
		guard.Mark(markEquity(i))
		for _, sym := range symbols {
			cs := candles[sym]
			if i > len(cs) {
				continue
			}
			c := cs[i-1]
			hist := cs[:i]

			// 1) Manage open position: intrabar TP/SL from the candle after entry.
			if ps, ok := open[sym]; ok {
				if guard.ShouldFlatten() {
					closeTrade(sym, ps, c.Close, c.CloseTime, "flatten")
				} else if i-1 > ps.entryIdx {
					if kind, px := CheckExit(ps.order, c); kind == ExitStop {
						closeTrade(sym, ps, px, c.CloseTime, "SL")
						cool.NoteLoss(sym, i-1)
					} else if kind == ExitTP {
						closeTrade(sym, ps, px, c.CloseTime, "TP")
					}
				}
			}

			// 2) Strategy candidates + regime + decision.
			_, inPos := open[sym]
			var cands []decision.Candidate
			for _, st := range strats[sym] {
				if sig := st.Evaluate(hist, inPos); sig != nil {
					cands = append(cands, decision.Candidate{Kind: st.Kind(), Signal: *sig})
				}
			}
			reg := regime.Classify(hist)
			sig := decision.Choose(reg, inPos, cands)
			if sig == nil {
				continue
			}
			if sig.Action == domain.Sell {
				if ps, ok := open[sym]; ok {
					closeTrade(sym, ps, c.Close, c.CloseTime, "rule")
				}
				continue
			}
			// Buy: respect guard + cooldown.
			if !guard.AllowEntry() || cool.Blocked(sym, i-1) {
				continue
			}
			acct := risk.Account{Equity: cash, FreeUSDT: cash, OpenPositions: len(open)}
			in := domain.Intent{Symbol: sym, Action: domain.Buy, Price: c.Close, StopDist: sig.StopDist, TPDist: sig.TPDist, Reason: sig.Reason, Time: c.CloseTime}
			if o, err := g.Evaluate(in, acct, f); err == nil {
				cash -= o.Qty*o.Price + feeRate*o.Qty*o.Price
				open[sym] = &posState{order: o, entryIdx: i - 1}
			}
		}

		eq := markEquity(i)
		if eq > peak {
			peak = eq
		}
		if peak > 0 {
			if dd := (peak - eq) / peak * 100; dd > rep.MaxDrawdownPct {
				rep.MaxDrawdownPct = dd
			}
		}
	}

	// Force-close any dangling positions at their last close.
	for sym, ps := range open {
		cs := candles[sym]
		closeTrade(sym, ps, cs[len(cs)-1].Close, cs[len(cs)-1].CloseTime, "eod")
	}
	rep.FinalEquity = cash
	if rep.NumTrades > 0 {
		rep.WinRate = float64(rep.Wins) / float64(rep.NumTrades)
		rep.Expectancy = rep.NetPnL / float64(rep.NumTrades)
	}
	return rep
}
```

- [ ] **Step 4: Run** `go test ./... ` → PASS (whole suite).
- [ ] **Step 5: Commit** `git add internal/backtest && git commit -m "feat: multi-symbol backtest integrating regime, decision, guard, cooldown"`

---

## Self-Review

**Spec coverage (Phase 2):** RSI/MACD/Bollinger → T1–3 ✓. rsi_bb_reversion / donchian_breakout / macd_momentum → T4–6 ✓ (each: ATR stop, vol-aware TP via Gate, warm-up, the spec's exact entry/exit incl. SMA200 hard gate, min-edge, 10-bar channel exit, MACD debounce). Regime incl. Low-Vol-Chop → T7 ✓. Regime gating + same-symbol netting → T8 ✓. Daily/weekly loss + kill-switch + hard-flatten → T9 ✓. Candle-aligned cooldown → T10 ✓. Kill-state persistence + signals/P&L recording → T11 ✓. Multi-symbol integration (real open count via `len(open)`, fixes Phase-0 M4 stub) → T12 ✓.

**Placeholder scan:** none — every step has complete code and tests.

**Type consistency:** `Strategy` gains `Kind() string` (T4) implemented by all four strategies; `regime.Regime`, `decision.Candidate{Kind, Signal}` / `Choose(Regime, bool, []Candidate)`, `risk.Guard` / `risk.Cooldown`, and `backtest.RunMulti(...)` signatures are used identically across tasks. `risk.Gate`/`risk.Account`/`risk.Filters`/`CheckExit`/`Report`/`Trade` reused unchanged from Phase 0–1.
