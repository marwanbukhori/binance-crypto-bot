# Binance Spot Trading Bot — Design Spec

**Date:** 2026-06-26
**Status:** Approved (architecture + starter strategies + risk defaults + dashboard IA); first
implementation plan scoped to Phases 0–1
**Language:** Go · **Target host:** Oracle Cloud Always Free ARM VM

> Strategy logic, risk defaults, and the dashboard IA in §6–§9 and §15 were hardened by an
> adversarial verification pass. Where a default was changed to fix a real flaw, the reason is
> noted inline. **Nothing here is backtested** — every threshold is a reasoned default and a
> hypothesis that must earn its keep on recorded data (see §17 caveats).

---

## 1. Overview

A comprehensive, self-hosted automated trading bot for **Binance.com Spot**, written in Go as a
single static binary. It ingests live market data, makes long-only buy/sell decisions from
rule-based strategies, passes every decision through a non-bypassable risk gate, executes on
Binance, records everything, learns from recorded outcomes, is notified and controlled via a
Telegram bot, and exposes a web dashboard.

**Profit is the end goal**, treated primarily as a *risk-management and validation* problem: the
bot wins by not losing, by validating every strategy on historical and paper data before real
money, and by enforcing hard guardrails on every order.

| Requirement | Where it lives |
|---|---|
| Trade | `execution`, `portfolio` |
| Notify | `notify` (Telegram) |
| Decisions from algorithm + chart | `strategy`, `indicators`, `decision`, `regime` |
| Learn from decisions | `learning`, `store` |
| Dashboards | `dashboard` |
| Record data | `store` |
| Profit (don't blow up) | `risk` (hard gate), `backtest`, paper-trading mode |

---

## 2. Decisions log (locked)

1. **Market:** Binance.com **Spot only** — long-only, no shorting/margin/leverage, cannot be
   liquidated. A position is either "in the coin" or "in cash (USDT)". Futures deferred.
2. **Timeframe:** **Configurable** — each strategy declares its timeframe; the engine subscribes
   to the matching candle streams.
3. **Decision engine:** **Hybrid** — rule-based strategies place trades; a learning layer tunes
   parameters, scores strategies, and learns which strategy to trust in which market regime. No
   black box ever gets direct order control.
4. **Control mode:** **Tiered + hard guardrails** (paper → approve-first → autonomous as a config
   flag; guardrails apply in every mode).
5. **Notify/control:** **Telegram bot** via long-polling (no public URL); inline approve/reject
   buttons + commands.
6. **Storage:** **SQLite** (pure-Go `modernc.org/sqlite`), migration-ready for Postgres.
7. **Dashboard:** web UI served by the Go binary itself.
8. **Host:** **Oracle Cloud Always Free** ARM VM (24/7 at $0; the bot must never sleep).
9. **Profit-target behavior:** **per-trade TP/SL only** — no daily profit-lock; the bot keeps
   trading all day. Daily/weekly *loss* limits + kill-switch still apply (intentional asymmetry).

---

## 3. Guiding principles

1. **You win by not losing.** The risk gate is the most important code in the system and is
   non-bypassable. Risk and indicators get TDD and the highest coverage.
2. **One pipeline, three modes.** Backtest, paper (testnet), and live run the *exact same*
   strategy → decision → risk → execution code. Only the data source and the executor swap.
3. **Record everything.** Every signal, decision, rejection, fill, and P&L snapshot is persisted;
   the learning layer and dashboard are readers of that record.
4. **No look-ahead, anywhere.** All indicators — for signals *and* for sizing/stops (ATR included)
   — use **closed candles only**, never the forming candle. The backtester applies a pessimistic
   intrabar fill rule (§8).
5. **Never trust in-memory state.** Reconcile balances/orders against the exchange on startup and
   periodically; recover cleanly from a crash.
6. **Single static binary** — bot + dashboard + Telegram in one process, ideal for one free ARM box.

---

## 4. Architecture

### 4.1 Pipeline (live mode)

```
Binance WS (klines + user-data) ┐
Binance REST (history backfill) ├─► MarketData ──candles──► Strategy Engine ──signals──┐
                                │                                 ▲                     ▼
                                │                          Regime + weights      Decision Aggregator
                                │                                 │              (nets correlated longs)
                                │                            Learning Layer             │
User-data WS (fills) ───────────┴──► Portfolio ◄──┐         (periodic, offline)   ⛔ RISK GATE ⛔
                                          │        │                                    │ approved
                                          │        └──────── reads ──────┐              ▼
                                          ▼                              │         Execution ──► Binance
                                        STORE (SQLite) ◄─────────────────┴───┐          │
                                          ▲                                  │          ▼
                                   Dashboard (web)              Notifier (Telegram) ◄── fills/rejects
```

### 4.2 Components (Go packages, one binary)

| Package | Responsibility |
|---|---|
| `marketdata` | Binance WS streams (klines, ticker, user-data) + REST backfill; normalize to internal `Candle`/`Fill`; rolling candle buffers per (symbol, timeframe); auto-reconnect + gap-fill. |
| `indicators` | Pure functions: EMA, SMA, RSI, MACD, Bollinger, ATR, ADX. No state/I-O; fully unit-tested. |
| `strategy` | `Strategy` declares needed symbols/timeframes, consumes **closed** candles, emits a `Signal`. Config-driven params; many run concurrently. Per-(strategy,symbol,timeframe) warm-up gating: **no signal until the longest indicator window is full**. The four starter strategies live here (§7). |
| `regime` | Classifies the current market regime per symbol (Trending-Up / Trending-Down / Ranging / High-Vol / **Low-Vol-Chop**) from ADX + ATR/Bollinger-bandwidth percentile, closed candles only. Drives which strategies the aggregator activates. |
| `decision` | Aggregates signals into one `Intent` per symbol, applying learning-layer weights + regime. **Nets correlated same-symbol longs** so the three trend/momentum strategies cannot stack into a 3× bet. |
| `risk` | **The hard gate.** Position sizing (§6) + every guardrail (§5). Emits an approved `Order` or a logged + notified rejection. Logs the *binding constraint* and the *effective risk %* on every order. Non-bypassable. |
| `execution` | Places orders (REST), idempotent via `clientOrderId`, partial-fill + retry/backoff, reconciles against user-data stream. Pluggable: **real** (live/testnet) vs **simulated** executor (backtest; models fees + slippage + pessimistic intrabar fills). |
| `portfolio` | Source of truth for balances, positions, avg entry, realized/unrealized P&L. Reconciles with the exchange on startup + periodically. |
| `store` | SQLite via pure-Go `modernc.org/sqlite`. Records candles, signals, intents, risk decisions, orders, fills, positions, P&L snapshots, strategy scores, config history, learning runs, audit events. Postgres-ready schema. |
| `backtest` | Replays historical candles through the **same** pipeline → simulated fills → performance report. Enforces the §8 exit-precedence + warm-up rules so backtest ≈ live. |
| `learning` | Periodic, off the hot path. Scores each strategy (win rate, expectancy, drawdown, Sharpe-ish) **by regime**; sweeps only the 2–3 genuine free knobs per strategy via walk-forward / out-of-sample backtest (canonical constants **pinned**); outputs regime weights + recommended params, applied automatically or after Telegram approval. |
| `notify` | Telegram bot. Out: trade alerts, approve/reject inline buttons, daily/weekly digest, kill-switch + error alerts. In (long-poll): `/status` `/positions` `/pause` `/resume` `/kill` + approval callbacks. |
| `dashboard` | Embedded HTTP server; 6 pages (§9). Live updates via SSE. Token/basic auth (public VM). |
| `engine` | Orchestrator: wires components via channels, runs the event loop, owns mode + lifecycle + graceful shutdown + config hot-reload + the kill-switch flag (persisted). |
| `config` | YAML + env: mode, strategies & params, risk limits, symbols, secrets (env only). |

### 4.3 Modes

- **Backtest:** historical candles → strategies → risk → **simulated** executor → store → report.
- **Paper:** live **testnet** data → strategies → risk → simulated/testnet executor. Risks nothing.
- **Live:** real account, guardrails active, optional approve-first via Telegram.

---

## 5. Risk guardrails (the most important section)

Every `Intent` must pass *all* of these before becoming an `Order`. Any failure → reject, log,
notify. Config-driven with the conservative defaults below.

| Control | Default | Notes |
|---|---|---|
| `max_pct_per_trade` | **5%** | Max balance in one position. **This is the real binding constraint** for any stop tighter than ~20% of price (i.e. always) — it, not `risk_per_trade_pct`, sets effective per-trade risk. |
| `risk_per_trade_pct` | **1% (CEILING)** | Upper bound on fixed-fractional risk. The 5% notional cap usually binds first → **effective risk ~0.1–0.3% of equity**. Every order logs the binding cap + realized effective-risk %; never let an operator believe 1% is live. |
| `max_open_positions` | **2** | Caps correlated exposure (reduced from 3: crypto longs are one bet in a market-wide drop). |
| `portfolio_max_deployed_pct` | **10%** | Portfolio-wide ceiling on total deployed notional. At 10%, a −30% market gap costs ~3% equity. Enforced in the sizer. |
| `daily_loss_limit_pct` | **3% (kill-switch trigger)** | Realized+unrealized daily loss halts **new** entries; open positions still managed. A *trigger*, not a guaranteed ceiling. |
| `hard_flatten_drawdown_pct` | **6% (separate, harder ceiling)** | At −6% intraday/rolling, optionally **flatten or tighten stops** on open positions — the real backstop above the soft trigger. |
| `weekly_loss_limit_pct` | **8%** | Halts the week, forces human review. Manual reset. |
| `post_loss_cooldown` | **candle-aligned, N=2 closed candles** | Replaces wall-clock 60 min (which could expire mid-candle and allow revenge re-entry). Backtest ≈ live. |
| `take_profit` | **volatility-aware: `tp_dist = 1.6 × stop_dist`, gated to post-fee net R:R ≥ 1.3** | Replaces a fixed % TP (which, against a floating 2–4% ATR stop, was ~0.56–0.82 : 1 after fees — a slow-bleed machine). Any TP/SL pair failing the R:R gate is rejected/warned. |
| `stop_loss` | **2.0 × ATR(14)** (2.5× for breakout), 1% floor | All four strategies use ATR stops; self-scales to volatility. Stops are **best-effort**, not exchange-guaranteed. |
| `trailing_stop` | **0 (off)** | Per-strategy defaults provided but inactive until proven, so backtest ≈ live. |
| `fee_model` | **0.30% round-trip majors / 0.45%+ alts** | Sweep 0.20/0.30/0.45 in backtest; any strategy that flips unprofitable across it must not graduate past paper. Enable BNB discount (~0.15%/side) where possible. |
| `min_notional` | validate **live** LOT_SIZE + MIN_NOTIONAL, **reject (never up-size)** | Refresh `exchangeInfo` at runtime. On a tiny account, trades are correctly skipped rather than over-sized. |
| same-symbol long netting | on | The three trend/momentum strategies cannot stack into a concentrated correlated position. |
| **Kill-switch** | — | A single persisted flag (SQLite) settable by daily-loss breach, `/kill`, or repeated errors. Instantly halts new orders; still allows managing/exiting open positions. Survives crash-restart. |

---

## 6. Position sizing

**Method:** fixed-fractional risk per trade with an ATR-derived stop, hard-capped by a max-notional
ceiling *and* a portfolio-deployed ceiling, floored/validated against Binance LOT_SIZE +
MIN_NOTIONAL. Long-only: quantity is always ≥ 0; "sell" only reduces an existing position to cash.
**Sizing only ever rounds DOWN**, so realized risk ≤ the risk budget always.

**Formula** (all inputs from closed candles; equity from a fresh reconcile, not stale memory):

1. **Risk budget (ceiling):** `risk_usdt = equity × risk_per_trade_pct/100`.
2. **Stop distance:** `stop_dist = max(atr_mult × ATR(14), price × stop_loss_pct/100)`; `stop_price = price − stop_dist`.
3. **Volatility-aware TP:** `tp_dist = tp_reward_mult × stop_dist` (default 1.6). **Gate:** require post-fee net R:R = `(tp_dist − fee×price)/(stop_dist + fee×price) ≥ 1.3` (fee = 0.0030 majors / 0.0045 alts); else reject/warn.
4. **Qty from risk:** `qty_risk = risk_usdt / stop_dist`.
5. **Caps:** `qty_cap = equity × max_pct_per_trade/100 / price`; clamp so total deployed ≤ `equity × portfolio_max_deployed_pct/100`; `qty_free = free_usdt × 0.995 / price`.
6. **Choose:** `qty = min(qty_risk, qty_cap, qty_portfolio, qty_free)`. **Log the binding constraint and `effective_risk = qty × stop_dist` as % of equity.**
7. **Exchange filters:** floor to `stepSize`; require `qty ≥ minQty` and `qty × price ≥ minNotional × 1.01`; else **reject**.

**Worked example:** equity 1000 USDT, `risk_per_trade_pct`=1 → risk_usdt=10. BTC=60000, ATR=900,
`atr_mult`=2 → `stop_dist`=1800 = **3.0% wide**. `qty_risk`=0.005556 BTC (333 USDT notional).
`qty_cap` at 5% = 0.000833 BTC (50 USDT). min → **qty = 0.000833 BTC; binding = notional cap;
effective risk = 1.5 USDT = 0.15% of equity** (not 1% — the notional cap dominates; this is normal,
log it). TP at 1.6× → 62880 (~4.8%); post-fee R:R = (4.8−0.3)/(3.0+0.3) = **1.36 ≥ 1.3** ✓.

---

## 7. Starter strategy library

Four distinct, simple, long-only strategies covering different regimes. Canonical constants
(EMA 9/21, ADX/ATR/RSI 14, MACD 12/26/9, BB 20/2.0, SMA200, EMA100) are **pinned / non-optimizable**;
only the noted knobs are tunable (walk-forward OOS only). All signals on candle **close**; all
exits obey the §8 pessimistic intrabar precedence.

### 7.1 `ema_cross_trend` — trend-following · default **1h**
- **Indicators:** EMA(9/21), ADX(14), ATR(14).
- **Entry:** EMA9 crosses above EMA21 **AND** ADX(14) ≥ 25 *(raised from 20 to cut boundary whipsaw)*.
- **Exit (rule):** EMA9 crosses back below EMA21 (lets the trend ride).
- **Stop / TP:** 2.0×ATR / 1.6×stop. Trailing 2.5% (off).
- **Tunable:** `adx_min`, `tp_reward_mult`, `sl_atr_mult`.
- **Regime:** trending; **disabled in Low-Vol-Chop.** **Fails:** chop whipsaw, lags turns, gap-downs.

### 7.2 `rsi_bb_reversion` — mean-reversion · default **1h** *(raised from 15m — at 15m the capture was net-negative after fees)*
- **Indicators:** RSI(14), Bollinger(20, 2.0), SMA(200), ATR(14), ADX(14).
- **Entry:** RSI ≤ 30 **AND** close ≤ lower BB **AND** close > SMA200 *(hard gate)* **AND** ADX < 25 **AND** min-edge: close-to-SMA20 distance ≥ 1.2% *(~4× round-trip cost; else skip)*.
- **Exit (rule):** close ≥ middle BB (SMA20) **OR** RSI ≥ 60.
- **Stop / TP:** 2.0×ATR *(was fixed 1.5%)* / 1.6×stop (mean-touch usually exits first).
- **Regime:** the designated **Low-Vol-Chop / ranging owner**; gated off in downtrends.
- **Fails:** knife-catching in a crash (SMA200 gate blocks most), idle in tight ranges; long-only
  harvests only half a reversion cycle — discount two-sided expectancy.

### 7.3 `donchian_breakout` — breakout · default **4h**
- **Indicators:** ATR(14), SMA, Bollinger.
- **Entry:** close > highest close of prior 20 candles **AND** ATR(14) > avg ATR(14) of last 20 *(real volatility expansion)*.
- **Exit (rule):** close < lowest close of prior 10 candles — a **10-bar trailing-low (Turtle) exit** *(corrected from a mislabeled "middle of channel")*.
- **Stop / TP:** 2.5×ATR / 4.0% wide cap (rule-exit is primary; lets winners run). Trailing 3.0% (off).
- **Tunable:** `exit_lookback`. **Regime:** vol-expansion / trend-start; off in quiet.
- **Fails:** false breakouts/bull traps; buys high; win rate <45% by design — profitability depends
  on letting winners run. Cost-robust (6–10% moves dwarf fees).

### 7.4 `macd_momentum` — momentum · default **1h** (2h fallback)
- **Indicators:** MACD(12/26/9), EMA(100), RSI(14), ADX(14), ATR(14).
- **Entry:** MACD line crosses above signal **AND** histogram rising **AND** close > EMA100 **AND** ADX(14) ≥ 20 **AND** histogram magnitude ≥ threshold **AND** RSI ≤ 75 *(anti-churn gates — this is the highest-churn cross system)*.
- **Exit (rule):** MACD line crosses back below signal; **1-candle debounce** (no recross round-trip within a bar).
- **Stop / TP:** 2.0×ATR / 1.6×stop. **Tunable:** `adx_min`, `hist_min_mult`, `tp/sl mults`.
- **Regime:** momentum/early-trend; off in flat/low-vol. **Fails:** sideways whipsaw, lags, churn.

> **Regime model.** `regime` classifies Trending-Up / Trending-Down / Ranging / High-Vol /
> **Low-Vol-Chop** (ADX + ATR/BB-bandwidth percentile). In Low-Vol-Chop the aggregator **disables
> trend/breakout/momentum** and leaves only `rsi_bb_reversion` active — closing the basket's single
> biggest robustness gap (3 of 4 strategies bleed fees in quiet drift). This classifier is
> **load-bearing**; it must be validated and use closed candles only.

---

## 8. Testing strategy

- **TDD** on `indicators` (known input→output vectors) and `risk` (these protect real money).
- **Exit-precedence (look-ahead) test:** the simulated executor activates TP/SL/trailing only from
  the candle **after** entry, and when one candle's [low,high] straddles both TP and SL (or a TP and
  a close-based rule-exit), it assumes the **worse fill (stop first)**. Without this every backtest
  is optimistically biased.
- **Warm-up gating test:** asserts **zero signals** before each strategy's longest indicator window
  is full, per (strategy, symbol, timeframe).
- **Effective-risk test:** asserts realized risk ≤ risk budget and that the binding-constraint +
  effective-risk % is logged on every order.
- **Fee-sensitivity sweep as acceptance gate:** backtest at 0.20/0.30/0.45% round-trip; a strategy
  that flips unprofitable (prime suspects: `rsi_bb_reversion`, `macd_momentum`) must not graduate
  past paper. Judge on the **realized-exit distribution** (rule-exits usually fire before the TP),
  not the headline TP.
- Strategy tests on fixture candle series; the backtester doubles as a regression harness.
- **Paper-trading on testnet is the acceptance gate** before any live money.

---

## 9. Dashboard information architecture (6 pages)

1. **Overview** *(the only page a tired operator needs at 2am)* — Bot-status banner (state,
   WS heartbeat age, clock skew, API-key permission check); Kill-switch & safety controls (daily 3%
   trigger, weekly 8%, the distinct 6% hard-flatten line); Equity & P&L summary; Equity-curve
   sparkline with event markers; Current position (entry, TP/SL/trailing levels, distance-to-exit,
   binding sizing constraint + effective-risk %); Open orders.
2. **Decisions** *(the honesty log)* — Decision feed (ENTER / EXIT-RULE / EXIT-TP / EXIT-SL /
   EXIT-TRAIL / HOLD / SKIP-BLOCKED with one-line reason, incl. blocked entries); Decision-detail
   drawer (exact closed-candle indicator values, rule fired, computed TP/SL, R:R-gate result, sizing
   binding-constraint, intended vs filled, slippage); Decision-rate / churn guard (near-break-even
   exit % per strategy).
3. **Trades & Performance** — Closed-trades ledger (NET P&L after fees, fee column always visible,
   exit reason, regime at entry, CSV export); Performance stats (win rate, profit factor, expectancy
   **net of fees**, + fee-sensitivity sweep table); Full equity curve; Drawdown (underwater curve,
   max DD, recovery time, distance to 6% hard-flatten).
4. **Strategy Scorecards** — Roster (status, default-timeframe badge, pinned-vs-tunable param
   styling); **Regime scorecard matrix** (metrics × regime incl. Low-Vol-Chop, low-sample cells
   flagged); Per-strategy equity & drawdown; Reasoning notes.
5. **Projection** *(honest forward view)* — Monte-Carlo cone (bootstraps this bot's realized
   net-of-fee trades; 5/50/95 percentile fan; persistent "past performance, not a guarantee");
   Risk-of-ruin & outcome spread; Assumptions & caveats block (always visible).
6. **Settings & Logs** — Read-only config mirror; Change & event audit (kill-switch, hard-flatten,
   param changes, with before/after); System-log tail.

---

## 10. Operating the bot — configuration & lifecycle

All non-secret settings live in one `config.yaml`; secrets in `.env` (gitignored).

```yaml
mode: paper                 # backtest | paper | live
exchange: { testnet: true }
symbols: [BTCUSDT, ETHUSDT]

strategies:
  - { name: ema_cross_trend,   enabled: true,  timeframe: 1h, params: { adx_min: 25, sl_atr_mult: 2.0, tp_reward_mult: 1.6 } }
  - { name: rsi_bb_reversion,  enabled: true,  timeframe: 1h, params: { oversold: 30, exit_rsi: 60, min_edge_pct: 1.2, sl_atr_mult: 2.0 } }
  - { name: donchian_breakout, enabled: false, timeframe: 4h, params: { entry_lookback: 20, exit_lookback: 10, sl_atr_mult: 2.5 } }
  - { name: macd_momentum,     enabled: false, timeframe: 1h, params: { adx_min: 20, rsi_max: 75, sl_atr_mult: 2.0, tp_reward_mult: 1.6 } }

risk:
  max_pct_per_trade: 5
  risk_per_trade_pct: 1            # CEILING; effective risk ~0.1-0.3% (notional cap binds)
  max_open_positions: 2
  portfolio_max_deployed_pct: 10
  daily_loss_limit_pct: 3          # kill-switch trigger
  hard_flatten_drawdown_pct: 6     # flatten/tighten open positions
  weekly_loss_limit_pct: 8
  post_loss_cooldown_candles: 2
  tp_reward_mult: 1.6              # volatility-aware TP; gated to post-fee R:R >= 1.3
  trailing_stop_pct: 0             # off by default
  fee_model: { majors: 0.30, alts: 0.45, bnb_discount: true }

control:  { autonomous: false }    # false = approve-first via Telegram
notify:   { telegram: { enabled: true, daily_digest_at: "21:00" } }
dashboard:{ enabled: true, port: 8080 }
```

```env
# .env (gitignored)
BINANCE_API_KEY=...   BINANCE_API_SECRET=...   # trade+read only, NO withdraw
TELEGRAM_BOT_TOKEN=...   TELEGRAM_CHAT_ID=...   DASHBOARD_TOKEN=...
```

**Lifecycle (one binary, increasing trust):**
1. **Backtest** — `bot backtest --strategy ema_cross_trend --from 2024-01-01 --to 2025-06-01` →
   report; tune (OOS) until justified.
2. **Paper** — `mode: paper` on testnet, live prices, fake money. Observe for weeks.
3. **Live, approve-first** — real keys, `autonomous: false`; bot sends *"BUY 0.01 BTC @ 64,300 —
   TP 65,586 / SL 63,657 — Approve / Reject"*.
4. **Autonomous** — `autonomous: true` once recorded numbers earn trust. Guardrails stay on.

**Profit-target behavior (decision A):** per-trade TP/SL only; no daily profit-lock (the bot keeps
trading). Daily/weekly *loss* limits + kill-switch remain active — downside bounded per day, upside
open.

**Projections (honest only):** expectancy, a Monte-Carlo cone (5/50/95 percentile equity fan), and
risk-of-ruin / time-to-goal — all from recorded/backtested trades, each labeled *"past performance,
not a guarantee."* **No fabricated forward-profit number, ever.**

**Reporting:** Telegram daily/weekly digest; the 6-page dashboard; per-strategy scorecard split by
regime; CSV trade log + monthly PDF.

---

## 11. Error handling & resilience

WS auto-reconnect with backoff + REST gap-fill on reconnect · idempotent orders (`clientOrderId`) +
startup/periodic reconciliation (recover from exchange + DB after a crash) · Binance rate-limit/weight
awareness (backoff on 429/418) · server-time offset (`recvWindow`) · panic recovery per goroutine ·
structured logging; critical errors notify · graceful shutdown flushes state · kill-switch + cooldown
state persisted in SQLite so a restart cannot bypass a guardrail.

---

## 12. Data model (initial sketch)

SQLite tables: `candles` (cache), `signals`, `intents`, `risk_decisions`, `orders`, `fills`,
`positions`, `pnl_snapshots`, `strategy_scores`, `config_history`, `learning_runs`, `events` (audit).
Monetary/quantity fields stored as strings or scaled integers; computation uses `shopspring/decimal`.
No SQLite-specific features in the hot path (Postgres-ready).

---

## 13. Build phases (each independently shippable & safe)

- **Phase 0 — Walking skeleton:** config + Binance **testnet** connection + fetch candles + one
  `ema_cross_trend` strategy + risk-gate stub + simulated execution + SQLite recording. Proves the
  pipeline end-to-end.
- **Phase 1 — Backtester:** historical backfill + replay (with §8 exit-precedence + warm-up) +
  performance report + fee-sensitivity sweep. *Strategies validated before risking anything.*
- **Phase 2 — Full strategy library (§7) + indicators + regime classifier + complete risk
  guardrails (§5–§6).**
- **Phase 3 — Paper trading on testnet** (live data, full guardrails).
- **Phase 4 — Telegram bot** (alerts, approve/reject, commands, digests).
- **Phase 5 — Dashboard** (the 6 pages, §9).
- **Phase 6 — Learning layer** (regime scoring, pinned-constant param sweep, weighting).
- **Phase 7 — Live on Oracle Always Free:** approve-mode at tiny size → autonomous once earned.

**First implementation plan covers Phases 0–1.**

---

## 14. Configuration & security

YAML for non-secrets; **environment variables for all secrets**. Binance API key created with
**trade + read only, NO withdrawal**, IP-restricted to the VM where possible. Dashboard behind a
token / basic auth (or localhost + SSH tunnel). Secrets never committed.

---

## 15. Deployment (Oracle Cloud Always Free)

Cross-compile the static Go binary for ARM64 (pure-Go SQLite → no CGO). Run under `systemd`
(auto-restart, start on boot). SQLite file + config on the VM; periodic DB backup. Telegram via
long-polling → no inbound ports for the bot; only the dashboard port (or SSH tunnel) is exposed,
behind auth.

---

## 16. Open items to verify at implementation time

- **Go Binance library:** `adshao/go-binance` (leading candidate) vs Binance's official Go connector
  — confirm maintenance + Spot Testnet support.
- **Binance Spot Testnet** base URLs, key provisioning, feature parity.
- **`modernc.org/sqlite`** current version + ARM64 caveats.
- **Oracle Always Free** ARM capacity/region + Binance API geo-restriction for the chosen region.
- Decimal handling (`shopspring/decimal` vs scaled ints) — finalize in Phase 0.

---

## 17. Honest limitations & caveats

- **Not financial advice; no profit guarantee.** Any automated strategy can and will lose money.
  Size so a total loss would not hurt you.
- **Nothing here is backtested.** The verifiers confirmed the rules are look-ahead-clean and the
  math is consistent, but the threshold values (adx_min 25, exit_rsi 60, min_edge 1.2%,
  tp_reward_mult 1.6, …) are reasoned defaults, not fitted/validated numbers. **Paper-first is
  mandatory.**
- **Effective risk is ~0.1–0.3%, not 1%** (the notional cap binds). Safe, but don't widen stops or
  raise `max_pct_per_trade` believing 1% is live — read the per-trade binding-constraint log.
- **Spot stops are best-effort, not guaranteed.** A gap/flash-crash can fill well below the stop;
  realized loss can exceed the 3% daily limit on a single candle. The 6% hard-flatten + kill-switch
  are the real catastrophe brakes — stress-test against historical gap-down candles.
- **Correlation is the real tail risk.** The trend/momentum strategies go long the same symbols
  together; `max_open_positions=2` + `portfolio_max_deployed=10%` + same-symbol netting bound it,
  but a correlated crash hits all in-coin positions at once.
- **Long-only halves the reversion edge** — discount two-sided expectancy in acceptance.
- **Regime-layer dependency.** The Low-Vol-Chop protection is load-bearing; if the classifier is
  wrong/absent, 3 of 4 strategies whipsaw in quiet markets.
- **Fees can still kill the two marginal strategies** (`rsi_bb_reversion`, `macd_momentum`) across
  the fee sweep — if so, they stay on paper. BNB discount helps. High-churn scalping is out.
- **Validate exchange filters at runtime** (stepSize/minQty/minNotional change per symbol).

---

## 18. Explicit non-goals (YAGNI)

No futures/margin/leverage (Spot only). No black-box ML with direct order control. No multi-exchange
abstraction yet (keep interfaces clean enough that it's *possible* later). No SPA build pipeline
(server-rendered + SSE). No HFT / sub-second latency.
