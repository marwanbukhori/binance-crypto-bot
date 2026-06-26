# Trading Bot Phase 7 (Live Execution + Oracle Deployment) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Place real orders and ship the bot: a signed Binance REST executor (account + market orders), real per-symbol exchange filters, a `/healthz` endpoint, `mode: live` wiring (real keys, signed executor, approve-first default), and the deployment artifacts to run 24/7 on an Oracle Cloud Always-Free ARM VM.

**Architecture:** A signed REST client (HMAC-SHA256) implements the existing `execution.Executor` interface, so live mode reuses the entire Phase 0–6 pipeline unchanged — only the executor and the exchange filters differ from paper. Live mode can point at the **testnet** (`exchange.testnet: true`) to rehearse real order placement safely before production. Deployment is a single static ARM64 binary under `systemd`.

**Tech Stack:** Go 1.22 stdlib (`crypto/hmac`, `crypto/sha256`, `net/http`). No new deps. Plus ops files (Makefile, systemd, Dockerfile, docs).

## Global Constraints

- **Money-touching code is high-risk.** The signed client + live executor get the strongest tests; the HMAC signer is verified against Binance's documented known-answer vector.
- **Live is opt-in and safe-by-default.** `mode: live` requires real API keys; it defaults to **approve-first** (`control.autonomous: false`) unless explicitly set true. The API key must be **trade+read, NO withdrawal**.
- **Reuse the pipeline.** Live mode changes ONLY the executor (signed) and the filters (real `exchangeInfo`); strategy/regime/decision/risk/guard/cooldown/portfolio/store are identical to paper.
- **Real exchange filters.** Never hardcode lot size / min-notional in live; fetch `exchangeInfo` per symbol at startup.
- **Module path:** `tradebot`. TDD + one commit per task. No Claude attribution in commits.

---

## File Structure

```
internal/binance/sign.go         HMAC-SHA256 request signer
internal/binance/client.go       signed client: GetAccount, NewMarketOrder
internal/binance/filters.go      exchangeInfo -> per-symbol risk.Filters
internal/execution/live.go       LiveExecutor implementing execution.Executor (wraps the signed client)
internal/dashboard/api.go        + GET /healthz (unauthenticated liveness)
cmd/bot/main.go                  + runLive() and mode==live dispatch
Makefile                         + build-arm64 cross-compile target
deploy/tradebot.service          systemd unit
deploy/Dockerfile                multi-stage static build
deploy/config.sample.yaml        sample config
.env.example                     secret template
docs/DEPLOY.md                   Oracle Always-Free runbook
```

---

## Task 1: HMAC request signer + signed client

**Files:** Create `internal/binance/sign.go`, `internal/binance/client.go`, `internal/binance/sign_test.go`, `internal/binance/client_test.go`

**Interfaces:**
- Produces:
  - `func Sign(query, secret string) string` — hex HMAC-SHA256 of `query` with `secret`.
  - `type Client struct {...}`, `func NewClient(apiKey, apiSecret string, testnet bool) *Client`, `func (c *Client) WithHTTP(h *http.Client, base string) *Client`
  - `type Balance struct { Asset string; Free float64 }`, `func (c *Client) GetAccount() ([]Balance, error)` (signed GET /api/v3/account)
  - `type OrderFill struct { Price, Qty, Commission float64 }`, `func (c *Client) NewMarketOrder(symbol, side string, qty float64) (OrderFill, error)` (signed POST /api/v3/order, type MARKET, averages the returned `fills`).
  - Signed requests append `timestamp` + `recvWindow=5000` and the `signature`, and send `X-MBX-APIKEY`.

- [ ] **Step 1: Failing test** — `internal/binance/sign_test.go` (Binance's documented known-answer vector):
```go
package binance

import "testing"

func TestSignKnownVector(t *testing.T) {
	secret := "NhqPtmdSJYdKjVHjA7PZj4Mge3R5YNiP1e3UZjInClVN65XAbvqqM6A7H5fATj0"
	query := "symbol=LTCBTC&side=BUY&type=LIMIT&timeInForce=GTC&quantity=1&price=0.1&recvWindow=5000&timestamp=1499827319559"
	want := "c8db56825ae71d6d79447849e617115f4a920fa2acdcab2b053c4b2838bd6b71"
	if got := Sign(query, secret); got != want {
		t.Fatalf("Sign mismatch:\n got %s\nwant %s", got, want)
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/binance/ -run Sign` → FAIL.

- [ ] **Step 3: Implement** — `internal/binance/sign.go`:
```go
package binance

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// Sign returns the hex HMAC-SHA256 of query keyed by secret (Binance signature scheme).
func Sign(query, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(query))
	return hex.EncodeToString(mac.Sum(nil))
}
```
`internal/binance/client.go`:
```go
package binance

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const (
	prodBase    = "https://api.binance.com"
	testnetBase = "https://testnet.binance.vision"
)

type Client struct {
	key, secret string
	base        string
	http        *http.Client
	nowMs       func() int64
}

func NewClient(apiKey, apiSecret string, testnet bool) *Client {
	base := prodBase
	if testnet {
		base = testnetBase
	}
	return &Client{key: apiKey, secret: apiSecret, base: base, http: &http.Client{Timeout: 15 * time.Second},
		nowMs: func() int64 { return time.Now().UnixMilli() }}
}

func (c *Client) WithHTTP(h *http.Client, base string) *Client {
	c.http = h
	c.base = base
	return c
}

// signedRequest builds a signed request; params must NOT include timestamp/signature.
func (c *Client) signedRequest(method, path string, params url.Values) (*http.Request, error) {
	if params == nil {
		params = url.Values{}
	}
	params.Set("recvWindow", "5000")
	params.Set("timestamp", strconv.FormatInt(c.nowMs(), 10))
	q := params.Encode()
	q += "&signature=" + Sign(q, c.secret)
	req, err := http.NewRequest(method, c.base+path+"?"+q, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-MBX-APIKEY", c.key)
	return req, nil
}

func (c *Client) do(req *http.Request, out any) error {
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		var b [2048]byte
		n, _ := resp.Body.Read(b[:])
		return fmt.Errorf("binance %s: %d %s", req.URL.Path, resp.StatusCode, string(b[:n]))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

type Balance struct {
	Asset string
	Free  float64
}

func (c *Client) GetAccount() ([]Balance, error) {
	req, err := c.signedRequest(http.MethodGet, "/api/v3/account", nil)
	if err != nil {
		return nil, err
	}
	var raw struct {
		Balances []struct {
			Asset string `json:"asset"`
			Free  string `json:"free"`
		} `json:"balances"`
	}
	if err := c.do(req, &raw); err != nil {
		return nil, err
	}
	out := make([]Balance, 0, len(raw.Balances))
	for _, b := range raw.Balances {
		f, _ := strconv.ParseFloat(b.Free, 64)
		out = append(out, Balance{Asset: b.Asset, Free: f})
	}
	return out, nil
}

type OrderFill struct {
	Price, Qty, Commission float64
}

func (c *Client) NewMarketOrder(symbol, side string, qty float64) (OrderFill, error) {
	params := url.Values{
		"symbol":   {symbol},
		"side":     {side},
		"type":     {"MARKET"},
		"quantity": {strconv.FormatFloat(qty, 'f', -1, 64)},
	}
	req, err := c.signedRequest(http.MethodPost, "/api/v3/order", params)
	if err != nil {
		return OrderFill{}, err
	}
	var raw struct {
		Fills []struct {
			Price string `json:"price"`
			Qty   string `json:"qty"`
			Comm  string `json:"commission"`
		} `json:"fills"`
	}
	if err := c.do(req, &raw); err != nil {
		return OrderFill{}, err
	}
	var notional, totQty, comm float64
	for _, f := range raw.Fills {
		p, _ := strconv.ParseFloat(f.Price, 64)
		q, _ := strconv.ParseFloat(f.Qty, 64)
		cm, _ := strconv.ParseFloat(f.Comm, 64)
		notional += p * q
		totQty += q
		comm += cm
	}
	avg := 0.0
	if totQty > 0 {
		avg = notional / totQty
	}
	return OrderFill{Price: avg, Qty: totQty, Commission: comm}, nil
}
```

- [ ] **Step 4: Failing test (client)** — `internal/binance/client_test.go`:
```go
package binance

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewMarketOrderAveragesFillsAndSigns(t *testing.T) {
	var sawSig, sawKey bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawSig = r.URL.Query().Get("signature") != ""
		sawKey = r.Header.Get("X-MBX-APIKEY") == "k"
		if !strings.Contains(r.URL.Path, "/order") {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.Write([]byte(`{"fills":[{"price":"100.0","qty":"0.6","commission":"0.06"},{"price":"110.0","qty":"0.4","commission":"0.04"}]}`))
	}))
	defer srv.Close()
	c := NewClient("k", "s", false).WithHTTP(srv.Client(), srv.URL)
	f, err := c.NewMarketOrder("BTCUSDT", "BUY", 1.0)
	if err != nil {
		t.Fatalf("order: %v", err)
	}
	if !sawSig || !sawKey {
		t.Fatalf("request must be signed (sig=%v key=%v)", sawSig, sawKey)
	}
	// avg = (100*0.6 + 110*0.4)/1.0 = 104
	if f.Price < 103.99 || f.Price > 104.01 || f.Qty < 0.999 {
		t.Fatalf("bad averaged fill: %+v", f)
	}
}
```

- [ ] **Step 5: Run** `go test ./internal/binance/` → PASS. **Commit:** `git add internal/binance && git commit -m "feat: add signed Binance REST client (account + market orders)"`

---

## Task 2: Exchange filters + live executor

**Files:** Create `internal/binance/filters.go`, `internal/binance/filters_test.go`, `internal/execution/live.go`, `internal/execution/live_test.go`

**Interfaces:**
- `binance`: `func (c *Client) SymbolFilters(symbol string) (risk.Filters, error)` — GET `/api/v3/exchangeInfo?symbol=X` (public), parse `LOT_SIZE` (stepSize, minQty) and `NOTIONAL`/`MIN_NOTIONAL` (minNotional) into `risk.Filters`.
- `execution`: `type LiveExecutor struct {...}`, `func NewLive(c *binance.Client) *LiveExecutor` implementing `Executor`: `Execute(o domain.Order, _ domain.Candle)` calls `c.NewMarketOrder(o.Symbol, o.Side.String(), o.Qty)` and returns a `domain.Fill{Symbol, Side, Qty: fill.Qty, Price: fill.Price, Fee: fill.Commission, Time: o.Time}`.

- [ ] **Step 1: Failing test** — `internal/binance/filters_test.go`:
```go
package binance

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSymbolFiltersParse(t *testing.T) {
	body := `{"symbols":[{"symbol":"BTCUSDT","filters":[
	  {"filterType":"LOT_SIZE","stepSize":"0.00001000","minQty":"0.00001000"},
	  {"filterType":"NOTIONAL","minNotional":"10.00000000"}
	]}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
	defer srv.Close()
	c := NewClient("k", "s", false).WithHTTP(srv.Client(), srv.URL)
	f, err := c.SymbolFilters("BTCUSDT")
	if err != nil {
		t.Fatalf("filters: %v", err)
	}
	if f.StepSize != 0.00001 || f.MinQty != 0.00001 || f.MinNotional != 10 {
		t.Fatalf("bad filters: %+v", f)
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/binance/ -run Filters` → FAIL.

- [ ] **Step 3: Implement** — `internal/binance/filters.go`:
```go
package binance

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"tradebot/internal/risk"
)

func (c *Client) SymbolFilters(symbol string) (risk.Filters, error) {
	resp, err := c.http.Get(c.base + "/api/v3/exchangeInfo?symbol=" + symbol)
	if err != nil {
		return risk.Filters{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return risk.Filters{}, fmt.Errorf("exchangeInfo: status %d", resp.StatusCode)
	}
	var raw struct {
		Symbols []struct {
			Filters []struct {
				Type        string `json:"filterType"`
				StepSize    string `json:"stepSize"`
				MinQty      string `json:"minQty"`
				MinNotional string `json:"minNotional"`
			} `json:"filters"`
		} `json:"symbols"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return risk.Filters{}, err
	}
	if len(raw.Symbols) == 0 {
		return risk.Filters{}, fmt.Errorf("no filters for %s", symbol)
	}
	var f risk.Filters
	num := func(s string) float64 { v, _ := strconv.ParseFloat(s, 64); return v }
	for _, fl := range raw.Symbols[0].Filters {
		switch fl.Type {
		case "LOT_SIZE":
			f.StepSize = num(fl.StepSize)
			f.MinQty = num(fl.MinQty)
		case "NOTIONAL", "MIN_NOTIONAL":
			if fl.MinNotional != "" {
				f.MinNotional = num(fl.MinNotional)
			}
		}
	}
	return f, nil
}
```
`internal/execution/live.go`:
```go
package execution

import (
	"tradebot/internal/binance"
	"tradebot/internal/domain"
)

// LiveExecutor places real market orders via the signed Binance client.
type LiveExecutor struct{ c *binance.Client }

func NewLive(c *binance.Client) *LiveExecutor { return &LiveExecutor{c: c} }

func (e *LiveExecutor) Execute(o domain.Order, _ domain.Candle) (domain.Fill, error) {
	fill, err := e.c.NewMarketOrder(o.Symbol, o.Side.String(), o.Qty)
	if err != nil {
		return domain.Fill{}, err
	}
	return domain.Fill{Symbol: o.Symbol, Side: o.Side, Qty: fill.Qty, Price: fill.Price, Fee: fill.Commission, Time: o.Time}, nil
}
```

- [ ] **Step 4: Failing test (executor)** — `internal/execution/live_test.go`:
```go
package execution

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"tradebot/internal/binance"
	"tradebot/internal/domain"
)

func TestLiveExecutorPlacesOrder(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"fills":[{"price":"60000.0","qty":"0.001","commission":"0.06"}]}`))
	}))
	defer srv.Close()
	c := binance.NewClient("k", "s", false).WithHTTP(srv.Client(), srv.URL)
	ex := NewLive(c)
	fill, err := ex.Execute(domain.Order{Symbol: "BTCUSDT", Side: domain.Buy, Qty: 0.001, Time: 5}, domain.Candle{})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if fill.Price != 60000 || fill.Qty != 0.001 || fill.Fee != 0.06 {
		t.Fatalf("bad fill: %+v", fill)
	}
}
```

- [ ] **Step 5: Run** `go test ./internal/binance/ ./internal/execution/` → PASS. **Commit:** `git add internal/binance internal/execution && git commit -m "feat: add exchange filters and live order executor"`

---

## Task 3: Health endpoint

**Files:** Modify `internal/dashboard/api.go`, `internal/dashboard/api_test.go`

**Interfaces:**
- Add an UNAUTHENTICATED `GET /healthz` to `Handler()` returning `200` and `{"status":"ok","paused":<bool>}` (liveness probe for systemd/uptime checks; no secrets). Everything else stays authed.

- [ ] **Step 1: Failing test** — append to `internal/dashboard/api_test.go`:
```go
func TestHealthzNoAuth(t *testing.T) {
	s, _, _ := newServer(t)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest("GET", "/healthz", nil)) // no token
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "ok") {
		t.Fatalf("healthz must be 200/ok without auth, got %d %s", rr.Code, rr.Body.String())
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/dashboard/ -run Healthz` → FAIL.

- [ ] **Step 3: Implement** — in `Handler()`, register before the authed routes:
```go
mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"status": "ok", "paused": s.ctrl.Paused()})
})
```

- [ ] **Step 4: Run** `go test ./internal/dashboard/` → PASS. **Commit:** `git add internal/dashboard && git commit -m "feat: add unauthenticated /healthz liveness endpoint"`

---

## Task 4: `mode: live` wiring

**Files:** Modify `cmd/bot/main.go`

**Interfaces:**
- Add `runLive(cfg)` and dispatch it from `main()` when `cfg.Mode == "live"`. It mirrors `runPaper` but: requires `cfg.Secrets.BinanceAPIKey`/`Secret` (fatal if missing); builds a `binance.NewClient(key, secret, cfg.Exchange.Testnet)`; fetches the **real starting USDT balance** via `GetAccount`; fetches **real per-symbol filters** via `SymbolFilters` (fatal on error — never trade live with guessed filters); uses `execution.NewLive(client)` as the executor; defaults `autonomous` to `cfg.Control.Autonomous` (approve-first unless explicitly true); everything else (engine, guard, cooldown, telegram, dashboard) identical to paper. Log a clear "LIVE TRADING" banner with the resolved autonomous mode.

> No new unit test (network + real account); covered by Tasks 1–2 and the shared pipeline. Refactor the shared setup from `runPaper` into a helper if it reduces duplication, but keep paper's behavior identical.

- [ ] **Step 1: Implement** — add the `live` dispatch in `main()` (`if cfg.Mode == "live" { runLive(cfg); return }`) and `runLive`:
```go
func runLive(cfg config.Config) {
	if cfg.Secrets.BinanceAPIKey == "" || cfg.Secrets.BinanceAPISecret == "" {
		log.Fatal("live mode requires BINANCE_API_KEY and BINANCE_API_SECRET (trade+read, NO withdrawal)")
	}
	interval := "1h"
	if len(cfg.Strategies) > 0 && cfg.Strategies[0].Timeframe != "" {
		interval = cfg.Strategies[0].Timeframe
	}
	bc := binance.NewClient(cfg.Secrets.BinanceAPIKey, cfg.Secrets.BinanceAPISecret, cfg.Exchange.Testnet)
	// real starting USDT balance
	var startUSDT float64
	bals, err := bc.GetAccount()
	if err != nil {
		log.Fatalf("account: %v", err)
	}
	for _, b := range bals {
		if b.Asset == "USDT" {
			startUSDT = b.Free
		}
	}
	st, err := store.Open("tradebot.db")
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	defer st.Close()
	buf := marketdata.NewBuffer(500)
	rest := marketdata.NewClient(cfg.Exchange.Testnet)
	for _, sym := range cfg.Symbols {
		cs, err := rest.Klines(sym, interval, 500)
		if err != nil {
			log.Printf("warmup %s failed: %v", sym, err)
			continue
		}
		buf.Seed(sym, cs)
	}
	// real exchange filters per symbol (fatal if missing — never guess live)
	filt := risk.Filters{}
	if len(cfg.Symbols) > 0 {
		filt, err = bc.SymbolFilters(cfg.Symbols[0])
		if err != nil {
			log.Fatalf("exchange filters for %s: %v", cfg.Symbols[0], err)
		}
	}
	gate := risk.NewGate(cfg.Risk, cfg.Risk.FeeModel.Majors/100)
	guard := risk.NewGuard(cfg.Risk, startUSDT)
	if killed, _ := st.LoadKillState(); killed {
		guard.Kill()
		log.Print("kill-switch ACTIVE from a prior session — entries blocked until reset")
	}
	cool := risk.NewCooldown(cfg.Risk.PostLossCooldownCandles)
	pf := portfolio.New(startUSDT)
	mk := func(sym string) []strategy.Strategy { return []strategy.Strategy{strategy.NewEMACross(sym, interval, nil)} }
	l := engine.NewLive(cfg.Symbols, mk, gate, guard, cool, execution.NewLive(bc), pf, st, filt, buf)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	var notifier telegram.Notifier = telegram.NoopNotifier{}
	ctrl := control.New()
	if cfg.Secrets.TelegramBotToken != "" {
		chatID, _ := strconv.ParseInt(cfg.Secrets.TelegramChatID, 10, 64)
		tg := telegram.NewClient(cfg.Secrets.TelegramBotToken)
		notifier = telegram.NewTelegramNotifier(tg, chatID)
		go telegram.Poll(ctx, tg, ctrl, chatID, func() string { return ctrl.Status() }, func() string { return ctrl.Status() })
	}
	l.SetControl(ctrl, notifier, cfg.Control.Autonomous)
	if cfg.Dashboard.Enabled && cfg.Secrets.DashboardToken != "" {
		port := cfg.Dashboard.Port
		if port == 0 {
			port = 8080
		}
		srv := dashboard.New(st, ctrl, cfg.Secrets.DashboardToken)
		go func() { _ = http.ListenAndServe(fmt.Sprintf(":%d", port), srv.Handler()) }()
	}
	stream := marketdata.NewWSStream(cfg.Exchange.Testnet, cfg.Symbols, interval)
	defer stream.Close()
	log.Printf("*** LIVE TRADING *** symbols=%v interval=%s startUSDT=%.2f autonomous=%v testnet=%v",
		cfg.Symbols, interval, startUSDT, cfg.Control.Autonomous, cfg.Exchange.Testnet)
	if err := l.Run(ctx, stream); err != nil && err != context.Canceled {
		log.Printf("live run ended: %v", err)
	}
}
```
Add imports `tradebot/internal/binance` (and ensure `execution`, `risk`, etc. already imported).

- [ ] **Step 2: Run** `go build ./cmd/bot && go test ./... && go vet ./...` → all pass.
- [ ] **Step 3: Commit** `git add cmd/bot && git commit -m "feat: wire live trading mode with signed executor and real filters"`

---

## Task 5: Deployment artifacts

**Files:** Modify `Makefile`; create `deploy/tradebot.service`, `deploy/Dockerfile`, `deploy/config.sample.yaml`, `.env.example`, `docs/DEPLOY.md`

**Interfaces:** Ops artifacts; the acceptance check is that the ARM64 binary cross-compiles and the unit/docs are present.

- [ ] **Step 1: Add the ARM cross-compile target** to `Makefile`:
```makefile
build-arm64:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -o bin/bot-arm64 ./cmd/bot
```

- [ ] **Step 2: Create `deploy/tradebot.service`:**
```ini
[Unit]
Description=tradebot (Binance Spot bot)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=tradebot
WorkingDirectory=/opt/tradebot
EnvironmentFile=/opt/tradebot/.env
ExecStart=/opt/tradebot/bot --config /opt/tradebot/config.yaml
Restart=always
RestartSec=5
# hardening
NoNewPrivileges=true
ProtectSystem=strict
ReadWritePaths=/opt/tradebot
ProtectHome=true

[Install]
WantedBy=multi-user.target
```

- [ ] **Step 3: Create `deploy/Dockerfile`:**
```dockerfile
FROM golang:1.22-alpine AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -o /bot ./cmd/bot

FROM gcr.io/distroless/static-debian12
COPY --from=build /bot /bot
ENTRYPOINT ["/bot", "--config", "/config/config.yaml"]
```

- [ ] **Step 4: Create `deploy/config.sample.yaml`** (the documented config with safe defaults — paper/testnet, approve-first) and **`.env.example`:**
```yaml
# deploy/config.sample.yaml
mode: paper            # paper -> live only after testnet validation
exchange: { testnet: true }
symbols: [BTCUSDT]
strategies:
  - { name: ema_cross_trend, enabled: true, timeframe: 1h, params: { adx_min: 25, sl_atr_mult: 2.0, tp_reward_mult: 1.6 } }
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
control: { autonomous: false }   # approve-first; flip to true only once trusted
notify: { telegram: { enabled: true } }
dashboard: { enabled: true, port: 8080 }
```
```bash
# .env.example  (copy to .env; NEVER commit .env)
BINANCE_API_KEY=        # trade + read ONLY, NO withdrawal; IP-restrict to the VM
BINANCE_API_SECRET=
TELEGRAM_BOT_TOKEN=
TELEGRAM_CHAT_ID=       # your chat id — only this chat may command the bot
DASHBOARD_TOKEN=        # long random string; dashboard is token-gated
```

- [ ] **Step 5: Create `docs/DEPLOY.md`** — an Oracle Always-Free runbook covering: create the ARM VM (Ampere, Ubuntu); `make build-arm64` and scp the binary (or build on the VM / use the Dockerfile); create the `tradebot` user + `/opt/tradebot`; place `bot`, `config.yaml`, `.env`; create the Binance API key (trade+read, **no withdrawal**, IP-restricted); install + enable `tradebot.service`; verify with `curl localhost:8080/healthz`; back up `tradebot.db` (cron); and the **safe go-live ladder: testnet paper → testnet live (real order placement, fake funds) → prod live approve-first, tiny size → autonomous**. Include the `journalctl -u tradebot -f` log tip and the kill-switch (`/kill`) note.

- [ ] **Step 6: Verify + commit** — `make build-arm64` (cross-compiles) and `go test ./...` (green), then:
```bash
git add Makefile deploy .env.example docs/DEPLOY.md
git commit -m "feat: add Oracle ARM deployment artifacts and runbook"
```

---

## Self-Review

**Spec coverage (Phase 7):** signed REST client (HMAC known-vector, account, market orders) → T1 ✓; real exchange filters + live executor implementing the shared `Executor` → T2 ✓; `/healthz` → T3 ✓; `mode: live` wiring (real balance, real filters, signed executor, approve-first default, reuses the whole pipeline; testnet-capable rehearsal) → T4 ✓; ARM cross-compile + systemd (hardened) + Dockerfile + sample config/.env + Oracle runbook with the staged go-live ladder → T5 ✓. Completes the §11 deployment + the live half of §4.3 modes.

**Placeholder scan:** none — complete code/tests/artifacts.

**Type consistency:** `binance.Sign`/`Client`/`Balance`/`OrderFill`/`SymbolFilters`, `execution.LiveExecutor`/`NewLive` (implements the existing `Executor`), and the `/healthz` route are consistent; live mode reuses `risk.Filters`, `engine.NewLive`, `control`, `telegram`, `dashboard`, `store` unchanged. The signed client is verified against Binance's documented signature vector.
