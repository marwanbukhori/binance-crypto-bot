# Binance Spot Trading Bot — Design Spec

**Date:** 2026-06-26
**Status:** Approved (architecture); first implementation plan scoped to Phases 0–1
**Language:** Go
**Target host:** Oracle Cloud — Always Free ARM VM

---

## 1. Overview

A comprehensive, self-hosted automated trading bot for **Binance.com Spot**, written in Go as a
single static binary. It ingests live market data, makes buy/sell decisions from rule-based
strategies, passes every decision through a non-bypassable risk gate, executes on Binance,
records everything, learns from recorded outcomes, notifies and is controlled via a Telegram
bot, and exposes a web dashboard.

**Profit is the end goal.** The design treats that goal as primarily a *risk-management and
validation* problem: the bot wins by not losing, by validating every strategy on historical and
paper data before real money, and by enforcing hard guardrails on every order.

### What the user asked for, mapped to components
| Requirement | Where it lives |
|---|---|
| Trade | `execution`, `portfolio` |
| Notify | `notify` (Telegram) |
| Decisions from algorithm + chart | `strategy`, `indicators`, `decision` |
| Learn from decisions | `learning`, `store` |
| Dashboards | `dashboard` |
| Record data | `store` |
| Profit (don't blow up) | `risk` (hard gate), `backtest`, paper-trading mode |

---

## 2. Decisions log (locked during brainstorming)

1. **Market:** Binance.com **Spot only** (no leverage, no shorting, cannot be liquidated). Futures
   is explicitly deferred to a future, separate module; nothing here precludes it.
2. **Timeframe:** **Configurable** — a strategy declares the timeframe(s) it needs; the engine
   subscribes to the matching candle streams. Not hardcoded to scalping/swing.
3. **Decision engine:** **Hybrid** — a rule-based strategy engine actually places trades; a
   learning layer on top tunes parameters, scores strategies, and learns which strategy to trust
   in which market regime. No black-box model is ever given direct control of orders.
4. **Control mode:** **Tiered + hard guardrails.** Mode is a config switch
   (paper → approve-first → autonomous); the guardrail layer applies in *every* mode regardless.
5. **Notify/control channel:** **Telegram bot** via long-polling (no public URL, no webhook, no
   TLS cert needed; works behind NAT). Inline buttons for approve/reject; commands for control.
6. **Storage:** **SQLite** to start (pure-Go `modernc.org/sqlite` driver for trivial ARM
   cross-compile), schema designed to migrate to Postgres later.
7. **Dashboard:** lightweight web UI **served by the Go binary itself** (no separate frontend
   deploy).
8. **Host:** **Oracle Cloud Always Free** ARM VM (genuine 24/7 at $0; a trading bot must never
   sleep, which disqualifies sleep-on-idle free tiers).

---

## 3. Guiding principles

1. **You win by not losing.** The risk gate is the most important code in the system and is
   non-bypassable. Risk rules get TDD and the highest test coverage.
2. **One pipeline, three modes.** Backtest, paper (testnet), and live run the *exact same*
   strategy → decision → risk → execution code. The only thing swapped is the data source and the
   executor implementation. This eliminates "worked in backtest, broke live" drift.
3. **Record everything.** Every signal, decision, rejection, fill, and P&L snapshot is persisted.
   The learning layer and the dashboard are *readers* of that record, never a separate source of
   truth.
4. **Single static binary.** Bot + dashboard + Telegram in one process — ideal for one free ARM box.
5. **Never trust in-memory state alone.** Reconcile balances and open orders against the exchange
   on startup and periodically; the bot must recover cleanly from a crash.

---

## 4. Architecture

### 4.1 Pipeline (live mode)

```
Binance WS (klines + user-data) ┐
Binance REST (history backfill) ├─► MarketData ──candles──► Strategy Engine ──signals──┐
                                │                                 ▲                     ▼
                                │                          (weights / regime)    Decision Aggregator
                                │                                 │                     │
                                │                            Learning Layer             ▼
User-data WS (fills) ───────────┴──► Portfolio ◄──┐         (periodic, offline)   ⛔ RISK GATE ⛔
                                          │        │                                    │ approved
                                          │        └──────── reads ──────┐              ▼
                                          ▼                              │         Execution ──► Binance
                                        STORE (SQLite) ◄─────────────────┴───┐          │
                                          ▲                                  │          ▼
                                   Dashboard (web)              Notifier (Telegram) ◄── fills/rejects
```

### 4.2 Components

Each package has one clear purpose, a well-defined interface, and is independently testable.

| Package | Responsibility | Key interface(s) |
|---|---|---|
| `marketdata` | Binance WS streams (klines, ticker, user-data) + REST backfill; normalize to internal `Candle`/`Fill`; rolling candle buffers per (symbol, timeframe); auto-reconnect + gap-fill on reconnect. | `Source.Subscribe(symbol, tf) <-chan Candle`; `Source.Backfill(symbol, tf, from, to) []Candle` |
| `indicators` | Pure functions over candle series: EMA, SMA, RSI, MACD, Bollinger, ATR, ADX, etc. No state, no I/O — fully unit-tested. | `EMA(series, period) []float64`, etc. |
| `strategy` | `Strategy` declares needed symbols/timeframes, consumes candles, emits a `Signal`. Config-driven params so the learning layer can tune. Many run concurrently and independently. | `Strategy.Requires() []Subscription`; `Strategy.OnCandle(c Candle) *Signal` |
| `decision` | Aggregate signals from all active strategies into one `Intent` per symbol, applying learning-layer weights + current market regime. | `Aggregator.Decide([]Signal) []Intent` |
| `risk` | **The hard gate.** Position sizing (fixed-fractional / ATR-based) + all guardrails. Emits an approved `Order` or a logged + notified rejection. Cannot be bypassed by any code path. | `Gate.Evaluate(Intent, PortfolioState) (Order, error)` |
| `execution` | Place orders (REST), idempotent via `clientOrderId`, partial-fill + retry/backoff, reconcile against user-data stream. Pluggable executor: **real** (live/testnet) vs **simulated** (backtest; models fees + slippage). | `Executor.Place(Order) (OrderResult, error)` |
| `portfolio` | Source of truth for balances, positions, avg entry, realized/unrealized P&L. Reconciles with the exchange on startup + periodically. | `Portfolio.State() PortfolioState` |
| `store` | SQLite via pure-Go `modernc.org/sqlite`. Records candles, signals, intents, risk decisions, orders, fills, positions, P&L snapshots, strategy scores, config history, learning runs. Migration-ready for Postgres. | repository interfaces per entity |
| `backtest` | Replay historical candles through the **same** pipeline → simulated fills → performance report (equity curve, win rate, expectancy, drawdown). | `Backtester.Run(cfg) Report` |
| `learning` | Periodic, off the hot path. Score each strategy (win rate, expectancy, drawdown, Sharpe-ish) **by regime**; sweep parameters via backtest (grid/random search); classify regime (trend vs range via ADX/volatility); output updated weights + recommended params, applied automatically or after Telegram approval. | `Learner.Run() LearningResult` |
| `notify` | Telegram bot. **Out:** trade alerts, approve/reject inline buttons, daily/weekly P&L digest, kill-switch + error alerts. **In (long-poll):** `/status` `/positions` `/pause` `/resume` `/kill` + approval callbacks. | `Notifier.Send(Event)`; `Notifier.Commands() <-chan Command` |
| `dashboard` | Embedded HTTP server: equity curve, live positions, open orders, recent decisions, per-strategy performance, logs, kill-switch button. Live updates via SSE. Token/basic auth (public VM). | standard `http.Handler` |
| `engine` | Orchestrator: wire components via internal channels, run the event loop, own mode + lifecycle + graceful shutdown + config hot-reload + the kill-switch flag. | `Engine.Run(ctx)` |
| `config` | YAML + env: mode, enabled strategies & params, risk limits, symbols, secrets (API keys, Telegram token — env only, never committed). | `Load() Config` |

### 4.3 Modes (same pipeline, swapped data source + executor)

- **Backtest:** historical candles → strategies → risk → **simulated** executor (fees + slippage
  modeled) → store → report.
- **Paper:** live **testnet** data → strategies → risk → simulated or testnet executor. A live
  rehearsal that risks nothing.
- **Live:** real account, guardrails active, optional approve-first via Telegram.

---

## 5. Risk guardrails (the most important section)

Every `Intent` must pass *all* of these before becoming an `Order`. Any failure → reject, log,
notify. These are config-driven with safe defaults.

- **Max position size** per symbol (absolute and as % of total balance).
- **Max % of balance per trade.**
- **Max number of concurrent open positions.**
- **Daily loss limit** and **weekly loss limit** → breach trips the **kill-switch** (halts all new
  orders until manually reset).
- **Post-loss cooldown** — no new entry on a symbol for N minutes after a losing exit.
- **Exchange filter compliance** — lot size, min-notional, price/quantity precision (orders that
  violate Binance filters are rejected before they're sent).
- **Available-balance check** — never order more than is free.
- **Duplicate/again protection** — don't re-fire the same intent within a debounce window.
- **Kill-switch** — a single flag, settable by daily-loss breach, manual `/kill`, or repeated
  execution errors, that immediately halts all new orders. Closing/managing existing positions is
  still allowed.

Position sizing default: **fixed-fractional risk per trade** (risk X% of balance, with stop
distance derived from ATR), falling back to a fixed quote-amount per trade when no stop is defined.

---

## 6. Error handling & resilience

- WebSocket **auto-reconnect** with exponential backoff; on reconnect, **gap-fill** missing candles
  via REST.
- **Idempotent orders** via `clientOrderId`; **startup + periodic reconciliation** of balances and
  open orders against the exchange (recover state from exchange + DB after a crash).
- **Rate-limit / weight awareness** — respect Binance request weights; back off on HTTP 429/418.
- **Server-time offset handling** — sync to Binance server time; set `recvWindow` appropriately.
- **Panic recovery** per goroutine; structured logging; critical errors notify via Telegram.
- **Graceful shutdown** — flush state to SQLite, stop accepting new intents, optionally flatten or
  leave positions per config.

---

## 7. Data model (initial sketch)

Tables (SQLite): `candles` (optional cache), `signals`, `intents`, `risk_decisions`, `orders`,
`fills`, `positions`, `pnl_snapshots`, `strategy_scores`, `config_history`, `learning_runs`,
`events` (audit log). All monetary/quantity fields stored as strings or scaled integers to avoid
float precision loss; computation uses `shopspring/decimal` (or equivalent). Schema is designed so
a later swap to Postgres is mechanical (no SQLite-specific features in the hot path).

---

## 8. Testing strategy

- **TDD** on `indicators` (known input→output vectors) and `risk` (these protect real money — they
  must be bulletproof).
- **Strategy tests** against fixture candle series (deterministic).
- The **backtester doubles as a regression harness** — strategy behavior is pinned by replaying
  fixed historical windows and asserting on the report.
- **Paper-trading on testnet is the acceptance gate** before any live money is enabled.

---

## 9. Build phases (each independently shippable and safe)

- **Phase 0 — Walking skeleton:** config + Binance **testnet** connection + fetch candles + one
  EMA-crossover strategy + risk-gate stub + simulated execution + SQLite recording. Proves the
  pipeline end-to-end.
- **Phase 1 — Backtester:** historical backfill + replay engine + performance report. *Strategies
  can now be validated before risking anything.*
- **Phase 2 — Full strategy engine + indicators library + complete risk guardrails.**
- **Phase 3 — Paper trading on testnet** (live data, full guardrails, simulated/testnet fills).
- **Phase 4 — Telegram bot** (alerts, approve/reject, commands, daily/weekly digests).
- **Phase 5 — Dashboard** (web UI + kill-switch).
- **Phase 6 — Learning layer** (scoring, parameter optimization, regime-based weighting).
- **Phase 7 — Live on Oracle Always Free:** go live in approve-mode with tiny size → graduate to
  autonomous once the recorded numbers earn it.

**First implementation plan covers Phases 0–1** (skeleton + backtester): a real, runnable
foundation that can validate strategies before any real money is involved.

---

## 10. Operating the bot — configuration, targets, projections, reporting

### 10.1 Configuration

All non-secret settings live in one `config.yaml`; secrets live in `.env` (gitignored). Example:

```yaml
mode: paper                 # backtest | paper | live
exchange: { testnet: true }

symbols: [BTCUSDT, ETHUSDT]

strategies:
  - name: ema_cross
    enabled: true
    timeframe: 1h
    params: { fast: 9, slow: 21, rsi_period: 14, rsi_max: 70 }
  - name: rsi_reversion
    enabled: false
    timeframe: 15m
    params: { rsi_period: 14, oversold: 30, overbought: 70 }

risk:
  max_open_positions: 3
  max_pct_per_trade: 5          # cap: % of balance in one position
  risk_per_trade_pct: 1         # fixed-fractional risk (ATR-based stop)
  daily_loss_limit_pct: 3       # breach -> kill-switch for the day
  weekly_loss_limit_pct: 8
  post_loss_cooldown_min: 60

profit:
  take_profit_pct: 2            # per-trade target (locks the win)
  stop_loss_pct: 1              # per-trade stop (cuts the loss)
  trailing_stop_pct: 0          # 0 = off (optional; ride winners when > 0)
  # NOTE: no daily profit-lock by design (decision A). The bot keeps trading
  # all day; only the daily/weekly LOSS limits above can halt it.
  account_goal_pct: 50          # long-horizon goal -> reporting/projection ONLY

control:
  autonomous: false             # false = approve-first via Telegram

notify:  { telegram: { enabled: true, daily_digest_at: "21:00" } }
dashboard: { enabled: true, port: 8080 }
```

```env
# .env  (gitignored)
BINANCE_API_KEY=...      BINANCE_API_SECRET=...
TELEGRAM_BOT_TOKEN=...   TELEGRAM_CHAT_ID=...
DASHBOARD_TOKEN=...
```

### 10.2 Operating lifecycle (one binary, increasing trust)

1. **Backtest** — `bot backtest --strategy ema_cross --from 2024-01-01 --to 2025-06-01` → performance
   report. Tune until the numbers justify proceeding.
2. **Paper** — `mode: paper` runs on **testnet** with live prices and fake money. Observe for weeks.
3. **Live, approve-first** — real keys, `autonomous: false`. The bot sends *"BUY 0.01 BTC @ 64,300 —
   TP 65,586 / SL 63,657 — Approve / Reject"* and trades only on approval.
4. **Autonomous** — flip `autonomous: true` once recorded numbers earn trust. Guardrails stay on.

### 10.3 Profit-target behavior (decision: per-trade only — option A)

The "profit target" is a set of **exit/risk controls, never a prediction**:

- **Per-trade take-profit / stop-loss** *(active)* — each position auto-exits at +`take_profit_pct`
  or −`stop_loss_pct`.
- **Trailing stop** *(optional, default off)* — when set, the exit trails price up to ride winners.
- **Daily profit-lock** *(NOT used — decision A)* — the bot does **not** stop after a good day; it
  keeps trading. Accepted trade-off: it may give back some unrealized gains chasing more.
- **Account goal** *(reporting/projection only)* — never drives trades.

**Intentional asymmetry:** there is no daily *profit* cap, but the daily/weekly *loss* limits +
kill-switch (§5) remain fully active. Downside is bounded per day; upside is left open.

### 10.4 Projections (the honest version)

No tool can project future profit; the bot never shows a fabricated "you'll make $X" number. It
shows only outputs grounded in recorded/backtested data, each labeled *"past performance, not a
guarantee"*:

- **Expectancy** — avg return per trade × trade frequency, from backtest/live history.
- **Monte Carlo cone** — resample the actual trade-outcome distribution thousands of times to show a
  5th / 50th / 95th-percentile equity fan and the worst drawdown observed across runs (shows the
  *spread of luck*, not a point estimate).
- **Risk-of-ruin & time-to-goal** — probability of an X% drawdown and median time to reach
  `account_goal_pct`, *conditional on the historical edge holding*.

### 10.5 Reporting

| Surface | Contents |
|---|---|
| **Telegram digest** (daily/weekly) | Realized + unrealized P&L, win rate, best/worst trade, open positions, balance, fees, progress toward account goal. |
| **Dashboard** | Live equity curve, drawdown chart, open positions, recent decisions *with reasons*, per-strategy scorecard, regime breakdown, Monte Carlo projection cone. |
| **Per-strategy scorecard** | Win rate, expectancy, Sharpe-ish, max drawdown — split by market regime. Also feeds the learning layer. |
| **Exports** | CSV trade log (tax-friendly) + monthly PDF statement. |

---

## 11. Configuration & security

- Config via YAML for non-secrets (mode, symbols, strategies + params, risk limits) + **environment
  variables for all secrets** (Binance API key/secret, Telegram bot token, dashboard auth token).
- Binance API key created **without withdrawal permission** (trade + read only); IP-restricted to
  the Oracle VM where possible.
- Dashboard behind a token / basic auth since the VM is public; bind to localhost + SSH tunnel as an
  even safer option.
- Secrets never committed; `.env` / config-with-secrets gitignored.

---

## 12. Deployment (Oracle Cloud Always Free)

- Cross-compile the single static Go binary for ARM64 (pure-Go SQLite driver keeps this trivial —
  no CGO).
- Run under `systemd` (auto-restart on crash, start on boot). SQLite file + config live on the VM;
  periodic backup of the DB file.
- Telegram via long-polling → no inbound ports required for the bot. Only the dashboard port (or an
  SSH tunnel) is exposed, behind auth.

---

## 13. Open items to verify at implementation time

These are deliberately deferred to the writing-plans / build phase (documentation discovery), not
guessed at now:

- **Go Binance library:** `adshao/go-binance` is the leading candidate (REST + WS, Spot + Futures);
  confirm current maintenance status and Spot Testnet support vs Binance's official Go connector.
- **Binance Spot Testnet** base URLs, key provisioning, and feature parity with production.
- **`modernc.org/sqlite`** current version + any ARM64 caveats.
- **Oracle Always Free** ARM capacity/region availability and Binance API geo-restrictions for the
  chosen VM region.
- Decimal library choice (`shopspring/decimal` vs scaled ints) — finalize in Phase 0.

---

## 14. Explicit non-goals (YAGNI)

- No futures, margin, or leverage (Spot only).
- No black-box ML model with direct order control.
- No multi-exchange abstraction yet (Binance only; keep `execution`/`marketdata` interfaces clean
  enough that it's *possible* later, but don't build for it now).
- No separate frontend framework / SPA build pipeline (dashboard is server-rendered + SSE).
- No high-frequency / co-located / sub-second latency optimization.
