# CLAUDE.md

Guidance for AI agents (and humans) working in this repo. Read this before changing code.

## What this is

`tradebot` — a Go (module `tradebot`, Go 1.22+) automated **Binance.com Spot**, long-only trading
bot. Single static binary: engine + web dashboard + Telegram, recording to SQLite. See
[README.md](README.md) for the feature tour and [ROADMAP.md](ROADMAP.md) for what's next.

## Commands

```bash
make build              # -> bin/bot
make test               # go test ./...
make vet                # go vet ./...
make build-arm64        # static linux/arm64 binary for the Oracle VM

go test ./...                       # full suite (must stay green)
go test -race ./internal/control/ ./internal/engine/ ./internal/telegram/
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build ./cmd/bot   # cross-compile check

# run modes (mode comes from config.yaml)
./bin/bot backtest --symbol BTCUSDT --interval 1h --bars 1000
./bin/bot --config config.yaml      # paper or live, per cfg.Mode
./bin/bot learn                     # scorecards + projection from tradebot.db
```

Always run `go build ./... && go test ./... && go vet ./...` before committing. Pure-Go SQLite
(`modernc.org/sqlite`) keeps `CGO_ENABLED=0` cross-compiles trivial — don't introduce CGO deps.

## Non-negotiable invariants

These are the rules the whole design rests on. A change that violates one is a bug even if tests pass:

1. **One pipeline, three modes.** Backtest / paper / live MUST run the same
   `strategy → regime → decision → risk → execution → portfolio → store` logic. Only the data source
   and the `execution.Executor` differ. Never fork the trading logic per mode — if live and backtest
   could decide differently on identical candles, that's a defect.
2. **No look-ahead.** Indicators for signals AND the ATR used for stops use **closed candles only**
   (`Candle.Closed` / WS `k.x == true`). The backtester applies pessimistic intrabar fills: TP/SL
   active only from the candle *after* entry; a candle straddling both → assume the stop filled first.
3. **Long-only.** Quantity is always ≥ 0; a `Sell` only reduces a position to cash. No shorting.
4. **The risk gate (`internal/risk`) is non-bypassable.** Every order goes through `Gate.Evaluate`;
   it logs the binding constraint + effective-risk %. Sizing only ever rounds DOWN
   (realized risk ≤ budget). Guardrails (loss limits, kill-switch, hard-flatten, cooldown) apply in
   every mode. Don't add an order path that skips it.
5. **Race-free control.** The Telegram poller goroutine mutates ONLY the mutex-guarded
   `control.Controller` (pause/kill/approval decisions). It NEVER touches trade/portfolio state — the
   engine drains approved orders and executes them in its own goroutine. The dashboard handlers read
   the store + `Controller.Status()` only, never the live portfolio.
6. **Pinned canonical constants.** EMA 9/21, ADX/ATR/RSI 14, MACD 12/26/9, BB 20/2.0, SMA200, EMA100
   are not tunable. Optimization (`internal/learning`) only sweeps the declared knobs (`adx_min`,
   `tp_reward_mult`, `sl_atr_mult`, …) and selects on TRAIN, reports on TEST (out-of-sample).
7. **Honest projections only.** Monte-Carlo bootstraps recorded net-of-fee trades; always labeled
   "past performance, not a guarantee." Never fabricate a forward number.
8. **Secrets via env only** (`config.Secrets`, `yaml:"-"`). Never log a token/secret or put it in an
   error message or URL.

## Conventions

- **TDD.** Write the failing test, watch it fail, implement minimally, watch it pass, commit. Tests
  are the contract; don't weaken a test to make it pass.
- **Small, focused files**, one responsibility each. Follow existing patterns in the package.
- **Commit identity:** commits and pushes must be authored **`marwanbukhori`
  <marwanbukhori.dev@gmail.com>**. Do NOT add any `Co-Authored-By: Claude` / "Generated with" trailer
  to commit messages or PR bodies.
- **Pushing:** the `gh` active account can silently flip to `marwan-virtus` (a 403 on push). Run
  `gh auth switch --hostname github.com --user marwanbukhori` before pushing. Remote:
  `github.com/marwanbukhori/binance-crypto-bot`; working branch `binance-go-trade-bot`.
- One commit per logical change; conventional-ish messages (`feat:`, `fix:`, `chore:`).

## How to add a strategy

1. Implement the `strategy.Strategy` interface (`Name`, `Kind`, `Symbol`, `Timeframe`, `Warmup`,
   `Evaluate(history, inPosition) *Signal`) in `internal/strategy/<name>.go`. Emit signals on candle
   close only; carry an ATR `StopDist` and a `TPDist` on a Buy.
2. `Kind()` must be one of `trend|reversion|breakout|momentum` (drives regime gating in
   `decision.KindEnabled`).
3. `Warmup()` returns the longest indicator window — `Evaluate` must emit nothing before it's full.
4. Add table/fixture tests. Multi-gate entries are fiddly: if a fixture doesn't trigger under real
   indicator dynamics, adjust the **fixture** (candle series), never the gate logic or the assertion.
5. Wire it into the strategy factory in `cmd/bot/main.go` (currently a single `ema_cross_trend`).

## Where things live

- Design spec: `docs/superpowers/specs/2026-06-26-binance-spot-trading-bot-design.md`
- Phase plans: `docs/superpowers/plans/2026-06-26-trading-bot-phase-{0-1..7}.md`
- Build ledger: `.superpowers/sdd/progress.md` (gitignored scratch)
- Deploy runbook: `docs/DEPLOY.md`

## Known gotchas

- **Binance HMAC test vector:** Binance's *documented* example signature (`c8db56…`) is a long-standing
  doc typo. The correct HMAC-SHA256 of that query/secret is `b89008e7…` (verified vs OpenSSL + Python).
  The signer in `internal/binance/sign.go` is correct — don't "fix" it back to the doc value.
- **Live mode is single-symbol** for now: `runLive` fails closed (`log.Fatal`) if `len(symbols) > 1`,
  because the engine applies one `risk.Filters` set to all symbols. Per-symbol live filters is a
  roadmap item.
- **Before autonomous live**, three robustness gaps must land (see ROADMAP / DEPLOY): order
  reconciliation, 429/418 backoff, clientOrderId-based retry. Run approve-first until then.
- **Binance kline WS JSON is case-collision-prone.** A real kline `k` object carries `"L"` (last
  trade id, a number) alongside `"l"` (low, a string); Go's `encoding/json` matches keys
  case-insensitively, so without explicit exact-case fields (`LastID json:"L"`, `TakerV json:"V"`,
  …) the number lands in the low/string field and `Unmarshal` errors on **every** live message —
  the bot then silently emits zero candles. The `klineMsg.K` struct in `internal/marketdata/ws.go`
  declares those absorber fields; `TestParseKlineEventRealMessage` guards it. Any new WS struct that
  mirrors a Binance payload must include the colliding uppercase keys, and tests must use a *full*
  real message, not a trimmed fixture.
