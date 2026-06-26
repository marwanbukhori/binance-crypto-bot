# tradebot — Binance Spot Trading Bot

A self-hosted, automated **Binance.com Spot** trading bot in Go. It ingests live market data,
makes long-only decisions from rule-based strategies, passes every decision through a
non-bypassable risk gate, executes orders, records everything to SQLite, learns from the record,
and is controlled from your phone via Telegram and a web dashboard.

> ⚠️ **Not financial advice. No profit guarantee.** Any automated strategy can and will lose money.
> Nothing here is backtested on your data yet — every threshold is a reasoned default and a
> *hypothesis*. Validate on backtest → paper → testnet-live → tiny approve-first live before trusting
> it, and only ever trade money you can afford to lose. See [Safety](#safety--risk-model).

---

## Why it's built this way

1. **You win by not losing.** The risk gate is the most important code in the repo and cannot be
   bypassed. It gets the strictest tests.
2. **One pipeline, three modes.** Backtest, paper (testnet), and live run the *exact same*
   `marketdata → strategy → regime → decision → risk → execution → portfolio → store` code. Only the
   data source and the executor are swapped, so nothing you validate on history breaks in production.
3. **Record everything.** Every signal, decision, rejection, fill, and P&L snapshot is persisted;
   the dashboard and learning layer are readers of that record.
4. **Single static binary.** Bot + dashboard + Telegram in one process — perfect for one free ARM VM.

---

## Features

- **Strategies:** `ema_cross_trend`, `rsi_bb_reversion`, `donchian_breakout`, `macd_momentum`, built
  on a pure-function indicator library (EMA, SMA, RSI, MACD, Bollinger, ATR, ADX).
- **Market-regime classifier** (TrendingUp/Down, Ranging, High-Vol, **Low-Vol-Chop**) that disables
  trend/breakout/momentum strategies in choppy markets, leaving only mean-reversion.
- **Non-bypassable risk gate:** ATR-based position sizing, volatility-aware take-profit gated to a
  post-fee reward:risk ≥ 1.3, max % per trade, portfolio-wide deployment cap, daily/weekly loss
  limits with a **kill-switch**, a hard-flatten drawdown ceiling, and a candle-aligned post-loss
  cooldown. Same-symbol long netting prevents correlated stacking.
- **Backtester** that replays the real pipeline with pessimistic intrabar fills, plus a
  **fee-sensitivity sweep** (0.20 / 0.30 / 0.45% round-trip) as the graduation gate.
- **Real-time paper trading** on Binance testnet WebSocket streams with simulated fills.
- **Telegram control:** `/status` `/positions` `/pause` `/resume` `/kill` and tap-to-approve trades,
  authorized to your chat ID only.
- **Web dashboard:** equity chart, after-fee trades ledger, decisions feed, per-regime scorecards,
  and a Monte-Carlo projection cone — token-authenticated (cookie, POST-only controls, constant-time
  compare).
- **Learning layer:** per-strategy/per-regime scorecards, honest Monte-Carlo projections with
  risk-of-ruin, and walk-forward (out-of-sample) parameter optimization.
- **Live execution:** HMAC-SHA256-signed Binance REST orders with real exchange filters, defaulting
  to approve-first.
- **Deploy-ready:** static ARM64 cross-compile, hardened `systemd` unit, distroless `Dockerfile`,
  and an Oracle Cloud Always-Free runbook.

---

## Architecture

```
Binance WS (klines + user-data) ┐
Binance REST (history backfill) ├─► marketdata ──candles──► strategy ──signals──┐
                                │                              ▲                 ▼
                                │                       regime + weights   decision (nets longs)
                                │                              │                 │
                                │                         learning              ▼
fills ──────────────────────────┴──► portfolio ◄──┐    (offline)         ⛔ risk gate ⛔
                                         │         │                           │ approved
                                         ▼         └──── reads ───┐            ▼
                                   store (SQLite) ◄───────────────┴──┐    execution ──► Binance
                                         ▲                           │        │
                                   dashboard (web)        telegram (control) ◄┘
```

### Packages (`internal/`)

| Package | Responsibility |
|---|---|
| `domain` | Core types: `Candle`, `Signal`, `Intent`, `Order`, `Fill`, `Position`, `Action` |
| `config` | YAML config + env-only secrets loader |
| `indicators` | Pure functions: EMA, SMA, RSI, MACD, Bollinger, ATR, ADX |
| `strategy` | `Strategy` interface + the four strategies |
| `regime` | Market-regime classifier |
| `decision` | Signal aggregation, regime gating, same-symbol netting |
| `risk` | Non-bypassable gate: sizing, R:R gate, guard (loss limits/kill-switch/hard-flatten), cooldown |
| `execution` | `Executor` interface; `Simulated` (backtest/paper) + `Live` (signed) |
| `portfolio` | Balances, positions, realized/unrealized P&L |
| `store` | SQLite recorder + dashboard/learning queries |
| `backtest` | Pipeline replay, pessimistic exits, report, fee sweep, multi-symbol |
| `marketdata` | Binance REST klines/backfill + reconnecting WebSocket + rolling buffer |
| `engine` | Live engine driving the pipeline per closed candle |
| `telegram` | Bot API client, notifier, command/approval parsing, long-poll loop |
| `control` | Race-free control surface (pause/kill/approvals) |
| `dashboard` | Embedded HTTP server, JSON API, HTML page, `/healthz` |
| `learning` | Scorecards, Monte-Carlo projection, walk-forward optimization |
| `binance` | Signed REST client (HMAC), account, market orders, exchange filters |
| `version` | Build version |

---

## Quick start

```bash
# build & test
make build          # -> bin/bot
make test
make vet

# 1) Backtest a strategy (validate before risking anything)
./bin/bot backtest --symbol BTCUSDT --interval 1h --bars 1000
#    prints the report + the 0.20/0.30/0.45% fee-sensitivity sweep

# 2) Paper trade on testnet (live prices, fake money)
cp deploy/config.sample.yaml config.yaml   # mode: paper, testnet: true
cp .env.example .env                        # fill in secrets (see below)
./bin/bot --config config.yaml

# 3) Inspect recorded performance
./bin/bot learn     # scorecards by regime + Monte-Carlo projection

# dashboard (if enabled): http://localhost:8080/?token=<DASHBOARD_TOKEN>  (once; then cookie-auth)
```

### Configuration

Non-secrets live in `config.yaml` (start from `deploy/config.sample.yaml`); **all secrets come from
environment variables** (`.env`, never committed):

```bash
BINANCE_API_KEY=        # trade + read ONLY, NO withdrawal; IP-restrict to your host
BINANCE_API_SECRET=
TELEGRAM_BOT_TOKEN=
TELEGRAM_CHAT_ID=       # only this chat may command the bot
DASHBOARD_TOKEN=        # long random string
```

Key `config.yaml` knobs: `mode` (`backtest|paper|live`), `exchange.testnet`, `symbols`, per-strategy
`params`, the `risk` block (sizing + all guardrails), `control.autonomous` (approve-first when
false), and `dashboard`/`notify` toggles.

---

## Safety & risk model

- **Long-only spot** — you can't be liquidated or go negative; worst case is the value of what you hold.
- **Effective per-trade risk is ~0.1–0.3%** (the 5% notional cap binds before the 1% risk ceiling);
  every order logs the binding constraint and realized risk.
- **Guardrails are non-bypassable:** daily/weekly loss limits trip a persisted kill-switch (halts new
  entries, exits still run); a −6% drawdown triggers hard-flatten. `/kill` does the same from Telegram.
- **Stops are best-effort, not guaranteed** — a gap can fill below the stop. The daily limit + 6%
  hard-flatten + kill-switch are the real catastrophe brakes.
- **The go-live ladder** (full detail in [`docs/DEPLOY.md`](docs/DEPLOY.md)):
  **backtest → testnet paper → testnet live (real order placement, fake funds) → prod live
  approve-first (tiny size) → autonomous** — and only after the
  [pre-autonomous items](ROADMAP.md) land.

---

## Deployment

Built to run 24/7 on an **Oracle Cloud Always-Free ARM VM** as a single static binary under
`systemd`. Cross-compile with `make build-arm64`; full runbook (VM setup, API key creation, systemd
install, DB backup, health checks) is in [`docs/DEPLOY.md`](docs/DEPLOY.md). A distroless
[`deploy/Dockerfile`](deploy/Dockerfile) is also provided.

Health probe: `GET /healthz` (unauthenticated) → `{"status":"ok","paused":<bool>}`.

---

## Testing

```bash
go test ./...                 # full suite (114 tests across 19 packages)
go test -race ./internal/...  # concurrency-sensitive packages
```

TDD throughout: the indicators and the risk gate have the highest coverage; the backtester doubles
as a regression harness; the HMAC signer is verified against an independently-computed vector.

---

## Documentation

- [`docs/superpowers/specs/`](docs/superpowers/specs/) — the design spec (adversarially hardened)
- [`docs/superpowers/plans/`](docs/superpowers/plans/) — the phased implementation plans (0–1 … 7)
- [`docs/DEPLOY.md`](docs/DEPLOY.md) — Oracle deployment runbook + go-live ladder
- [`CLAUDE.md`](CLAUDE.md) — guidance for AI agents working in this repo
- [`ROADMAP.md`](ROADMAP.md) — what's next (incl. a possible **stocks/ETF** module)

---

## License

Personal project — no warranty. Use at your own risk.
