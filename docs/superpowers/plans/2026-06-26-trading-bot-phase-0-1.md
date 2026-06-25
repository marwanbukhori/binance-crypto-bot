# Trading Bot Phases 0–1 (Walking Skeleton + Backtester) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a runnable Go skeleton that streams candles through a strategy → risk gate → simulated executor → portfolio → SQLite recorder, plus a backtester that replays history through the *same* pipeline and reports performance — so strategies can be validated before any real money.

**Architecture:** A single static Go binary. Domain types are shared; each stage is an interface so backtest/paper/live differ only by data source + executor. Phase 0 proves the pipeline end-to-end against fixture and REST candles with a simulated executor. Phase 1 adds paginated historical backfill, a position manager that enforces pessimistic intrabar TP/SL exit-precedence, and a backtest report with a fee-sensitivity sweep.

**Tech Stack:** Go 1.22, `modernc.org/sqlite` (pure-Go, no CGO → trivial ARM cross-compile), `gopkg.in/yaml.v3`, stdlib `net/http` + `testing`. Float64 for prices/quantities in Phases 0–1 (backtest math is float); live execution (Phase 3+) will adopt `shopspring/decimal` for order precision — out of scope here.

## Global Constraints

- **Spot, long-only.** Quantity is always ≥ 0; a `Sell` only ever reduces an existing position to cash. No shorting/leverage anywhere.
- **No look-ahead.** All indicators (signals AND ATR for stops) use **closed candles only** — never the forming candle. Strategies emit signals on candle close.
- **Pessimistic intrabar fills (backtester).** TP/SL/trailing become active only from the candle *after* entry; when one candle's [low,high] straddles both TP and SL, assume the **stop filled first**.
- **Risk gate is non-bypassable** and logs the binding constraint + effective-risk % on every order. Sizing only ever rounds DOWN (realized risk ≤ budget).
- **Volatility-aware TP:** `tp_dist = tp_reward_mult × stop_dist` (default 1.6), gated to post-fee net R:R ≥ 1.3; reject/warn otherwise.
- **Fee model:** backtest at 0.30% round-trip (majors); support a sweep over 0.20/0.30/0.45%.
- **Module path:** `tradebot`. **Go version:** `go 1.22`.
- **TDD + frequent commits.** Every task: failing test → run (fail) → implement → run (pass) → commit.

---

## File Structure

```
go.mod                              module tradebot, go 1.22
Makefile                            build/test/lint shortcuts
cmd/bot/main.go                     entrypoint, mode dispatch, `backtest` CLI
internal/domain/types.go            Candle, Action, Signal, Intent, Order, Fill, Position
internal/config/config.go           YAML + env loader, defaults, validation
internal/indicators/indicators.go   EMA, SMA, ATR, ADX (pure funcs over []Candle / []float64)
internal/strategy/strategy.go       Strategy interface
internal/strategy/ema_cross.go      ema_cross_trend
internal/risk/risk.go               Gate, position sizing (§6), R:R gate, caps
internal/execution/executor.go      Executor interface
internal/execution/sim.go           SimulatedExecutor (fees)
internal/portfolio/portfolio.go     Portfolio (cash, position, realized/unrealized P&L)
internal/store/store.go             SQLite schema + record/read methods
internal/marketdata/rest.go         Binance REST kline fetch + paginated backfill
internal/engine/engine.go           Phase 0 wiring; position manager (TP/SL exit-precedence)
internal/backtest/backtest.go       replay + Report + fee sweep
testdata/                           fixture candle CSVs, fixture config.yaml
```

Each `internal/<pkg>` has a sibling `_test.go`. Files that change together live together; split is by responsibility, not layer.

---

## Task 1: Project scaffold

**Files:**
- Create: `go.mod`, `Makefile`, `cmd/bot/main.go`, `internal/version/version_test.go`, `internal/version/version.go`

**Interfaces:**
- Consumes: nothing.
- Produces: a buildable module `tradebot`; `Version` constant.

- [ ] **Step 1: Write the failing test**

`internal/version/version_test.go`:
```go
package version

import "testing"

func TestVersionNotEmpty(t *testing.T) {
	if Version == "" {
		t.Fatal("Version must not be empty")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/version/`
Expected: FAIL — `go.mod` missing / `Version` undefined.

- [ ] **Step 3: Create module + minimal files**

`go.mod`:
```
module tradebot

go 1.22
```

`internal/version/version.go`:
```go
package version

// Version is the build version of the bot.
const Version = "0.0.0-dev"
```

`cmd/bot/main.go`:
```go
package main

import (
	"fmt"

	"tradebot/internal/version"
)

func main() {
	fmt.Printf("tradebot %s\n", version.Version)
}
```

`Makefile`:
```makefile
.PHONY: build test vet
build:
	go build -o bin/bot ./cmd/bot
test:
	go test ./...
vet:
	go vet ./...
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./... && go build ./cmd/bot`
Expected: PASS; binary builds.

- [ ] **Step 5: Commit**

```bash
git add go.mod Makefile cmd internal/version
git commit -m "chore: scaffold tradebot go module"
```

---

## Task 2: Domain types

**Files:**
- Create: `internal/domain/types.go`, `internal/domain/types_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `type Action int` with `Hold, Buy, Sell` and `func (a Action) String() string`.
  - `type Candle struct { Symbol, Timeframe string; OpenTime int64; Open, High, Low, Close, Volume float64; CloseTime int64; Closed bool }`
  - `type Signal struct { Symbol string; Action Action; Reason string; StopDist, TPDist float64; Time int64 }`
  - `type Intent struct { Symbol string; Action Action; Reason string; Price, StopDist, TPDist float64; Time int64 }`
  - `type Order struct { Symbol string; Side Action; Qty, Price, StopPrice, TPPrice float64; Type, Reason, BindingConstraint string; EffectiveRiskPct float64; Time int64 }`
  - `type Fill struct { Symbol string; Side Action; Qty, Price, Fee float64; Time int64 }`
  - `type Position struct { Symbol string; Qty, AvgEntry float64 }`

- [ ] **Step 1: Write the failing test**

`internal/domain/types_test.go`:
```go
package domain

import "testing"

func TestActionString(t *testing.T) {
	cases := map[Action]string{Hold: "HOLD", Buy: "BUY", Sell: "SELL"}
	for a, want := range cases {
		if got := a.String(); got != want {
			t.Errorf("Action(%d).String() = %q, want %q", a, got, want)
		}
	}
}

func TestPositionZeroValue(t *testing.T) {
	var p Position
	if p.Qty != 0 || p.AvgEntry != 0 {
		t.Fatal("zero Position must have zero Qty and AvgEntry")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/domain/`
Expected: FAIL — types undefined.

- [ ] **Step 3: Implement types**

`internal/domain/types.go`:
```go
package domain

// Action is a trade direction. Spot is long-only: Sell only reduces a position to cash.
type Action int

const (
	Hold Action = iota
	Buy
	Sell
)

func (a Action) String() string {
	switch a {
	case Buy:
		return "BUY"
	case Sell:
		return "SELL"
	default:
		return "HOLD"
	}
}

// Candle is an OHLCV bar. Times are unix milliseconds.
type Candle struct {
	Symbol, Timeframe              string
	OpenTime                       int64
	Open, High, Low, Close, Volume float64
	CloseTime                      int64
	Closed                         bool
}

// Signal is a strategy's decision on a closed candle. StopDist/TPDist are price-distance hints (0 if none).
type Signal struct {
	Symbol   string
	Action   Action
	Reason   string
	StopDist float64
	TPDist   float64
	Time     int64
}

// Intent is a proposed action handed to the risk gate.
type Intent struct {
	Symbol   string
	Action   Action
	Reason   string
	Price    float64 // reference price (candle close)
	StopDist float64
	TPDist   float64
	Time     int64
}

// Order is a risk-approved instruction for the executor.
type Order struct {
	Symbol            string
	Side              Action
	Qty               float64
	Price             float64
	StopPrice         float64
	TPPrice           float64
	Type              string // "MARKET"
	Reason            string
	BindingConstraint string // "risk" | "notional" | "portfolio" | "free"
	EffectiveRiskPct  float64
	Time              int64
}

// Fill is the executor's report of an executed order.
type Fill struct {
	Symbol string
	Side   Action
	Qty    float64
	Price  float64
	Fee    float64
	Time   int64
}

// Position is the currently held base-asset amount and its average entry price.
type Position struct {
	Symbol   string
	Qty      float64
	AvgEntry float64
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/domain/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/domain
git commit -m "feat: add core domain types"
```

---

## Task 3: Config loader

**Files:**
- Create: `internal/config/config.go`, `internal/config/config_test.go`, `testdata/config.yaml`

**Interfaces:**
- Consumes: nothing.
- Produces: `type Config struct {...}`, `type StrategyCfg struct {...}`, `type RiskCfg struct {...}`; `func Load(path string) (Config, error)`. Secrets read from env (`BINANCE_API_KEY`, `BINANCE_API_SECRET`, `TELEGRAM_BOT_TOKEN`, `TELEGRAM_CHAT_ID`, `DASHBOARD_TOKEN`).

- [ ] **Step 1: Write the failing test**

`testdata/config.yaml`:
```yaml
mode: paper
exchange: { testnet: true }
symbols: [BTCUSDT]
strategies:
  - { name: ema_cross_trend, enabled: true, timeframe: 1h, params: { adx_min: 25 } }
risk:
  max_pct_per_trade: 5
  risk_per_trade_pct: 1
  max_open_positions: 2
  portfolio_max_deployed_pct: 10
  daily_loss_limit_pct: 3
  hard_flatten_drawdown_pct: 6
  weekly_loss_limit_pct: 8
  post_loss_cooldown_candles: 2
  tp_reward_mult: 1.6
  trailing_stop_pct: 0
  fee_model: { majors: 0.30, alts: 0.45, bnb_discount: true }
```

`internal/config/config_test.go`:
```go
package config

import (
	"os"
	"testing"
)

func TestLoadParsesFileAndEnv(t *testing.T) {
	os.Setenv("BINANCE_API_KEY", "k")
	os.Setenv("BINANCE_API_SECRET", "s")
	defer os.Unsetenv("BINANCE_API_KEY")
	defer os.Unsetenv("BINANCE_API_SECRET")

	c, err := Load("../../testdata/config.yaml")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Mode != "paper" || len(c.Symbols) != 1 || c.Symbols[0] != "BTCUSDT" {
		t.Fatalf("bad parse: %+v", c)
	}
	if c.Risk.MaxOpenPositions != 2 || c.Risk.TPRewardMult != 1.6 {
		t.Fatalf("bad risk parse: %+v", c.Risk)
	}
	if c.Secrets.BinanceAPIKey != "k" {
		t.Fatalf("env secret not loaded: %+v", c.Secrets)
	}
	if len(c.Strategies) != 1 || c.Strategies[0].Name != "ema_cross_trend" {
		t.Fatalf("bad strategies: %+v", c.Strategies)
	}
}

func TestLoadRejectsBadMode(t *testing.T) {
	if _, err := Load("../../testdata/missing.yaml"); err == nil {
		t.Fatal("expected error on missing file")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/`
Expected: FAIL — `Load` undefined, and `gopkg.in/yaml.v3` not required.

- [ ] **Step 3: Add dependency + implement**

Run: `go get gopkg.in/yaml.v3@v3.0.1`

`internal/config/config.go`:
```go
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type StrategyCfg struct {
	Name      string             `yaml:"name"`
	Enabled   bool               `yaml:"enabled"`
	Timeframe string             `yaml:"timeframe"`
	Params    map[string]float64 `yaml:"params"`
}

type FeeModel struct {
	Majors       float64 `yaml:"majors"`
	Alts         float64 `yaml:"alts"`
	BNBDiscount  bool    `yaml:"bnb_discount"`
}

type RiskCfg struct {
	MaxPctPerTrade          float64  `yaml:"max_pct_per_trade"`
	RiskPerTradePct         float64  `yaml:"risk_per_trade_pct"`
	MaxOpenPositions        int      `yaml:"max_open_positions"`
	PortfolioMaxDeployedPct float64  `yaml:"portfolio_max_deployed_pct"`
	DailyLossLimitPct       float64  `yaml:"daily_loss_limit_pct"`
	HardFlattenDrawdownPct  float64  `yaml:"hard_flatten_drawdown_pct"`
	WeeklyLossLimitPct      float64  `yaml:"weekly_loss_limit_pct"`
	PostLossCooldownCandles int      `yaml:"post_loss_cooldown_candles"`
	TPRewardMult            float64  `yaml:"tp_reward_mult"`
	TrailingStopPct         float64  `yaml:"trailing_stop_pct"`
	FeeModel                FeeModel `yaml:"fee_model"`
}

type Secrets struct {
	BinanceAPIKey, BinanceAPISecret string
	TelegramBotToken, TelegramChatID string
	DashboardToken                  string
}

type Config struct {
	Mode       string        `yaml:"mode"`
	Exchange   struct{ Testnet bool `yaml:"testnet"` } `yaml:"exchange"`
	Symbols    []string      `yaml:"symbols"`
	Strategies []StrategyCfg `yaml:"strategies"`
	Risk       RiskCfg       `yaml:"risk"`
	Secrets    Secrets       `yaml:"-"`
}

// Load reads YAML config and overlays secrets from environment variables.
func Load(path string) (Config, error) {
	var c Config
	b, err := os.ReadFile(path)
	if err != nil {
		return c, fmt.Errorf("read config: %w", err)
	}
	if err := yaml.Unmarshal(b, &c); err != nil {
		return c, fmt.Errorf("parse config: %w", err)
	}
	switch c.Mode {
	case "backtest", "paper", "live":
	default:
		return c, fmt.Errorf("invalid mode %q (want backtest|paper|live)", c.Mode)
	}
	c.Secrets = Secrets{
		BinanceAPIKey:    os.Getenv("BINANCE_API_KEY"),
		BinanceAPISecret: os.Getenv("BINANCE_API_SECRET"),
		TelegramBotToken: os.Getenv("TELEGRAM_BOT_TOKEN"),
		TelegramChatID:   os.Getenv("TELEGRAM_CHAT_ID"),
		DashboardToken:   os.Getenv("DASHBOARD_TOKEN"),
	}
	return c, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config testdata/config.yaml go.mod go.sum
git commit -m "feat: add YAML+env config loader"
```

---

## Task 4: Indicators — EMA and SMA

**Files:**
- Create: `internal/indicators/indicators.go`, `internal/indicators/indicators_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `func SMA(vals []float64, period int) []float64`, `func EMA(vals []float64, period int) []float64`. Both return a slice the same length as `vals`; positions before warm-up (`period-1`) are `NaN`.

- [ ] **Step 1: Write the failing test**

`internal/indicators/indicators_test.go`:
```go
package indicators

import (
	"math"
	"testing"
)

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func TestSMA(t *testing.T) {
	got := SMA([]float64{1, 2, 3, 4, 5}, 3)
	if !math.IsNaN(got[0]) || !math.IsNaN(got[1]) {
		t.Fatal("first period-1 values must be NaN")
	}
	for i, want := range map[int]float64{2: 2, 3: 3, 4: 4} {
		if !approx(got[i], want) {
			t.Errorf("SMA[%d]=%v want %v", i, got[i], want)
		}
	}
}

func TestEMASeedAndStep(t *testing.T) {
	// EMA(3) seeds at index 2 with SMA of first 3 = 2; multiplier = 2/(3+1)=0.5.
	// index 3: 0.5*4 + 0.5*2 = 3 ; index 4: 0.5*5 + 0.5*3 = 4
	got := EMA([]float64{1, 2, 3, 4, 5}, 3)
	if !approx(got[2], 2) || !approx(got[3], 3) || !approx(got[4], 4) {
		t.Fatalf("EMA seq wrong: %v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/indicators/`
Expected: FAIL — `SMA`/`EMA` undefined.

- [ ] **Step 3: Implement**

`internal/indicators/indicators.go`:
```go
package indicators

import "math"

// SMA returns the simple moving average; indices < period-1 are NaN.
func SMA(vals []float64, period int) []float64 {
	out := make([]float64, len(vals))
	for i := range out {
		out[i] = math.NaN()
	}
	if period <= 0 {
		return out
	}
	var sum float64
	for i, v := range vals {
		sum += v
		if i >= period {
			sum -= vals[i-period]
		}
		if i >= period-1 {
			out[i] = sum / float64(period)
		}
	}
	return out
}

// EMA seeds at index period-1 with the SMA of the first `period` values, then
// applies multiplier 2/(period+1). Indices < period-1 are NaN.
func EMA(vals []float64, period int) []float64 {
	out := make([]float64, len(vals))
	for i := range out {
		out[i] = math.NaN()
	}
	if period <= 0 || len(vals) < period {
		return out
	}
	k := 2.0 / float64(period+1)
	var seed float64
	for i := 0; i < period; i++ {
		seed += vals[i]
	}
	prev := seed / float64(period)
	out[period-1] = prev
	for i := period; i < len(vals); i++ {
		prev = vals[i]*k + prev*(1-k)
		out[i] = prev
	}
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/indicators/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/indicators
git commit -m "feat: add EMA and SMA indicators"
```

---

## Task 5: Indicators — ATR and ADX

**Files:**
- Modify: `internal/indicators/indicators.go`
- Modify: `internal/indicators/indicators_test.go`

**Interfaces:**
- Consumes: `domain.Candle`.
- Produces: `func ATR(cs []domain.Candle, period int) []float64`, `func ADX(cs []domain.Candle, period int) []float64`. Same-length slices; pre-warm-up positions are `NaN`. Both use Wilder's smoothing.

- [ ] **Step 1: Write the failing test**

Append to `internal/indicators/indicators_test.go`:
```go
import "tradebot/internal/domain" // add to the import block

func candles(highs, lows, closes []float64) []domain.Candle {
	cs := make([]domain.Candle, len(closes))
	for i := range closes {
		cs[i] = domain.Candle{High: highs[i], Low: lows[i], Close: closes[i]}
	}
	return cs
}

func TestATRProducesPositiveAfterWarmup(t *testing.T) {
	h := []float64{10, 11, 12, 11, 13, 14, 13, 15}
	l := []float64{9, 9, 10, 9, 11, 12, 11, 13}
	c := []float64{9.5, 10.5, 11.5, 10, 12.5, 13.5, 12, 14.5}
	atr := ATR(candles(h, l, c), 3)
	if !math.IsNaN(atr[1]) {
		t.Fatal("ATR before warmup must be NaN")
	}
	if math.IsNaN(atr[len(atr)-1]) || atr[len(atr)-1] <= 0 {
		t.Fatalf("ATR after warmup must be positive, got %v", atr[len(atr)-1])
	}
}

func TestADXRangeAndTrendDetection(t *testing.T) {
	// Steady uptrend should yield a rising ADX in [0,100].
	n := 40
	h := make([]float64, n); l := make([]float64, n); c := make([]float64, n)
	for i := 0; i < n; i++ {
		base := 100.0 + float64(i) // strict uptrend
		h[i], l[i], c[i] = base+1, base-1, base+0.5
	}
	adx := ADX(candles(h, l, c), 14)
	last := adx[n-1]
	if math.IsNaN(last) || last < 0 || last > 100 {
		t.Fatalf("ADX out of range: %v", last)
	}
	if last < 20 {
		t.Fatalf("strong uptrend should give ADX>=20, got %v", last)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/indicators/`
Expected: FAIL — `ATR`/`ADX` undefined.

- [ ] **Step 3: Implement (Wilder)**

Append to `internal/indicators/indicators.go`:
```go
import "tradebot/internal/domain" // add to the import block (keep "math")

func trueRange(c, prev domain.Candle) float64 {
	hl := c.High - c.Low
	hc := math.Abs(c.High - prev.Close)
	lc := math.Abs(c.Low - prev.Close)
	return math.Max(hl, math.Max(hc, lc))
}

// ATR is Wilder's Average True Range. Indices < period are NaN (needs a prior candle).
func ATR(cs []domain.Candle, period int) []float64 {
	out := make([]float64, len(cs))
	for i := range out {
		out[i] = math.NaN()
	}
	if period <= 0 || len(cs) <= period {
		return out
	}
	var sum float64
	for i := 1; i <= period; i++ {
		sum += trueRange(cs[i], cs[i-1])
	}
	prev := sum / float64(period)
	out[period] = prev
	for i := period + 1; i < len(cs); i++ {
		tr := trueRange(cs[i], cs[i-1])
		prev = (prev*float64(period-1) + tr) / float64(period)
		out[i] = prev
	}
	return out
}

// ADX is Wilder's Average Directional Index in [0,100]. Pre-warm-up positions are NaN.
func ADX(cs []domain.Candle, period int) []float64 {
	out := make([]float64, len(cs))
	for i := range out {
		out[i] = math.NaN()
	}
	n := len(cs)
	if period <= 0 || n <= 2*period {
		return out
	}
	plusDM := make([]float64, n)
	minusDM := make([]float64, n)
	tr := make([]float64, n)
	for i := 1; i < n; i++ {
		up := cs[i].High - cs[i-1].High
		down := cs[i-1].Low - cs[i].Low
		if up > down && up > 0 {
			plusDM[i] = up
		}
		if down > up && down > 0 {
			minusDM[i] = down
		}
		tr[i] = trueRange(cs[i], cs[i-1])
	}
	// Wilder-smoothed sums over `period`, seeded at index `period`.
	var sTR, sP, sM float64
	for i := 1; i <= period; i++ {
		sTR += tr[i]; sP += plusDM[i]; sM += minusDM[i]
	}
	dx := make([]float64, n)
	calcDX := func(sTR, sP, sM float64) float64 {
		if sTR == 0 {
			return 0
		}
		pDI := 100 * sP / sTR
		mDI := 100 * sM / sTR
		if pDI+mDI == 0 {
			return 0
		}
		return 100 * math.Abs(pDI-mDI) / (pDI + mDI)
	}
	dx[period] = calcDX(sTR, sP, sM)
	for i := period + 1; i < n; i++ {
		sTR = sTR - sTR/float64(period) + tr[i]
		sP = sP - sP/float64(period) + plusDM[i]
		sM = sM - sM/float64(period) + minusDM[i]
		dx[i] = calcDX(sTR, sP, sM)
	}
	// ADX = Wilder average of DX over `period`, first value at index 2*period.
	var sumDX float64
	for i := period; i < 2*period; i++ {
		sumDX += dx[i]
	}
	adx := sumDX / float64(period)
	out[2*period-1] = adx
	for i := 2 * period; i < n; i++ {
		adx = (adx*float64(period-1) + dx[i]) / float64(period)
		out[i] = adx
	}
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/indicators/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/indicators
git commit -m "feat: add ATR and ADX (Wilder) indicators"
```

---

## Task 6: Strategy interface + ema_cross_trend

**Files:**
- Create: `internal/strategy/strategy.go`, `internal/strategy/ema_cross.go`, `internal/strategy/ema_cross_test.go`

**Interfaces:**
- Consumes: `domain.Candle`, `domain.Signal`, indicators `EMA`, `ATR`, `ADX`.
- Produces:
  - `type Strategy interface { Name() string; Symbol() string; Timeframe() string; Warmup() int; Evaluate(history []domain.Candle, inPosition bool) *domain.Signal }` — `history` is closed candles ascending; the last element is the newest closed candle.
  - `func NewEMACross(symbol, timeframe string, params map[string]float64) *EMACross`.

- [ ] **Step 1: Write the failing test**

`internal/strategy/ema_cross_test.go`:
```go
package strategy

import (
	"testing"

	"tradebot/internal/domain"
)

func mkCandles(closes []float64) []domain.Candle {
	cs := make([]domain.Candle, len(closes))
	for i, c := range closes {
		cs[i] = domain.Candle{High: c + 1, Low: c - 1, Close: c, Closed: true, CloseTime: int64(i)}
	}
	return cs
}

func TestEMACrossNoSignalBeforeWarmup(t *testing.T) {
	s := NewEMACross("BTCUSDT", "1h", nil)
	short := mkCandles([]float64{1, 2, 3})
	if sig := s.Evaluate(short, false); sig != nil {
		t.Fatalf("expected nil before warmup, got %+v", sig)
	}
}

func TestEMACrossBuysOnUptrendCross(t *testing.T) {
	s := NewEMACross("BTCUSDT", "1h", nil)
	// Long flat then a strong sustained ramp -> EMA9 crosses above EMA21 with ADX>=25.
	closes := make([]float64, 0, 80)
	for i := 0; i < 40; i++ {
		closes = append(closes, 100)
	}
	for i := 0; i < 40; i++ {
		closes = append(closes, 100+float64(i)*3)
	}
	cs := mkCandles(closes)
	var got *domain.Signal
	for i := s.Warmup(); i <= len(cs); i++ {
		if sig := s.Evaluate(cs[:i], false); sig != nil && sig.Action == domain.Buy {
			got = sig
			break
		}
	}
	if got == nil {
		t.Fatal("expected a BUY signal during the uptrend")
	}
	if got.StopDist <= 0 {
		t.Fatalf("BUY must carry an ATR stop distance, got %v", got.StopDist)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/strategy/`
Expected: FAIL — `NewEMACross`/`Strategy` undefined.

- [ ] **Step 3: Implement**

`internal/strategy/strategy.go`:
```go
package strategy

import "tradebot/internal/domain"

// Strategy consumes closed candles (ascending, newest last) and may emit a Signal.
type Strategy interface {
	Name() string
	Symbol() string
	Timeframe() string
	Warmup() int
	Evaluate(history []domain.Candle, inPosition bool) *domain.Signal
}
```

`internal/strategy/ema_cross.go`:
```go
package strategy

import (
	"fmt"
	"math"

	"tradebot/internal/domain"
	"tradebot/internal/indicators"
)

// EMACross is the ema_cross_trend strategy: EMA9>EMA21 cross with ADX>=adxMin.
type EMACross struct {
	symbol, timeframe                  string
	fast, slow, adxPeriod, atrPeriod   int
	adxMin, slATRMult, tpRewardMult    float64
}

func NewEMACross(symbol, timeframe string, p map[string]float64) *EMACross {
	get := func(k string, d float64) float64 {
		if v, ok := p[k]; ok {
			return v
		}
		return d
	}
	return &EMACross{
		symbol: symbol, timeframe: timeframe,
		fast: 9, slow: 21, adxPeriod: 14, atrPeriod: 14,
		adxMin:       get("adx_min", 25),
		slATRMult:    get("sl_atr_mult", 2.0),
		tpRewardMult: get("tp_reward_mult", 1.6),
	}
}

func (s *EMACross) Name() string      { return "ema_cross_trend" }
func (s *EMACross) Symbol() string    { return s.symbol }
func (s *EMACross) Timeframe() string { return s.timeframe }

// Warmup is the longest indicator window: ADX needs 2*period candles.
func (s *EMACross) Warmup() int { return 2*s.adxPeriod + 1 }

func (s *EMACross) Evaluate(h []domain.Candle, inPosition bool) *domain.Signal {
	if len(h) < s.Warmup() {
		return nil
	}
	closes := make([]float64, len(h))
	for i, c := range h {
		closes[i] = c.Close
	}
	emaF := indicators.EMA(closes, s.fast)
	emaS := indicators.EMA(closes, s.slow)
	adx := indicators.ADX(h, s.adxPeriod)
	atr := indicators.ATR(h, s.atrPeriod)
	i := len(h) - 1
	if math.IsNaN(emaF[i]) || math.IsNaN(emaF[i-1]) || math.IsNaN(emaS[i]) || math.IsNaN(emaS[i-1]) || math.IsNaN(adx[i]) || math.IsNaN(atr[i]) {
		return nil
	}
	crossUp := emaF[i-1] <= emaS[i-1] && emaF[i] > emaS[i]
	crossDown := emaF[i-1] >= emaS[i-1] && emaF[i] < emaS[i]
	now := h[i]
	if !inPosition && crossUp && adx[i] >= s.adxMin {
		stop := s.slATRMult * atr[i]
		return &domain.Signal{
			Symbol: s.symbol, Action: domain.Buy, Time: now.CloseTime,
			StopDist: stop, TPDist: s.tpRewardMult * stop,
			Reason: fmt.Sprintf("EMA9>EMA21 cross + ADX %.1f>=%.0f", adx[i], s.adxMin),
		}
	}
	if inPosition && crossDown {
		return &domain.Signal{
			Symbol: s.symbol, Action: domain.Sell, Time: now.CloseTime,
			Reason: "EMA9<EMA21 recross exit",
		}
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/strategy/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/strategy
git commit -m "feat: add Strategy interface and ema_cross_trend"
```

---

## Task 7: Risk gate + position sizing

**Files:**
- Create: `internal/risk/risk.go`, `internal/risk/risk_test.go`

**Interfaces:**
- Consumes: `domain.Intent`, `domain.Order`.
- Produces:
  - `type Account struct { Equity, FreeUSDT, DeployedNotional float64; OpenPositions int; PositionQty float64 }`
  - `type Filters struct { StepSize, MinQty, MinNotional float64 }`
  - `type Gate struct {...}`, `func NewGate(c config.RiskCfg, feeFraction float64) *Gate`
  - `func (g *Gate) Evaluate(in domain.Intent, a Account, f Filters) (domain.Order, error)` — sizes BUY per spec §6; for SELL returns a full-exit order of `a.PositionQty` (exits always allowed). Errors on R:R-gate fail, zero/!positive qty, min-notional fail, max-open-positions, or no free balance.

- [ ] **Step 1: Write the failing test**

`internal/risk/risk_test.go`:
```go
package risk

import (
	"math"
	"testing"

	"tradebot/internal/config"
	"tradebot/internal/domain"
)

func gate() *Gate {
	return NewGate(config.RiskCfg{
		MaxPctPerTrade: 5, RiskPerTradePct: 1, MaxOpenPositions: 2,
		PortfolioMaxDeployedPct: 10, TPRewardMult: 1.6,
	}, 0.003)
}

func TestSizingWorkedExample(t *testing.T) {
	// equity 1000, 1% risk, BTC 60000, stop_dist 1800 (3% wide).
	in := domain.Intent{Symbol: "BTCUSDT", Action: domain.Buy, Price: 60000, StopDist: 1800, TPDist: 2880}
	a := Account{Equity: 1000, FreeUSDT: 1000}
	f := Filters{StepSize: 0.000001, MinQty: 0.00001, MinNotional: 10}
	o, err := gate().Evaluate(in, a, f)
	if err != nil {
		t.Fatalf("unexpected reject: %v", err)
	}
	if math.Abs(o.Qty-0.000833) > 1e-6 {
		t.Fatalf("qty=%v want ~0.000833", o.Qty)
	}
	if o.BindingConstraint != "notional" {
		t.Fatalf("binding=%q want notional", o.BindingConstraint)
	}
	if math.Abs(o.EffectiveRiskPct-0.15) > 0.02 {
		t.Fatalf("effective risk=%v%% want ~0.15%%", o.EffectiveRiskPct)
	}
}

func TestRejectsBelowMinNotional(t *testing.T) {
	in := domain.Intent{Symbol: "BTCUSDT", Action: domain.Buy, Price: 60000, StopDist: 1800, TPDist: 2880}
	a := Account{Equity: 1000, FreeUSDT: 1000}
	f := Filters{StepSize: 0.000001, MinQty: 0.00001, MinNotional: 100} // 50 USDT notional < 100
	if _, err := gate().Evaluate(in, a, f); err == nil {
		t.Fatal("expected reject below MIN_NOTIONAL")
	}
}

func TestRejectsBadRiskReward(t *testing.T) {
	// TP barely above stop -> post-fee R:R < 1.3.
	in := domain.Intent{Symbol: "BTCUSDT", Action: domain.Buy, Price: 60000, StopDist: 1800, TPDist: 1900}
	a := Account{Equity: 1000, FreeUSDT: 1000}
	f := Filters{StepSize: 0.000001, MinQty: 0.00001, MinNotional: 10}
	if _, err := gate().Evaluate(in, a, f); err == nil {
		t.Fatal("expected reject on poor R:R")
	}
}

func TestSellExitsFullPosition(t *testing.T) {
	in := domain.Intent{Symbol: "BTCUSDT", Action: domain.Sell, Price: 61000}
	a := Account{Equity: 1000, FreeUSDT: 500, PositionQty: 0.000833, OpenPositions: 1}
	f := Filters{StepSize: 0.000001, MinQty: 0.00001, MinNotional: 10}
	o, err := gate().Evaluate(in, a, f)
	if err != nil {
		t.Fatalf("sell rejected: %v", err)
	}
	if o.Side != domain.Sell || math.Abs(o.Qty-0.000833) > 1e-9 {
		t.Fatalf("sell qty=%v want full 0.000833", o.Qty)
	}
}

func TestRejectsWhenMaxPositionsReached(t *testing.T) {
	in := domain.Intent{Symbol: "ETHUSDT", Action: domain.Buy, Price: 3000, StopDist: 90, TPDist: 144}
	a := Account{Equity: 1000, FreeUSDT: 1000, OpenPositions: 2}
	f := Filters{StepSize: 0.0001, MinQty: 0.001, MinNotional: 10}
	if _, err := gate().Evaluate(in, a, f); err == nil {
		t.Fatal("expected reject at max open positions")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/risk/`
Expected: FAIL — `Gate` undefined.

- [ ] **Step 3: Implement**

`internal/risk/risk.go`:
```go
package risk

import (
	"fmt"
	"math"

	"tradebot/internal/config"
	"tradebot/internal/domain"
)

type Account struct {
	Equity, FreeUSDT, DeployedNotional float64
	OpenPositions                      int
	PositionQty                        float64
}

type Filters struct{ StepSize, MinQty, MinNotional float64 }

type Gate struct {
	cfg config.RiskCfg
	fee float64 // round-trip fraction for the R:R gate, e.g. 0.003
	rr  float64 // min post-fee reward:risk
}

func NewGate(c config.RiskCfg, feeFraction float64) *Gate {
	return &Gate{cfg: c, fee: feeFraction, rr: 1.3}
}

func floorTo(v, step float64) float64 {
	if step <= 0 {
		return v
	}
	return math.Floor(v/step) * step
}

func (g *Gate) Evaluate(in domain.Intent, a Account, f Filters) (domain.Order, error) {
	if in.Action == domain.Sell {
		if a.PositionQty <= 0 {
			return domain.Order{}, fmt.Errorf("sell with no position")
		}
		qty := floorTo(a.PositionQty, f.StepSize)
		return domain.Order{
			Symbol: in.Symbol, Side: domain.Sell, Qty: qty, Price: in.Price,
			Type: "MARKET", Reason: in.Reason, BindingConstraint: "exit", Time: in.Time,
		}, nil
	}

	if a.OpenPositions >= g.cfg.MaxOpenPositions {
		return domain.Order{}, fmt.Errorf("max open positions (%d) reached", g.cfg.MaxOpenPositions)
	}
	if in.StopDist <= 0 || in.Price <= 0 {
		return domain.Order{}, fmt.Errorf("buy needs positive price and stop distance")
	}

	// Post-fee net reward:risk gate.
	feeAbs := g.fee * in.Price
	netRR := (in.TPDist - feeAbs) / (in.StopDist + feeAbs)
	if netRR < g.rr {
		return domain.Order{}, fmt.Errorf("post-fee R:R %.2f < %.2f", netRR, g.rr)
	}

	riskUSDT := a.Equity * g.cfg.RiskPerTradePct / 100
	qtyRisk := riskUSDT / in.StopDist
	qtyCap := (a.Equity * g.cfg.MaxPctPerTrade / 100) / in.Price
	roomPortfolio := a.Equity*g.cfg.PortfolioMaxDeployedPct/100 - a.DeployedNotional
	qtyPortfolio := math.Max(0, roomPortfolio) / in.Price
	qtyFree := (a.FreeUSDT * 0.995) / in.Price

	qty := qtyRisk
	binding := "risk"
	if qtyCap < qty {
		qty, binding = qtyCap, "notional"
	}
	if qtyPortfolio < qty {
		qty, binding = qtyPortfolio, "portfolio"
	}
	if qtyFree < qty {
		qty, binding = qtyFree, "free"
	}

	qty = floorTo(qty, f.StepSize)
	if qty < f.MinQty || qty <= 0 {
		return domain.Order{}, fmt.Errorf("qty %.8f below MinQty %.8f", qty, f.MinQty)
	}
	if qty*in.Price < f.MinNotional*1.01 {
		return domain.Order{}, fmt.Errorf("notional %.2f below MinNotional %.2f", qty*in.Price, f.MinNotional)
	}

	effRisk := qty * in.StopDist / a.Equity * 100
	return domain.Order{
		Symbol: in.Symbol, Side: domain.Buy, Qty: qty, Price: in.Price,
		StopPrice: in.Price - in.StopDist, TPPrice: in.Price + in.TPDist,
		Type: "MARKET", Reason: in.Reason, BindingConstraint: binding,
		EffectiveRiskPct: effRisk, Time: in.Time,
	}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/risk/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/risk
git commit -m "feat: add non-bypassable risk gate with position sizing"
```

---

## Task 8: Simulated executor

**Files:**
- Create: `internal/execution/executor.go`, `internal/execution/sim.go`, `internal/execution/sim_test.go`

**Interfaces:**
- Consumes: `domain.Order`, `domain.Candle`, `domain.Fill`.
- Produces:
  - `type Executor interface { Execute(o domain.Order, c domain.Candle) (domain.Fill, error) }`
  - `func NewSimulated(feeRate float64) *Simulated` (feeRate is per-side fraction, e.g. 0.0015). Fills at the candle close × (1 ± slippage=0); fee = `feeRate × qty × price`.

- [ ] **Step 1: Write the failing test**

`internal/execution/sim_test.go`:
```go
package execution

import (
	"math"
	"testing"

	"tradebot/internal/domain"
)

func TestSimulatedFillsAtCloseWithFee(t *testing.T) {
	ex := NewSimulated(0.0015)
	o := domain.Order{Symbol: "BTCUSDT", Side: domain.Buy, Qty: 0.001, Price: 60000}
	c := domain.Candle{Close: 60100}
	fill, err := ex.Execute(o, c)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if fill.Price != 60100 {
		t.Fatalf("fill price=%v want candle close 60100", fill.Price)
	}
	wantFee := 0.0015 * 0.001 * 60100
	if math.Abs(fill.Fee-wantFee) > 1e-9 {
		t.Fatalf("fee=%v want %v", fill.Fee, wantFee)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/execution/`
Expected: FAIL — `NewSimulated` undefined.

- [ ] **Step 3: Implement**

`internal/execution/executor.go`:
```go
package execution

import "tradebot/internal/domain"

// Executor turns an approved Order into a Fill against a candle.
type Executor interface {
	Execute(o domain.Order, c domain.Candle) (domain.Fill, error)
}
```

`internal/execution/sim.go`:
```go
package execution

import "tradebot/internal/domain"

// Simulated fills market orders at the candle close and models a per-side fee.
type Simulated struct{ feeRate float64 }

func NewSimulated(feeRate float64) *Simulated { return &Simulated{feeRate: feeRate} }

func (s *Simulated) Execute(o domain.Order, c domain.Candle) (domain.Fill, error) {
	price := c.Close
	return domain.Fill{
		Symbol: o.Symbol, Side: o.Side, Qty: o.Qty, Price: price,
		Fee: s.feeRate * o.Qty * price, Time: c.CloseTime,
	}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/execution/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/execution
git commit -m "feat: add simulated executor with fee model"
```

---

## Task 9: Portfolio tracker

**Files:**
- Create: `internal/portfolio/portfolio.go`, `internal/portfolio/portfolio_test.go`

**Interfaces:**
- Consumes: `domain.Fill`.
- Produces:
  - `type Portfolio struct {...}`, `func New(cashUSDT float64) *Portfolio`
  - `func (p *Portfolio) Apply(f domain.Fill)` — buys reduce cash (incl. fee) and grow the position at avg entry; sells add proceeds (minus fee) and realize P&L.
  - `func (p *Portfolio) Cash() float64`, `func (p *Portfolio) Position(symbol string) domain.Position`, `func (p *Portfolio) Equity(mark map[string]float64) float64`, `func (p *Portfolio) Realized() float64`.

- [ ] **Step 1: Write the failing test**

`internal/portfolio/portfolio_test.go`:
```go
package portfolio

import (
	"math"
	"testing"

	"tradebot/internal/domain"
)

func TestBuyThenSellRealizesPnL(t *testing.T) {
	p := New(1000)
	p.Apply(domain.Fill{Symbol: "BTCUSDT", Side: domain.Buy, Qty: 0.01, Price: 60000, Fee: 0.9})
	if math.Abs(p.Cash()-(1000-600-0.9)) > 1e-9 {
		t.Fatalf("cash after buy=%v", p.Cash())
	}
	if pos := p.Position("BTCUSDT"); math.Abs(pos.Qty-0.01) > 1e-12 || pos.AvgEntry != 60000 {
		t.Fatalf("position wrong: %+v", pos)
	}
	p.Apply(domain.Fill{Symbol: "BTCUSDT", Side: domain.Sell, Qty: 0.01, Price: 61000, Fee: 0.915})
	// realized = (61000-60000)*0.01 - buyFee - sellFee = 10 - 0.9 - 0.915 = 8.185
	if math.Abs(p.Realized()-8.185) > 1e-6 {
		t.Fatalf("realized=%v want 8.185", p.Realized())
	}
	if pos := p.Position("BTCUSDT"); pos.Qty != 0 {
		t.Fatalf("expected flat, got %+v", pos)
	}
}

func TestEquityMarksOpenPosition(t *testing.T) {
	p := New(1000)
	p.Apply(domain.Fill{Symbol: "BTCUSDT", Side: domain.Buy, Qty: 0.01, Price: 60000, Fee: 0})
	eq := p.Equity(map[string]float64{"BTCUSDT": 62000})
	// cash 400 + 0.01*62000 = 400 + 620 = 1020
	if math.Abs(eq-1020) > 1e-9 {
		t.Fatalf("equity=%v want 1020", eq)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/portfolio/`
Expected: FAIL — `New` undefined.

- [ ] **Step 3: Implement**

`internal/portfolio/portfolio.go`:
```go
package portfolio

import "tradebot/internal/domain"

type Portfolio struct {
	cash     float64
	realized float64
	pos      map[string]domain.Position
}

func New(cashUSDT float64) *Portfolio {
	return &Portfolio{cash: cashUSDT, pos: map[string]domain.Position{}}
}

func (p *Portfolio) Apply(f domain.Fill) {
	pos := p.pos[f.Symbol]
	switch f.Side {
	case domain.Buy:
		newQty := pos.Qty + f.Qty
		if newQty > 0 {
			pos.AvgEntry = (pos.AvgEntry*pos.Qty + f.Price*f.Qty) / newQty
		}
		pos.Qty = newQty
		pos.Symbol = f.Symbol
		p.cash -= f.Price*f.Qty + f.Fee
	case domain.Sell:
		p.realized += (f.Price-pos.AvgEntry)*f.Qty - f.Fee
		pos.Qty -= f.Qty
		p.cash += f.Price*f.Qty - f.Fee
		if pos.Qty <= 1e-12 {
			pos = domain.Position{Symbol: f.Symbol}
		}
	}
	p.pos[f.Symbol] = pos
}

func (p *Portfolio) Cash() float64     { return p.cash }
func (p *Portfolio) Realized() float64 { return p.realized }

func (p *Portfolio) Position(symbol string) domain.Position { return p.pos[symbol] }

func (p *Portfolio) Equity(mark map[string]float64) float64 {
	eq := p.cash
	for sym, pos := range p.pos {
		eq += pos.Qty * mark[sym]
	}
	return eq
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/portfolio/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/portfolio
git commit -m "feat: add portfolio tracker with realized P&L"
```

---

## Task 10: SQLite store

**Files:**
- Create: `internal/store/store.go`, `internal/store/store_test.go`

**Interfaces:**
- Consumes: `domain.Order`, `domain.Fill`.
- Produces:
  - `func Open(path string) (*Store, error)` (creates schema if absent; `path` may be `:memory:` or a file), `func (s *Store) Close() error`
  - `func (s *Store) RecordOrder(o domain.Order) error`
  - `func (s *Store) RecordFill(f domain.Fill) error`
  - `func (s *Store) CountFills() (int, error)` (for tests/dashboards).

- [ ] **Step 1: Write the failing test**

`internal/store/store_test.go`:
```go
package store

import (
	"testing"

	"tradebot/internal/domain"
)

func TestRecordAndCount(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()

	if err := s.RecordOrder(domain.Order{Symbol: "BTCUSDT", Side: domain.Buy, Qty: 0.01, Price: 60000, BindingConstraint: "notional", EffectiveRiskPct: 0.15}); err != nil {
		t.Fatalf("record order: %v", err)
	}
	if err := s.RecordFill(domain.Fill{Symbol: "BTCUSDT", Side: domain.Buy, Qty: 0.01, Price: 60000, Fee: 0.9}); err != nil {
		t.Fatalf("record fill: %v", err)
	}
	n, err := s.CountFills()
	if err != nil || n != 1 {
		t.Fatalf("CountFills=%d err=%v want 1", n, err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/`
Expected: FAIL — `Open` undefined, `modernc.org/sqlite` not required.

- [ ] **Step 3: Add dependency + implement**

Run: `go get modernc.org/sqlite@latest`

`internal/store/store.go`:
```go
package store

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"

	"tradebot/internal/domain"
)

type Store struct{ db *sql.DB }

const schema = `
CREATE TABLE IF NOT EXISTS orders (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  ts INTEGER, symbol TEXT, side TEXT, qty REAL, price REAL,
  stop_price REAL, tp_price REAL, reason TEXT, binding TEXT, eff_risk_pct REAL
);
CREATE TABLE IF NOT EXISTS fills (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  ts INTEGER, symbol TEXT, side TEXT, qty REAL, price REAL, fee REAL
);
`

func Open(path string) (*Store, error) {
	dsn := path
	if path != ":memory:" {
		dsn = "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("schema: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) RecordOrder(o domain.Order) error {
	_, err := s.db.Exec(
		`INSERT INTO orders(ts,symbol,side,qty,price,stop_price,tp_price,reason,binding,eff_risk_pct)
		 VALUES(?,?,?,?,?,?,?,?,?,?)`,
		o.Time, o.Symbol, o.Side.String(), o.Qty, o.Price, o.StopPrice, o.TPPrice, o.Reason, o.BindingConstraint, o.EffectiveRiskPct)
	return err
}

func (s *Store) RecordFill(f domain.Fill) error {
	_, err := s.db.Exec(
		`INSERT INTO fills(ts,symbol,side,qty,price,fee) VALUES(?,?,?,?,?,?)`,
		f.Time, f.Symbol, f.Side.String(), f.Qty, f.Price, f.Fee)
	return err
}

func (s *Store) CountFills() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM fills`).Scan(&n)
	return n, err
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store go.mod go.sum
git commit -m "feat: add SQLite store for orders and fills"
```

---

## Task 11: Engine wiring + end-to-end skeleton test

**Files:**
- Create: `internal/engine/engine.go`, `internal/engine/engine_test.go`

**Interfaces:**
- Consumes: `strategy.Strategy`, `risk.Gate`, `risk.Filters`, `execution.Executor`, `portfolio.Portfolio`, `store.Store`, `domain.Candle`.
- Produces:
  - `type Engine struct {...}`, `func New(s strategy.Strategy, g *risk.Gate, ex execution.Executor, pf *portfolio.Portfolio, st *store.Store, f risk.Filters) *Engine`
  - `func (e *Engine) OnClosedCandle(c domain.Candle, history []domain.Candle) error` — runs strategy → (size via gate) → execute → portfolio.Apply → record. Maintains in-position state via the portfolio.

- [ ] **Step 1: Write the failing test**

`internal/engine/engine_test.go`:
```go
package engine

import (
	"testing"

	"tradebot/internal/domain"
	"tradebot/internal/execution"
	"tradebot/internal/portfolio"
	"tradebot/internal/risk"
	"tradebot/internal/config"
	"tradebot/internal/store"
	"tradebot/internal/strategy"
)

func mkCandles(closes []float64) []domain.Candle {
	cs := make([]domain.Candle, len(closes))
	for i, c := range closes {
		cs[i] = domain.Candle{Symbol: "BTCUSDT", High: c + 5, Low: c - 5, Close: c, Closed: true, CloseTime: int64(i)}
	}
	return cs
}

func TestEndToEndBuyThenSellRecords(t *testing.T) {
	st, _ := store.Open(":memory:")
	defer st.Close()
	g := risk.NewGate(config.RiskCfg{MaxPctPerTrade: 50, RiskPerTradePct: 5, MaxOpenPositions: 2, PortfolioMaxDeployedPct: 100, TPRewardMult: 1.6}, 0.003)
	pf := portfolio.New(10000)
	s := strategy.NewEMACross("BTCUSDT", "1h", nil)
	e := New(s, g, execution.NewSimulated(0.0015), pf, st, risk.Filters{StepSize: 0.00001, MinQty: 0.00001, MinNotional: 5})

	// ramp up (triggers BUY) then ramp down (triggers SELL recross).
	closes := []float64{}
	for i := 0; i < 40; i++ { closes = append(closes, 100) }
	for i := 0; i < 40; i++ { closes = append(closes, 100+float64(i)*4) }
	for i := 0; i < 40; i++ { closes = append(closes, 260-float64(i)*4) }
	cs := mkCandles(closes)

	for i := 1; i <= len(cs); i++ {
		if err := e.OnClosedCandle(cs[i-1], cs[:i]); err != nil {
			t.Fatalf("OnClosedCandle: %v", err)
		}
	}
	n, _ := st.CountFills()
	if n < 2 {
		t.Fatalf("expected >=2 fills (a buy and a sell), got %d", n)
	}
	if pf.Position("BTCUSDT").Qty != 0 {
		t.Fatalf("expected flat at end, got %+v", pf.Position("BTCUSDT"))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/engine/`
Expected: FAIL — `New`/`Engine` undefined.

- [ ] **Step 3: Implement**

`internal/engine/engine.go`:
```go
package engine

import (
	"tradebot/internal/domain"
	"tradebot/internal/execution"
	"tradebot/internal/portfolio"
	"tradebot/internal/risk"
	"tradebot/internal/store"
	"tradebot/internal/strategy"
)

type Engine struct {
	strat strategy.Strategy
	gate  *risk.Gate
	exec  execution.Executor
	pf    *portfolio.Portfolio
	store *store.Store
	filt  risk.Filters
}

func New(s strategy.Strategy, g *risk.Gate, ex execution.Executor, pf *portfolio.Portfolio, st *store.Store, f risk.Filters) *Engine {
	return &Engine{strat: s, gate: g, exec: ex, pf: pf, store: st, filt: f}
}

// OnClosedCandle drives one tick of the pipeline for a just-closed candle.
// history is closed candles ascending, newest last (== c).
func (e *Engine) OnClosedCandle(c domain.Candle, history []domain.Candle) error {
	pos := e.pf.Position(c.Symbol)
	inPos := pos.Qty > 0
	sig := e.strat.Evaluate(history, inPos)
	if sig == nil {
		return nil
	}
	acct := risk.Account{
		Equity:        e.pf.Equity(map[string]float64{c.Symbol: c.Close}),
		FreeUSDT:      e.pf.Cash(),
		PositionQty:   pos.Qty,
		OpenPositions: e.openCount(),
	}
	in := domain.Intent{
		Symbol: sig.Symbol, Action: sig.Action, Reason: sig.Reason,
		Price: c.Close, StopDist: sig.StopDist, TPDist: sig.TPDist, Time: c.CloseTime,
	}
	order, err := e.gate.Evaluate(in, acct, e.filt)
	if err != nil {
		return nil // rejected: logged by caller in real engine; skip for skeleton
	}
	if err := e.store.RecordOrder(order); err != nil {
		return err
	}
	fill, err := e.exec.Execute(order, c)
	if err != nil {
		return err
	}
	e.pf.Apply(fill)
	return e.store.RecordFill(fill)
}

func (e *Engine) openCount() int {
	// Skeleton tracks a single symbol; count is 1 if that position is open.
	// Generalized in Phase 2's multi-symbol engine.
	if e.pf.Position("BTCUSDT").Qty > 0 {
		return 1
	}
	return 0
}
```

> Note: `openCount` is a Phase-0 single-symbol stub; Phase 2 generalizes the engine to many symbols and a real open-position count. Captured in the spec's Phase 2 scope.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/engine/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/engine
git commit -m "feat: wire strategy->risk->exec->portfolio->store pipeline"
```

---

## Task 12: Binance REST candle source + main entrypoint

**Files:**
- Create: `internal/marketdata/rest.go`, `internal/marketdata/rest_test.go`
- Modify: `cmd/bot/main.go`

**Interfaces:**
- Consumes: `domain.Candle`, `config.Config`.
- Produces:
  - `type Client struct {...}`, `func NewClient(testnet bool) *Client`, `func (c *Client) WithHTTP(h *http.Client, base string) *Client`
  - `func (c *Client) Klines(symbol, interval string, limit int) ([]domain.Candle, error)` — parses Binance's `/api/v3/klines` array response into closed `domain.Candle`s.
  - `func parseKline(symbol, interval string, raw []any) (domain.Candle, error)` (unexported; tested via an injected fake server).

- [ ] **Step 1: Write the failing test**

`internal/marketdata/rest_test.go`:
```go
package marketdata

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestKlinesParsesBinanceArray(t *testing.T) {
	body := `[
	  [1609459200000,"29000.0","29500.0","28900.0","29400.0","123.4",1609462799999,"x",10,"x","x","0"],
	  [1609462800000,"29400.0","29800.0","29300.0","29750.0","98.7", 1609466399999,"x",12,"x","x","0"]
	]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	}))
	defer srv.Close()

	c := NewClient(false).WithHTTP(srv.Client(), srv.URL)
	cs, err := c.Klines("BTCUSDT", "1h", 2)
	if err != nil {
		t.Fatalf("Klines: %v", err)
	}
	if len(cs) != 2 {
		t.Fatalf("want 2 candles, got %d", len(cs))
	}
	if cs[0].Open != 29000 || cs[0].High != 29500 || cs[0].Low != 28900 || cs[0].Close != 29400 {
		t.Fatalf("bad OHLC: %+v", cs[0])
	}
	if !cs[0].Closed || cs[0].Symbol != "BTCUSDT" {
		t.Fatalf("expected closed candle for BTCUSDT: %+v", cs[0])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/marketdata/`
Expected: FAIL — `NewClient` undefined.

- [ ] **Step 3: Implement**

`internal/marketdata/rest.go`:
```go
package marketdata

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"tradebot/internal/domain"
)

const (
	prodBase    = "https://api.binance.com"
	testnetBase = "https://testnet.binance.vision"
)

type Client struct {
	http *http.Client
	base string
}

func NewClient(testnet bool) *Client {
	base := prodBase
	if testnet {
		base = testnetBase
	}
	return &Client{http: &http.Client{Timeout: 15 * time.Second}, base: base}
}

func (c *Client) WithHTTP(h *http.Client, base string) *Client {
	c.http = h
	c.base = base
	return c
}

func parseKline(symbol, interval string, raw []any) (domain.Candle, error) {
	if len(raw) < 7 {
		return domain.Candle{}, fmt.Errorf("kline has %d fields", len(raw))
	}
	num := func(v any) (float64, error) {
		s, ok := v.(string)
		if !ok {
			return 0, fmt.Errorf("want string num, got %T", v)
		}
		return strconv.ParseFloat(s, 64)
	}
	openTime, _ := raw[0].(float64)
	closeTime, _ := raw[6].(float64)
	o, e1 := num(raw[1])
	h, e2 := num(raw[2])
	l, e3 := num(raw[3])
	cl, e4 := num(raw[4])
	vol, e5 := num(raw[5])
	for _, e := range []error{e1, e2, e3, e4, e5} {
		if e != nil {
			return domain.Candle{}, e
		}
	}
	return domain.Candle{
		Symbol: symbol, Timeframe: interval,
		OpenTime: int64(openTime), Open: o, High: h, Low: l, Close: cl, Volume: vol,
		CloseTime: int64(closeTime), Closed: true,
	}, nil
}

func (c *Client) Klines(symbol, interval string, limit int) ([]domain.Candle, error) {
	url := fmt.Sprintf("%s/api/v3/klines?symbol=%s&interval=%s&limit=%d", c.base, symbol, interval, limit)
	resp, err := c.http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("klines GET: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("klines status %d", resp.StatusCode)
	}
	var rows [][]any
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		return nil, fmt.Errorf("decode klines: %w", err)
	}
	out := make([]domain.Candle, 0, len(rows))
	for _, r := range rows {
		cd, err := parseKline(symbol, interval, r)
		if err != nil {
			return nil, err
		}
		out = append(out, cd)
	}
	return out, nil
}
```

Replace `cmd/bot/main.go`:
```go
package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"tradebot/internal/config"
	"tradebot/internal/marketdata"
	"tradebot/internal/version"
)

func main() {
	cfgPath := flag.String("config", "config.yaml", "path to config.yaml")
	flag.Parse()

	if len(flag.Args()) > 0 && flag.Arg(0) == "version" {
		fmt.Printf("tradebot %s\n", version.Version)
		return
	}

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	if len(cfg.Symbols) == 0 {
		log.Fatal("no symbols configured")
	}
	client := marketdata.NewClient(cfg.Exchange.Testnet)
	cs, err := client.Klines(cfg.Symbols[0], "1h", 5)
	if err != nil {
		fmt.Fprintf(os.Stderr, "klines fetch failed (offline is OK in dev): %v\n", err)
		return
	}
	fmt.Printf("tradebot %s — fetched %d candles for %s; last close=%.2f\n",
		version.Version, len(cs), cfg.Symbols[0], cs[len(cs)-1].Close)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/marketdata/ && go build ./cmd/bot`
Expected: PASS; binary builds.

- [ ] **Step 5: Commit**

```bash
git add internal/marketdata cmd/bot
git commit -m "feat: add Binance REST kline client and wire main"
```

---

## Task 13: Historical backfill (paginated klines)

**Files:**
- Modify: `internal/marketdata/rest.go`
- Modify: `internal/marketdata/rest_test.go`

**Interfaces:**
- Consumes: existing `Client`.
- Produces: `func (c *Client) Backfill(symbol, interval string, startMs, endMs int64) ([]domain.Candle, error)` — pages through `/api/v3/klines` using `startTime`/`endTime`/`limit=1000`, advancing past the last `CloseTime`, deduping on `OpenTime`, until `endMs` is reached or a page returns < 2 rows.

- [ ] **Step 1: Write the failing test**

Append to `internal/marketdata/rest_test.go` (add `"fmt"` to the existing import block):
```go
func TestBackfillPaginates(t *testing.T) {
	// Server returns 2 candles per page, advancing by startTime, then an empty page.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := r.URL.Query().Get("startTime")
		switch start {
		case "0":
			fmt.Fprint(w, `[[0,"1","2","0","1.5","1",59999,"x",1,"x","x","0"],[60000,"1.5","2.5","1","2","1",119999,"x",1,"x","x","0"]]`)
		case "120000":
			fmt.Fprint(w, `[[120000,"2","3","1.5","2.5","1",179999,"x",1,"x","x","0"]]`)
		default:
			fmt.Fprint(w, `[]`)
		}
	}))
	defer srv.Close()
	c := NewClient(false).WithHTTP(srv.Client(), srv.URL)
	cs, err := c.Backfill("BTCUSDT", "1m", 0, 200000)
	if err != nil {
		t.Fatalf("Backfill: %v", err)
	}
	if len(cs) != 3 {
		t.Fatalf("want 3 deduped candles, got %d", len(cs))
	}
	if cs[0].OpenTime != 0 || cs[2].OpenTime != 120000 {
		t.Fatalf("bad pagination order: %+v", cs)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/marketdata/ -run Backfill`
Expected: FAIL — `Backfill` undefined.

- [ ] **Step 3: Implement**

Append to `internal/marketdata/rest.go`:
```go
// Backfill pages klines in [startMs, endMs] and returns them deduped and ordered.
func (c *Client) Backfill(symbol, interval string, startMs, endMs int64) ([]domain.Candle, error) {
	var out []domain.Candle
	seen := map[int64]bool{}
	cur := startMs
	for cur <= endMs {
		url := fmt.Sprintf("%s/api/v3/klines?symbol=%s&interval=%s&startTime=%d&endTime=%d&limit=1000",
			c.base, symbol, interval, cur, endMs)
		resp, err := c.http.Get(url)
		if err != nil {
			return nil, fmt.Errorf("backfill GET: %w", err)
		}
		var rows [][]any
		err = json.NewDecoder(resp.Body).Decode(&rows)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("backfill decode: %w", err)
		}
		if len(rows) == 0 {
			break
		}
		var lastClose int64
		for _, r := range rows {
			cd, err := parseKline(symbol, interval, r)
			if err != nil {
				return nil, err
			}
			if !seen[cd.OpenTime] {
				seen[cd.OpenTime] = true
				out = append(out, cd)
			}
			lastClose = cd.CloseTime
		}
		if len(rows) < 2 {
			break
		}
		cur = lastClose + 1
	}
	return out, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/marketdata/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/marketdata
git commit -m "feat: add paginated historical kline backfill"
```

---

## Task 14: Position manager — pessimistic intrabar TP/SL exit-precedence

**Files:**
- Create: `internal/backtest/position.go`, `internal/backtest/position_test.go`

**Interfaces:**
- Consumes: `domain.Candle`, `domain.Order` (carries `StopPrice`, `TPPrice`).
- Produces:
  - `type ExitKind int` with `NoExit, ExitStop, ExitTP`.
  - `func CheckExit(o domain.Order, c domain.Candle) (ExitKind, float64)` — for a long position with active stop/TP, returns the exit kind and fill price for candle `c`. **Pessimistic:** if the candle's [Low,High] straddles both stop and TP, return `ExitStop` (stop assumed first). Caller guarantees `c` is *after* the entry candle.

- [ ] **Step 1: Write the failing test**

`internal/backtest/position_test.go`:
```go
package backtest

import (
	"testing"

	"tradebot/internal/domain"
)

func longOrder() domain.Order {
	return domain.Order{Side: domain.Buy, Price: 100, StopPrice: 98, TPPrice: 104}
}

func TestStopOnly(t *testing.T) {
	k, px := CheckExit(longOrder(), domain.Candle{High: 101, Low: 97, Close: 99})
	if k != ExitStop || px != 98 {
		t.Fatalf("got %v px=%v want ExitStop@98", k, px)
	}
}

func TestTPOnly(t *testing.T) {
	k, px := CheckExit(longOrder(), domain.Candle{High: 105, Low: 99, Close: 104})
	if k != ExitTP || px != 104 {
		t.Fatalf("got %v px=%v want ExitTP@104", k, px)
	}
}

func TestStraddleAssumesStopFirst(t *testing.T) {
	// Candle hits BOTH stop (98) and TP (104). Pessimistic => stop.
	k, px := CheckExit(longOrder(), domain.Candle{High: 105, Low: 97, Close: 102})
	if k != ExitStop || px != 98 {
		t.Fatalf("straddle got %v px=%v want ExitStop@98", k, px)
	}
}

func TestNoExit(t *testing.T) {
	k, _ := CheckExit(longOrder(), domain.Candle{High: 103, Low: 99, Close: 101})
	if k != NoExit {
		t.Fatalf("got %v want NoExit", k)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/backtest/`
Expected: FAIL — `CheckExit` undefined.

- [ ] **Step 3: Implement**

`internal/backtest/position.go`:
```go
package backtest

import "tradebot/internal/domain"

type ExitKind int

const (
	NoExit ExitKind = iota
	ExitStop
	ExitTP
)

// CheckExit applies pessimistic intrabar exit-precedence for a long position.
// If the candle straddles both stop and TP, the stop is assumed to fill first.
func CheckExit(o domain.Order, c domain.Candle) (ExitKind, float64) {
	hitStop := o.StopPrice > 0 && c.Low <= o.StopPrice
	hitTP := o.TPPrice > 0 && c.High >= o.TPPrice
	switch {
	case hitStop:
		return ExitStop, o.StopPrice
	case hitTP:
		return ExitTP, o.TPPrice
	default:
		return NoExit, 0
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/backtest/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/backtest/position.go internal/backtest/position_test.go
git commit -m "feat: add pessimistic intrabar TP/SL exit-precedence"
```

---

## Task 15: Backtest engine + performance report

**Files:**
- Create: `internal/backtest/backtest.go`, `internal/backtest/backtest_test.go`

**Interfaces:**
- Consumes: `strategy.Strategy`, `risk.Gate`, `risk.Filters`, `CheckExit`, `domain.Candle`.
- Produces:
  - `type Trade struct { EntryTime, ExitTime int64; EntryPx, ExitPx, Qty, NetPnL float64; ExitReason string }`
  - `type Report struct { Trades []Trade; NumTrades, Wins int; WinRate, NetPnL, Expectancy, MaxDrawdownPct, FinalEquity float64 }`
  - `func Run(s strategy.Strategy, g *risk.Gate, f risk.Filters, candles []domain.Candle, startCash, feeRate float64) Report` — replays candles: on each closed candle, if flat, ask the strategy for a BUY (size via gate, fill at close + fee, record stop/TP from the order); if in a position, first apply `CheckExit` on the *current* candle (entry's TP/SL active from the next candle), else ask the strategy for a rule SELL. Computes equity curve + max drawdown.

- [ ] **Step 1: Write the failing test**

`internal/backtest/backtest_test.go`:
```go
package backtest

import (
	"testing"

	"tradebot/internal/config"
	"tradebot/internal/domain"
	"tradebot/internal/risk"
	"tradebot/internal/strategy"
)

func ramp() []domain.Candle {
	closes := []float64{}
	for i := 0; i < 40; i++ { closes = append(closes, 100) }
	for i := 0; i < 60; i++ { closes = append(closes, 100+float64(i)*4) }
	for i := 0; i < 40; i++ { closes = append(closes, 340-float64(i)*4) }
	cs := make([]domain.Candle, len(closes))
	for i, c := range closes {
		cs[i] = domain.Candle{Symbol: "BTCUSDT", High: c + 5, Low: c - 5, Close: c, Closed: true, CloseTime: int64(i)}
	}
	return cs
}

func TestRunProducesTradesAndEquity(t *testing.T) {
	g := risk.NewGate(config.RiskCfg{MaxPctPerTrade: 90, RiskPerTradePct: 5, MaxOpenPositions: 1, PortfolioMaxDeployedPct: 100, TPRewardMult: 1.6}, 0.003)
	s := strategy.NewEMACross("BTCUSDT", "1h", nil)
	rep := Run(s, g, risk.Filters{StepSize: 0.00001, MinQty: 0.00001, MinNotional: 5}, ramp(), 10000, 0.0015)
	if rep.NumTrades < 1 {
		t.Fatalf("expected >=1 completed trade, got %d", rep.NumTrades)
	}
	if rep.FinalEquity <= 0 {
		t.Fatalf("final equity must be positive, got %v", rep.FinalEquity)
	}
	if rep.WinRate < 0 || rep.WinRate > 1 {
		t.Fatalf("win rate out of range: %v", rep.WinRate)
	}
	if rep.MaxDrawdownPct < 0 {
		t.Fatalf("max drawdown must be >=0, got %v", rep.MaxDrawdownPct)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/backtest/ -run TestRun`
Expected: FAIL — `Run`/`Report` undefined.

- [ ] **Step 3: Implement**

`internal/backtest/backtest.go`:
```go
package backtest

import (
	"tradebot/internal/domain"
	"tradebot/internal/risk"
	"tradebot/internal/strategy"
)

type Trade struct {
	EntryTime, ExitTime int64
	EntryPx, ExitPx, Qty, NetPnL float64
	ExitReason string
}

type Report struct {
	Trades                                            []Trade
	NumTrades, Wins                                   int
	WinRate, NetPnL, Expectancy, MaxDrawdownPct, FinalEquity float64
}

func Run(s strategy.Strategy, g *risk.Gate, f risk.Filters, candles []domain.Candle, startCash, feeRate float64) Report {
	cash := startCash
	var open *domain.Order // active entry order (carries stop/TP); nil when flat
	var entryIdx int
	var rep Report
	peak := startCash

	equityAt := func(i int) float64 {
		if open == nil {
			return cash
		}
		return cash + open.Qty*candles[i].Close
	}
	closeTrade := func(exitPx float64, exitTime int64, reason string) {
		proceeds := open.Qty*exitPx - feeRate*open.Qty*exitPx
		entryCost := open.Qty*open.Price + feeRate*open.Qty*open.Price
		net := proceeds - entryCost
		cash += proceeds
		rep.Trades = append(rep.Trades, Trade{
			EntryTime: open.Time, ExitTime: exitTime, EntryPx: open.Price,
			ExitPx: exitPx, Qty: open.Qty, NetPnL: net, ExitReason: reason,
		})
		rep.NumTrades++
		rep.NetPnL += net
		if net > 0 {
			rep.Wins++
		}
		open = nil
	}

	for i := 1; i <= len(candles); i++ {
		c := candles[i-1]
		hist := candles[:i]

		// 1) Manage an open position: TP/SL active from the candle AFTER entry.
		if open != nil && i-1 > entryIdx {
			if kind, px := CheckExit(*open, c); kind == ExitStop {
				closeTrade(px, c.CloseTime, "SL")
			} else if kind == ExitTP {
				closeTrade(px, c.CloseTime, "TP")
			}
		}

		// 2) Strategy decision.
		inPos := open != nil
		if sig := s.Evaluate(hist, inPos); sig != nil {
			if sig.Action == domain.Buy && open == nil {
				acct := risk.Account{Equity: cash, FreeUSDT: cash, OpenPositions: 0}
				in := domain.Intent{Symbol: sig.Symbol, Action: domain.Buy, Price: c.Close,
					StopDist: sig.StopDist, TPDist: sig.TPDist, Reason: sig.Reason, Time: c.CloseTime}
				if o, err := g.Evaluate(in, acct, f); err == nil {
					cash -= o.Qty*o.Price + feeRate*o.Qty*o.Price
					oo := o
					open = &oo
					entryIdx = i - 1
				}
			} else if sig.Action == domain.Sell && open != nil {
				closeTrade(c.Close, c.CloseTime, "rule")
			}
		}

		// 3) Equity + drawdown.
		eq := equityAt(i - 1)
		if eq > peak {
			peak = eq
		}
		if peak > 0 {
			if dd := (peak - eq) / peak * 100; dd > rep.MaxDrawdownPct {
				rep.MaxDrawdownPct = dd
			}
		}
	}

	// Close any dangling position at the last close for reporting.
	if open != nil {
		closeTrade(candles[len(candles)-1].Close, candles[len(candles)-1].CloseTime, "eod")
	}
	rep.FinalEquity = cash
	if rep.NumTrades > 0 {
		rep.WinRate = float64(rep.Wins) / float64(rep.NumTrades)
		rep.Expectancy = rep.NetPnL / float64(rep.NumTrades)
	}
	return rep
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/backtest/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/backtest/backtest.go internal/backtest/backtest_test.go
git commit -m "feat: add backtest engine with equity curve and report"
```

---

## Task 16: Fee-sensitivity sweep + `bot backtest` CLI

**Files:**
- Modify: `internal/backtest/backtest.go`
- Create: `internal/backtest/sweep_test.go`
- Modify: `cmd/bot/main.go`

**Interfaces:**
- Consumes: `Run`.
- Produces:
  - `type SweepRow struct { FeeRoundTripPct, NetPnL, Expectancy, WinRate float64; Profitable bool }`
  - `func Sweep(s strategy.Strategy, g *risk.Gate, f risk.Filters, candles []domain.Candle, startCash float64, feesRoundTrip []float64) []SweepRow` — runs `Run` at each round-trip fee (passing `feeRate = feeRoundTrip/2/100` per side); `Profitable = NetPnL > 0`.
  - CLI: `bot backtest --config <p> --symbol BTCUSDT --interval 1h --bars 1000` prints the report + the 0.20/0.30/0.45% sweep table.

- [ ] **Step 1: Write the failing test**

`internal/backtest/sweep_test.go`:
```go
package backtest

import (
	"testing"

	"tradebot/internal/config"
	"tradebot/internal/risk"
	"tradebot/internal/strategy"
)

func TestSweepRunsAllFeeLevels(t *testing.T) {
	g := risk.NewGate(config.RiskCfg{MaxPctPerTrade: 90, RiskPerTradePct: 5, MaxOpenPositions: 1, PortfolioMaxDeployedPct: 100, TPRewardMult: 1.6}, 0.003)
	s := strategy.NewEMACross("BTCUSDT", "1h", nil)
	rows := Sweep(s, g, risk.Filters{StepSize: 0.00001, MinQty: 0.00001, MinNotional: 5}, ramp(), 10000, []float64{0.20, 0.30, 0.45})
	if len(rows) != 3 {
		t.Fatalf("want 3 sweep rows, got %d", len(rows))
	}
	for _, r := range rows {
		if r.FeeRoundTripPct <= 0 {
			t.Fatalf("fee not recorded: %+v", r)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/backtest/ -run TestSweep`
Expected: FAIL — `Sweep` undefined.

- [ ] **Step 3: Implement**

Append to `internal/backtest/backtest.go`:
```go
type SweepRow struct {
	FeeRoundTripPct, NetPnL, Expectancy, WinRate float64
	Profitable                                   bool
}

// Sweep runs the backtest at several round-trip fee levels (percent).
func Sweep(s strategy.Strategy, g *risk.Gate, f risk.Filters, candles []domain.Candle, startCash float64, feesRoundTrip []float64) []SweepRow {
	rows := make([]SweepRow, 0, len(feesRoundTrip))
	for _, rt := range feesRoundTrip {
		perSide := rt / 2 / 100
		rep := Run(s, g, f, candles, startCash, perSide)
		rows = append(rows, SweepRow{
			FeeRoundTripPct: rt, NetPnL: rep.NetPnL,
			Expectancy: rep.Expectancy, WinRate: rep.WinRate,
			Profitable: rep.NetPnL > 0,
		})
	}
	return rows
}
```

Add a `backtest` subcommand to `cmd/bot/main.go` (replace the file):
```go
package main

import (
	"flag"
	"fmt"
	"log"

	"tradebot/internal/backtest"
	"tradebot/internal/config"
	"tradebot/internal/marketdata"
	"tradebot/internal/risk"
	"tradebot/internal/strategy"
	"tradebot/internal/version"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "backtest" {
		runBacktest(os.Args[2:])
		return
	}
	cfgPath := flag.String("config", "config.yaml", "path to config.yaml")
	flag.Parse()
	if len(flag.Args()) > 0 && flag.Arg(0) == "version" {
		fmt.Printf("tradebot %s\n", version.Version)
		return
	}
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	fmt.Printf("tradebot %s — mode=%s symbols=%v (run `bot backtest` to validate a strategy)\n",
		version.Version, cfg.Mode, cfg.Symbols)
}

func runBacktest(args []string) {
	fs := flag.NewFlagSet("backtest", flag.ExitOnError)
	cfgPath := fs.String("config", "config.yaml", "config path")
	symbol := fs.String("symbol", "BTCUSDT", "symbol")
	interval := fs.String("interval", "1h", "candle interval")
	bars := fs.Int("bars", 1000, "number of candles")
	fs.Parse(args)

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	client := marketdata.NewClient(cfg.Exchange.Testnet)
	candles, err := client.Klines(*symbol, *interval, *bars)
	if err != nil {
		log.Fatalf("klines: %v", err)
	}
	g := risk.NewGate(cfg.Risk, cfg.Risk.FeeModel.Majors/100)
	s := strategy.NewEMACross(*symbol, *interval, nil)
	filt := risk.Filters{StepSize: 0.00001, MinQty: 0.00001, MinNotional: 5}

	rep := backtest.Run(s, g, filt, candles, 10000, cfg.Risk.FeeModel.Majors/2/100)
	fmt.Printf("=== Backtest %s %s (%d candles) ===\n", *symbol, *interval, len(candles))
	fmt.Printf("trades=%d winRate=%.1f%% netPnL=%.2f expectancy=%.4f maxDD=%.2f%% finalEquity=%.2f\n",
		rep.NumTrades, rep.WinRate*100, rep.NetPnL, rep.Expectancy, rep.MaxDrawdownPct, rep.FinalEquity)
	fmt.Println("--- fee sensitivity (round-trip) ---")
	for _, r := range backtest.Sweep(s, g, filt, candles, 10000, []float64{0.20, 0.30, 0.45}) {
		mark := "OK"
		if !r.Profitable {
			mark = "UNPROFITABLE"
		}
		fmt.Printf("fee %.2f%%: netPnL=%.2f winRate=%.1f%% -> %s\n", r.FeeRoundTripPct, r.NetPnL, r.WinRate*100, mark)
	}
}
```

Add `"os"` to the import block of `cmd/bot/main.go`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./... && go build ./cmd/bot`
Expected: PASS; binary builds.

- [ ] **Step 5: Commit**

```bash
git add internal/backtest cmd/bot
git commit -m "feat: add fee-sensitivity sweep and backtest CLI"
```

---

## Final verification

- [ ] Run the whole suite: `go test ./...` → all PASS.
- [ ] `go vet ./...` → clean.
- [ ] `go build -o bin/bot ./cmd/bot` → builds.
- [ ] Smoke: `./bin/bot version` prints the version; `./bin/bot backtest --symbol BTCUSDT --interval 1h --bars 500` prints a report + sweep (needs network for live klines; offline this is expected to error on the fetch only).

---

## Self-Review

**1. Spec coverage (Phases 0–1):**
- Config + testnet connection + fetch candles → Tasks 3, 12. ✓
- ema_cross_trend strategy (warm-up gating, ATR stop, vol-aware TP) → Tasks 5, 6. ✓
- Risk gate + sizing (§6 worked example, R:R gate, binding-constraint + effective-risk logging, min-notional reject, max-positions) → Task 7. ✓
- Simulated execution + fees → Task 8. ✓
- Portfolio + P&L → Task 9. ✓
- SQLite recording → Task 10. ✓
- End-to-end pipeline → Task 11. ✓
- Historical backfill → Task 13. ✓
- Pessimistic intrabar exit-precedence (§8) → Task 14. ✓
- Backtest report + equity/drawdown → Task 15. ✓
- Fee-sensitivity sweep as acceptance gate (§8) → Task 16. ✓
- Deferred to later phases (noted): multi-symbol open-count, WS streaming, regime layer, trailing stops, the other 3 strategies, Telegram, dashboard, learning. Consistent with spec Phases 2–7.

**2. Placeholder scan:** No "TBD/TODO"; every code/test step shows complete content. The single Phase-0 simplification (`engine.openCount` single-symbol) is explicitly flagged with its Phase-2 generalization, not left as a silent stub.

**3. Type consistency:** `Strategy.Evaluate(history, inPosition)`, `Gate.Evaluate(Intent, Account, Filters) (Order, error)`, `Executor.Execute(Order, Candle) (Fill, error)`, `Portfolio.Apply(Fill)`, `Store.RecordOrder/RecordFill`, `CheckExit(Order, Candle) (ExitKind, float64)`, and `Run(...) Report` are used identically everywhere they appear. `config.RiskCfg` field names match between Tasks 3, 7, 15, 16. `domain.Order` carries `StopPrice`/`TPPrice` (set in Task 7, consumed in Task 14). ✓
