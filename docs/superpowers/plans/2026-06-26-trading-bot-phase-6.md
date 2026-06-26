# Trading Bot Phase 6 (Learning Layer & Honest Projections) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Turn recorded trades into insight: per-strategy/per-regime scorecards, an honest Monte-Carlo equity projection cone (with risk-of-ruin), and walk-forward parameter optimization — surfaced on the dashboard and via a `bot learn` command.

**Architecture:** Pure analytics over the `trades` the engine already records (extended with the entry regime). `internal/learning` computes scorecards, Monte-Carlo projections (seeded RNG for determinism), and grid-search optimization that reuses `backtest.Run` with a train/test (out-of-sample) split. The dashboard gains `/api/scorecard` + `/api/projection` and a Projection/Scorecards section. A `bot learn` CLI prints the report. Everything is backward-looking and labeled "past performance, not a guarantee."

**Tech Stack:** Go 1.22 stdlib (`math`, `math/rand`, `sort`). No new deps. Reuses store/backtest/strategy/regime.

## Global Constraints

- **Honest projections only.** Monte-Carlo bootstraps THIS bot's recorded net-of-fee trade returns; never a fabricated forward number. Always carry the "past performance, not a guarantee" caveat in any projection output/UI.
- **No look-ahead in optimization.** Grid search picks params on the TRAIN slice; reported quality is the TEST (out-of-sample) slice. Never select on the test slice.
- **Pin canonical constants.** Optimize only the strategy's declared tunable knobs (e.g. `adx_min`, `tp_reward_mult`, `sl_atr_mult`); never the canonical periods.
- **Determinism.** Monte-Carlo takes a `*rand.Rand` so tests seed it; no global rand in library code.
- **Module path:** `tradebot`. TDD + one commit per task. No Claude attribution in commits.

---

## File Structure

```
internal/store/store.go            + Regime column on trades; ListTrades returns it
internal/engine/live.go            record entry regime with each trade
internal/learning/score.go         per-strategy / per-(strategy,regime) scorecards
internal/learning/montecarlo.go    bootstrap projection + risk-of-ruin
internal/learning/optimize.go      walk-forward grid-search parameter optimization
internal/dashboard/api.go          + /api/scorecard, /api/projection
internal/dashboard/server.go       + Scorecards & Projection section in the page
cmd/bot/main.go                    + `bot learn` subcommand
```

---

## Task 1: Record entry regime on trades

**Files:** Modify `internal/store/store.go`, `internal/store/store_test.go`, `internal/engine/live.go`

**Interfaces:**
- `store.Trade` gains `Regime string`; the `trades` table gains a `regime` column; `RecordTrade` writes it; `ListTrades` scans it.
- `engine.Live` tracks `openRegime map[string]string` (set at entry from `regime.Classify(hist).String()`), and `recordClose` passes it into the recorded trade.

- [ ] **Step 1: Failing test** — append to `internal/store/store_test.go`:
```go
func TestTradeRegimeRoundTrips(t *testing.T) {
	s, _ := Open(":memory:")
	defer s.Close()
	s.RecordTrade(Trade{TS: 1, Symbol: "BTCUSDT", Strategy: "ema_cross_trend", Regime: "TrendingUp", Reason: "TP", NetPnL: 4})
	ts, _ := s.ListTrades(5)
	if len(ts) != 1 || ts[0].Regime != "TrendingUp" {
		t.Fatalf("regime not persisted: %+v", ts)
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/store/ -run TradeRegime` → FAIL.

- [ ] **Step 3: Implement.** In `store.go`: add `Regime string` to `Trade`; change the `trades` table DDL to include `regime TEXT` (after `strategy`); update the `RecordTrade` INSERT column list + values; update `ListTrades` SELECT + Scan to include `regime`. In `live.go`: add `openRegime map[string]string` (init in `NewLive`); where an entry executes set `l.openRegime[sym] = reg.String()` (the `reg` already computed for `decision.Pick`); in `recordClose`, set `Regime: l.openRegime[sym]`; `delete(l.openRegime, sym)` at both close sites alongside `openStrat`.

> Exact `RecordTrade` body:
```go
func (s *Store) RecordTrade(t Trade) error {
	_, err := s.db.Exec(`INSERT INTO trades(ts,symbol,strategy,regime,reason,entry_px,exit_px,qty,net_pnl)
		VALUES(?,?,?,?,?,?,?,?,?)`, t.TS, t.Symbol, t.Strategy, t.Regime, t.Reason, t.EntryPx, t.ExitPx, t.Qty, t.NetPnL)
	return err
}
```
and `ListTrades` SELECT becomes `SELECT ts,symbol,strategy,regime,reason,entry_px,exit_px,qty,net_pnl FROM trades ORDER BY id DESC LIMIT ?` with a matching `Scan(&t.TS,&t.Symbol,&t.Strategy,&t.Regime,&t.Reason,&t.EntryPx,&t.ExitPx,&t.Qty,&t.NetPnL)`.

- [ ] **Step 4: Run** `go test ./...` → PASS.
- [ ] **Step 5: Commit** `git add internal/store internal/engine && git commit -m "feat: record entry market regime with each trade"`

---

## Task 2: Per-strategy / per-regime scorecards

**Files:** Create `internal/learning/score.go`, `internal/learning/score_test.go`

**Interfaces:**
- Produces:
  - `type Score struct { Strategy, Regime string; Trades, Wins int; WinRate, NetPnL, Expectancy, MaxDrawdown float64 }`
  - `func ScoreByRegime(trades []store.Trade) []Score` — one Score per (strategy, regime) group; `MaxDrawdown` = max peak-to-trough of the group's cumulative net (oldest→newest; `store.ListTrades` is newest-first, so reverse internally). Sorted by Strategy then Regime.
  - `func ScoreByStrategy(trades []store.Trade) []Score` — one Score per strategy (Regime = "ALL").

- [ ] **Step 1: Failing test** — `internal/learning/score_test.go`:
```go
package learning

import (
	"testing"

	"tradebot/internal/store"
)

func TestScoreByStrategyAggregates(t *testing.T) {
	trades := []store.Trade{ // newest-first, as ListTrades returns
		{Strategy: "ema_cross_trend", Regime: "TrendingUp", NetPnL: -2},
		{Strategy: "ema_cross_trend", Regime: "TrendingUp", NetPnL: 5},
		{Strategy: "ema_cross_trend", Regime: "Ranging", NetPnL: 1},
	}
	all := ScoreByStrategy(trades)
	if len(all) != 1 { t.Fatalf("want 1 strategy score, got %d", len(all)) }
	s := all[0]
	if s.Trades != 3 || s.Wins != 2 { t.Fatalf("counts: %+v", s) }
	if s.NetPnL < 3.99 || s.NetPnL > 4.01 { t.Fatalf("netPnL=%v want 4", s.NetPnL) }
}

func TestScoreByRegimeSplits(t *testing.T) {
	trades := []store.Trade{
		{Strategy: "ema_cross_trend", Regime: "TrendingUp", NetPnL: 5},
		{Strategy: "ema_cross_trend", Regime: "Ranging", NetPnL: -3},
	}
	sc := ScoreByRegime(trades)
	if len(sc) != 2 { t.Fatalf("want 2 regime groups, got %d", len(sc)) }
}
```

- [ ] **Step 2: Run** `go test ./internal/learning/ -run Score` → FAIL.

- [ ] **Step 3: Implement** — `internal/learning/score.go`:
```go
package learning

import (
	"sort"

	"tradebot/internal/store"
)

type Score struct {
	Strategy, Regime                    string
	Trades, Wins                        int
	WinRate, NetPnL, Expectancy, MaxDrawdown float64
}

// reversed returns trades oldest->newest (ListTrades gives newest-first).
func reversed(ts []store.Trade) []store.Trade {
	out := make([]store.Trade, len(ts))
	for i := range ts {
		out[len(ts)-1-i] = ts[i]
	}
	return out
}

func score(key func(store.Trade) string, label func(store.Trade) (string, string), trades []store.Trade) []Score {
	groups := map[string][]store.Trade{}
	var order []string
	for _, t := range trades {
		k := key(t)
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], t)
	}
	var out []Score
	for _, k := range order {
		g := reversed(groups[k])
		var s Score
		s.Strategy, s.Regime = label(groups[k][0])
		var cum, peak, maxDD float64
		for _, t := range g {
			s.Trades++
			s.NetPnL += t.NetPnL
			if t.NetPnL > 0 {
				s.Wins++
			}
			cum += t.NetPnL
			if cum > peak {
				peak = cum
			}
			if peak-cum > maxDD {
				maxDD = peak - cum
			}
		}
		s.MaxDrawdown = maxDD
		if s.Trades > 0 {
			s.WinRate = float64(s.Wins) / float64(s.Trades)
			s.Expectancy = s.NetPnL / float64(s.Trades)
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Strategy != out[j].Strategy {
			return out[i].Strategy < out[j].Strategy
		}
		return out[i].Regime < out[j].Regime
	})
	return out
}

func ScoreByStrategy(trades []store.Trade) []Score {
	return score(func(t store.Trade) string { return t.Strategy },
		func(t store.Trade) (string, string) { return t.Strategy, "ALL" }, trades)
}

func ScoreByRegime(trades []store.Trade) []Score {
	return score(func(t store.Trade) string { return t.Strategy + "|" + t.Regime },
		func(t store.Trade) (string, string) { return t.Strategy, t.Regime }, trades)
}
```

- [ ] **Step 4: Run** `go test ./internal/learning/` → PASS.
- [ ] **Step 5: Commit** `git add internal/learning && git commit -m "feat: add per-strategy and per-regime scorecards"`

---

## Task 3: Monte-Carlo projection

**Files:** Create `internal/learning/montecarlo.go`, `internal/learning/montecarlo_test.go`

**Interfaces:**
- Produces:
  - `type Projection struct { Steps int; P5, P50, P95 []float64; TermP5, TermP50, TermP95, RiskOfRuinPct float64 }`
  - `func TradeReturns(trades []store.Trade) []float64` — per-trade net return fraction `NetPnL / (EntryPx*Qty)`, skipping trades with zero notional.
  - `func MonteCarlo(returns []float64, startEquity float64, steps, sims int, ruinDrawdownPct float64, rng *rand.Rand) Projection` — each sim draws `steps` returns with replacement, compounds equity from `startEquity`; records the per-step 5/50/95 percentiles across sims, terminal percentiles, and `RiskOfRuinPct` = share of sims whose equity ever falls `ruinDrawdownPct`% below `startEquity`. Empty returns → zero-value Projection.

- [ ] **Step 1: Failing test** — `internal/learning/montecarlo_test.go`:
```go
package learning

import (
	"math/rand"
	"testing"

	"tradebot/internal/store"
)

func TestTradeReturns(t *testing.T) {
	r := TradeReturns([]store.Trade{{EntryPx: 100, Qty: 2, NetPnL: 4}, {EntryPx: 0, Qty: 1, NetPnL: 1}})
	if len(r) != 1 { t.Fatalf("zero-notional trade must be skipped, got %d", len(r)) }
	if r[0] < 0.0199 || r[0] > 0.0201 { t.Fatalf("return=%v want 0.02", r[0]) }
}

func TestMonteCarloProducesOrderedCone(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	// mildly positive edge: +2% and -1% returns
	p := MonteCarlo([]float64{0.02, 0.02, -0.01}, 1000, 20, 500, 20, rng)
	if p.Steps != 20 || len(p.P50) != 20 { t.Fatalf("bad shape: %+v", p.Steps) }
	if !(p.TermP5 <= p.TermP50 && p.TermP50 <= p.TermP95) {
		t.Fatalf("percentiles must be ordered: %v %v %v", p.TermP5, p.TermP50, p.TermP95)
	}
	if p.RiskOfRuinPct < 0 || p.RiskOfRuinPct > 100 {
		t.Fatalf("risk of ruin out of range: %v", p.RiskOfRuinPct)
	}
}

func TestMonteCarloEmpty(t *testing.T) {
	p := MonteCarlo(nil, 1000, 10, 10, 20, rand.New(rand.NewSource(1)))
	if p.Steps != 0 || p.P50 != nil { t.Fatalf("empty returns -> zero projection, got %+v", p) }
}
```

- [ ] **Step 2: Run** `go test ./internal/learning/ -run 'TradeReturns|MonteCarlo'` → FAIL.

- [ ] **Step 3: Implement** — `internal/learning/montecarlo.go`:
```go
package learning

import (
	"math/rand"
	"sort"

	"tradebot/internal/store"
)

type Projection struct {
	Steps                                int
	P5, P50, P95                         []float64
	TermP5, TermP50, TermP95, RiskOfRuinPct float64
}

func TradeReturns(trades []store.Trade) []float64 {
	var out []float64
	for _, t := range trades {
		notional := t.EntryPx * t.Qty
		if notional == 0 {
			continue
		}
		out = append(out, t.NetPnL/notional)
	}
	return out
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(p / 100 * float64(len(sorted)-1))
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func MonteCarlo(returns []float64, startEquity float64, steps, sims int, ruinDrawdownPct float64, rng *rand.Rand) Projection {
	if len(returns) == 0 || steps <= 0 || sims <= 0 {
		return Projection{}
	}
	// equityAtStep[step] = slice of `sims` equities at that step
	equityAtStep := make([][]float64, steps)
	for i := range equityAtStep {
		equityAtStep[i] = make([]float64, sims)
	}
	ruinFloor := startEquity * (1 - ruinDrawdownPct/100)
	ruined := 0
	for s := 0; s < sims; s++ {
		eq := startEquity
		hitRuin := false
		for step := 0; step < steps; step++ {
			eq *= 1 + returns[rng.Intn(len(returns))]
			if eq <= ruinFloor {
				hitRuin = true
			}
			equityAtStep[step][s] = eq
		}
		if hitRuin {
			ruined++
		}
	}
	p := Projection{Steps: steps, P5: make([]float64, steps), P50: make([]float64, steps), P95: make([]float64, steps)}
	for step := 0; step < steps; step++ {
		col := equityAtStep[step]
		sort.Float64s(col)
		p.P5[step] = percentile(col, 5)
		p.P50[step] = percentile(col, 50)
		p.P95[step] = percentile(col, 95)
	}
	p.TermP5, p.TermP50, p.TermP95 = p.P5[steps-1], p.P50[steps-1], p.P95[steps-1]
	p.RiskOfRuinPct = 100 * float64(ruined) / float64(sims)
	return p
}
```

- [ ] **Step 4: Run** `go test ./internal/learning/` → PASS.
- [ ] **Step 5: Commit** `git add internal/learning && git commit -m "feat: add Monte-Carlo projection with risk-of-ruin"`

---

## Task 4: Walk-forward parameter optimization

**Files:** Create `internal/learning/optimize.go`, `internal/learning/optimize_test.go`

**Interfaces:**
- Produces:
  - `type OptResult struct { BestParams map[string]float64; TrainNetPnL, TestNetPnL float64; TestTrades int }`
  - `func Optimize(candles []domain.Candle, mk func(map[string]float64) strategy.Strategy, grid map[string][]float64, g *risk.Gate, f risk.Filters, startCash, feeRate, trainFrac float64) OptResult` — split candles at `trainFrac`; enumerate the grid (cartesian product); for each combo build the strategy via `mk`, run `backtest.Run` on the TRAIN slice, keep the combo with the best train NetPnL; then run `backtest.Run` on the TEST slice with the winning combo and report its (out-of-sample) NetPnL + trade count.

- [ ] **Step 1: Failing test** — `internal/learning/optimize_test.go`:
```go
package learning

import (
	"testing"

	"tradebot/internal/config"
	"tradebot/internal/domain"
	"tradebot/internal/risk"
	"tradebot/internal/strategy"
)

func synth(n int) []domain.Candle {
	cs := make([]domain.Candle, 0, n)
	price := 100.0
	for i := 0; i < n; i++ {
		// alternating trend segments so different adx_min values differ
		if (i/50)%2 == 0 { price += 1.5 } else { price -= 0.3 }
		cs = append(cs, domain.Candle{Symbol: "BTCUSDT", Close: price, High: price + 3, Low: price - 3, Closed: true, CloseTime: int64(i) * 3600_000})
	}
	return cs
}

func TestOptimizePicksAndReportsOOS(t *testing.T) {
	g := risk.NewGate(config.RiskCfg{MaxPctPerTrade: 90, RiskPerTradePct: 5, MaxOpenPositions: 1, PortfolioMaxDeployedPct: 100, TPRewardMult: 1.6}, 0.003)
	mk := func(p map[string]float64) strategy.Strategy { return strategy.NewEMACross("BTCUSDT", "1h", p) }
	grid := map[string][]float64{"adx_min": {20, 25, 30}, "tp_reward_mult": {1.6, 2.0}}
	res := Optimize(synth(400), mk, grid, g, risk.Filters{StepSize: 0.00001, MinQty: 0.00001, MinNotional: 5}, 10000, 0.0015, 0.7)
	if res.BestParams == nil { t.Fatal("expected best params") }
	if _, ok := res.BestParams["adx_min"]; !ok { t.Fatalf("best params missing adx_min: %+v", res.BestParams) }
}
```

- [ ] **Step 2: Run** `go test ./internal/learning/ -run Optimize` → FAIL.

- [ ] **Step 3: Implement** — `internal/learning/optimize.go`:
```go
package learning

import (
	"sort"

	"tradebot/internal/backtest"
	"tradebot/internal/domain"
	"tradebot/internal/risk"
	"tradebot/internal/strategy"
)

type OptResult struct {
	BestParams              map[string]float64
	TrainNetPnL, TestNetPnL float64
	TestTrades              int
}

// combos expands a grid into the cartesian product of param maps (sorted keys for determinism).
func combos(grid map[string][]float64) []map[string]float64 {
	keys := make([]string, 0, len(grid))
	for k := range grid {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := []map[string]float64{{}}
	for _, k := range keys {
		var next []map[string]float64
		for _, base := range out {
			for _, v := range grid[k] {
				m := map[string]float64{}
				for bk, bv := range base {
					m[bk] = bv
				}
				m[k] = v
				next = append(next, m)
			}
		}
		out = next
	}
	return out
}

func Optimize(candles []domain.Candle, mk func(map[string]float64) strategy.Strategy, grid map[string][]float64, g *risk.Gate, f risk.Filters, startCash, feeRate, trainFrac float64) OptResult {
	if len(candles) < 10 {
		return OptResult{}
	}
	split := int(float64(len(candles)) * trainFrac)
	train, test := candles[:split], candles[split:]
	var res OptResult
	best := -1e18
	for _, combo := range combos(grid) {
		rep := backtest.Run(mk(combo), g, f, train, startCash, feeRate)
		if rep.NetPnL > best {
			best = rep.NetPnL
			res.BestParams = combo
			res.TrainNetPnL = rep.NetPnL
		}
	}
	if res.BestParams != nil {
		testRep := backtest.Run(mk(res.BestParams), g, f, test, startCash, feeRate)
		res.TestNetPnL = testRep.NetPnL
		res.TestTrades = testRep.NumTrades
	}
	return res
}
```

- [ ] **Step 4: Run** `go test ./internal/learning/` → PASS.
- [ ] **Step 5: Commit** `git add internal/learning && git commit -m "feat: add walk-forward grid-search parameter optimization"`

---

## Task 5: Dashboard scorecard + projection endpoints & section

**Files:** Modify `internal/dashboard/api.go`, `internal/dashboard/server.go`, `internal/dashboard/api_test.go`

**Interfaces:**
- `api.go`: add authed routes `GET /api/scorecard` (→ `learning.ScoreByRegime(store.ListTrades(1000))`) and `GET /api/projection` (→ build `learning.TradeReturns` from `store.ListTrades(1000)`, run `MonteCarlo(returns, 10000, 30, 1000, 20, rand.New(rand.NewSource(1)))`, return the `Projection`). Seeded RNG keeps it deterministic per request.
- `server.go`: add a "Strategy scorecards (net, by regime)" table and a "Projection (Monte-Carlo — past performance, not a guarantee)" block to the page, fetched from the two endpoints; render the projection's terminal P5/P50/P95 + risk-of-ruin as text plus a simple cone (reuse the SVG approach: three polylines for P5/P50/P95).

- [ ] **Step 1: Failing test** — append to `internal/dashboard/api_test.go`:
```go
func TestScorecardAndProjection(t *testing.T) {
	s, st, _ := newServer(t)
	st.RecordTrade(store.Trade{TS: 3, Symbol: "BTCUSDT", Strategy: "ema_cross_trend", Regime: "TrendingUp", EntryPx: 100, Qty: 1, NetPnL: 4})
	st.RecordTrade(store.Trade{TS: 4, Symbol: "BTCUSDT", Strategy: "ema_cross_trend", Regime: "TrendingUp", EntryPx: 100, Qty: 1, NetPnL: -2})
	for _, path := range []string{"/api/scorecard?token=secret", "/api/projection?token=secret"} {
		rr := httptest.NewRecorder()
		s.Handler().ServeHTTP(rr, httptest.NewRequest("GET", path, nil))
		if rr.Code != 200 { t.Fatalf("%s status %d", path, rr.Code) }
	}
}
```
(add `"tradebot/internal/store"` to the test imports if not present)

- [ ] **Step 2: Run** `go test ./internal/dashboard/ -run ScorecardAndProjection` → FAIL.

- [ ] **Step 3: Implement.** In `api.go` register the two routes inside `Handler()` (wrapped in `authMiddleware`):
```go
mux.HandleFunc("/api/scorecard", s.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
	ts, _ := s.store.ListTrades(1000)
	writeJSON(w, learning.ScoreByRegime(ts))
}))
mux.HandleFunc("/api/projection", s.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
	ts, _ := s.store.ListTrades(1000)
	proj := learning.MonteCarlo(learning.TradeReturns(ts), 10000, 30, 1000, 20, rand.New(rand.NewSource(1)))
	writeJSON(w, proj)
}))
```
Add imports `math/rand` and `tradebot/internal/learning` to `api.go`. In `server.go`, add to the HTML (below the trades table) a scorecards table (`#scoreRows`) and a projection block (`#proj`), and extend the inline JS `refresh()` to `fetch('/api/scorecard')` + `fetch('/api/projection')` and render them (scorecards as table rows; projection as text "median Xx, 5th Yx, 95th Zx, risk-of-ruin N%" plus three SVG polylines for the cone). Keep using `textContent` for any data values (no innerHTML of server data).

- [ ] **Step 4: Run** `go test ./internal/dashboard/` → PASS. `go vet ./...` clean.
- [ ] **Step 5: Commit** `git add internal/dashboard && git commit -m "feat: add scorecard and Monte-Carlo projection to dashboard"`

---

## Task 6: `bot learn` CLI

**Files:** Modify `cmd/bot/main.go`

**Interfaces:**
- `bot learn` reads `tradebot.db`, prints `learning.ScoreByRegime(...)` as a table and the `learning.MonteCarlo` projection summary (terminal P5/P50/P95 + risk-of-ruin), each labeled "past performance, not a guarantee".

- [ ] **Step 1: Implement** — add a `learn` branch to `main()` (mirroring the `backtest` branch dispatch) and:
```go
func runLearn() {
	st, err := store.Open("tradebot.db")
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	defer st.Close()
	ts, _ := st.ListTrades(2000)
	fmt.Printf("=== Scorecards (net of fees) — %d trades ===\n", len(ts))
	fmt.Printf("%-18s %-12s %6s %7s %10s %10s\n", "strategy", "regime", "trades", "win%", "net", "expectancy")
	for _, s := range learning.ScoreByRegime(ts) {
		fmt.Printf("%-18s %-12s %6d %6.1f%% %10.2f %10.4f\n", s.Strategy, s.Regime, s.Trades, s.WinRate*100, s.NetPnL, s.Expectancy)
	}
	proj := learning.MonteCarlo(learning.TradeReturns(ts), 10000, 30, 2000, 20, rand.New(rand.NewSource(1)))
	fmt.Println("\n=== Monte-Carlo projection (30 trades ahead; PAST PERFORMANCE, NOT A GUARANTEE) ===")
	if proj.Steps == 0 {
		fmt.Println("not enough trades to project yet.")
		return
	}
	fmt.Printf("terminal equity from 10000: 5th=%.0f  median=%.0f  95th=%.0f  risk-of-ruin(-20%%)=%.1f%%\n",
		proj.TermP5, proj.TermP50, proj.TermP95, proj.RiskOfRuinPct)
}
```
Add `learn` to `main()`'s arg dispatch: `if len(os.Args) > 1 && os.Args[1] == "learn" { runLearn(); return }`. Add imports `math/rand` and `tradebot/internal/learning` to main.go.

- [ ] **Step 2: Run** `go build ./cmd/bot && go test ./... && go vet ./...` → all pass.
- [ ] **Step 3: Commit** `git add cmd/bot && git commit -m "feat: add bot learn command (scorecards + projection)"`

---

## Self-Review

**Spec coverage (Phase 6):** entry-regime on trades → T1 ✓; per-strategy/per-regime scorecards → T2 ✓ (fills the dashboard Strategy-Scorecard-by-regime gap deferred from Phase 5); honest Monte-Carlo cone + risk-of-ruin (bootstraps recorded net trades, "past performance" caveat) → T3 ✓; walk-forward OOS grid-search param optimization (pins canonical constants, selects on train, reports test) → T4 ✓; dashboard scorecard + projection endpoints/section → T5 ✓; `bot learn` CLI → T6 ✓. Auto-applying learned weights back into the live decision aggregator is intentionally left as an operator-reviewed step (the scorecards inform it) — noted; the regime gating itself already runs live (Phase 2).

**Placeholder scan:** none — complete code/tests.

**Type consistency:** `store.Trade.Regime`, `learning.Score`/`ScoreByStrategy`/`ScoreByRegime`, `learning.Projection`/`TradeReturns`/`MonteCarlo`, `learning.OptResult`/`Optimize`, and the two new dashboard routes are used consistently. Reuses `backtest.Run`, `risk.Gate`/`Filters`, `strategy.Strategy`, `store.Store` unchanged.
