# Roadmap

Status of `tradebot` and where it could go. Items are roughly ordered by priority within each
horizon. Nothing here is a commitment — it's a map.

## ✅ Done (v1 — phases 0–7)

- Domain model, config (YAML + env secrets), pure-function indicator library (EMA/SMA/RSI/MACD/Bollinger/ATR/ADX).
- Four strategies (`ema_cross_trend`, `rsi_bb_reversion`, `donchian_breakout`, `macd_momentum`) + market-regime classifier.
- Non-bypassable risk gate: ATR sizing, vol-aware TP (post-fee R:R ≥ 1.3), notional/portfolio caps, daily/weekly loss limits, kill-switch, hard-flatten, candle-aligned cooldown, same-symbol netting.
- Backtester with pessimistic intrabar fills + fee-sensitivity sweep; multi-symbol backtest.
- Real-time paper trading on Binance testnet WebSocket; persisted kill-state.
- Telegram control (chat-ID authorized) + web dashboard (cookie auth, POST-only controls, equity chart, trades ledger, scorecards, Monte-Carlo cone).
- Learning layer: per-regime scorecards, Monte-Carlo projection + risk-of-ruin, walk-forward parameter optimization, `bot learn`.
- Signed live executor (HMAC verified), real exchange filters, `/healthz`, `mode: live` (approve-first default).
- Deployment: ARM64 cross-compile, hardened `systemd` unit, distroless `Dockerfile`, Oracle Always-Free runbook.

---

## 🔜 Near-term — required before *autonomous* live trading

These are the gaps that make unattended live trading safe. **Run approve-first until they land.**

- [ ] **Order reconciliation** — on startup and after every WebSocket reconnect, fetch open orders +
      balances from the exchange and reconcile against in-memory + SQLite state. Never trust memory alone.
- [ ] **Rate-limit resilience** — respect Binance request weights; exponential backoff on HTTP 429/418
      instead of hammering.
- [ ] **Idempotent retries** — reuse a stable `clientOrderId` so a timed-out MARKET order can be safely
      re-checked/retried without double-filling. (The id is already sent; the retry/dedupe loop is not.)
- [ ] **Per-symbol live filters** — fetch `exchangeInfo` per symbol and key `risk.Filters` by symbol so
      live mode can trade more than one symbol (currently `runLive` fails closed on `len(symbols) > 1`).
- [ ] **Server-time sync** — periodically sync to Binance server time and adjust `recvWindow` to avoid
      timestamp rejections on a drifting clock.

## 🟡 Mid-term — capability & UX

- [ ] **Live trailing stops** (the engine has the field; wire the trailing logic into the live path).
- [ ] **Partial / scaled exits** (the portfolio already expenses fees proportionally on partial sells).
- [ ] **Multiple concurrent strategies per symbol**, with the decision aggregator + netting already in place.
- [ ] **Auto-apply learned weights** — feed `learning` scorecards back into `decision` to weight/disable
      strategies per regime, gated by a Telegram approval.
- [ ] **Full dashboard (spec §9)** — the remaining pages (per-regime strategy scorecard matrix, richer
      projection page, settings/audit/log tail) and live updates via SSE instead of 5s polling.
- [ ] **Backtest data cache** — store historical klines in SQLite so backtests/sweeps don't refetch.
- [ ] **Automated walk-forward** — schedule periodic re-optimization with out-of-sample acceptance gates.
- [ ] **BNB fee discount** modeling end-to-end; multi-timeframe inputs per strategy.
- [ ] **Notifications polish** — daily/weekly P&L digest formatting, error/heartbeat alerts.

## 🟢 Longer-term — new markets & intelligence

- [ ] **Binance USDⓈ-M Futures module** — leverage + shorting. This is a *separate* risk engine
      (liquidation, funding rates, margin); reuse the strategy/regime/decision layers, add a futures
      `Executor` + a futures-aware risk gate. Deliberately kept out of v1.
- [ ] **📈 Stocks / ETFs integration** (the natural next asset class) — see below.
- [ ] **ML signal layer** — a model as an *advisor* (feature → probability) that the rule engine can
      consult, never as the direct order trigger. Trained on the recorded trade/feature history.
- [ ] **Multi-exchange abstraction** — the `Executor` + `marketdata` interfaces already isolate the
      exchange; generalize to Bybit/OKX/Coinbase if desired.

---

## 📈 Stocks / ETF integration — design sketch

The architecture is already asset-agnostic where it matters: `strategy`, `indicators`, `regime`,
`decision`, `risk`, `portfolio`, and `store` know nothing about Binance. Adding equities is mostly a
**new broker adapter + a session/calendar layer**, not a rewrite.

**Recommended broker: [Alpaca](https://alpaca.markets)** — commission-free US equities + ETFs, a clean
REST/WebSocket API, **fractional shares**, and a free **paper-trading** environment that mirrors the
testnet-first ladder this bot already follows. (IBKR / Tradier are alternatives with more breadth but
heavier APIs.)

**What to build:**
- `internal/alpaca` — signed REST client (auth headers, place/cancel order, account, positions) +
  a market-data WebSocket, implementing the existing `execution.Executor` and a `marketdata.Stream`.
- A **market-session/calendar** layer — unlike 24/7 crypto, equities have trading hours, holidays,
  pre/post-market, and settlement (T+1). The engine must gate entries to open sessions and handle
  overnight gaps explicitly (the bot is already gap-aware in spirit; make it first-class).
- **Symbol + fee model** differences — equity symbols, lot/fractional rules, and a different (often
  zero-commission but spread/slippage-bearing) cost model feeding the same fee-sweep acceptance gate.
- **Regulatory guardrails** — the US **Pattern Day Trader (PDT)** rule (≥ $25k for >3 day-trades/5
  days on margin), and cash-account settlement. These belong in the risk gate as hard, configurable
  blocks.
- Config: add an `asset_class` / `broker` selector so one binary can run crypto or equities (or, later,
  both as separate processes sharing the strategy library).

**What's reused unchanged:** every strategy and indicator, the regime classifier, the decision
aggregator + netting, the risk sizing/guardrails, the backtester, the learning layer, the dashboard,
and the Telegram control surface. That reuse is the payoff of the "one pipeline, asset-agnostic core"
design — adding stocks is an adapter, not a second bot.

---

## 🚫 Explicit non-goals

- High-frequency / sub-second / co-located trading.
- API keys with **withdrawal** permission — never.
- A black-box model with **direct** order control (models may advise, the rule engine decides).
- Anything that bypasses the risk gate or the kill-switch.
