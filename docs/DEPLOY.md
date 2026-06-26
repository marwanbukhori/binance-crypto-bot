# Oracle Always-Free ARM Deployment Runbook

This guide walks through deploying the tradebot on an Oracle Always-Free Ampere (ARM64) VM running Ubuntu.

---

## 1. Provision the Oracle VM

1. Log into [Oracle Cloud Free Tier](https://cloud.oracle.com/).
2. Create a new VM instance:
   - **Shape:** VM.Standard.A1.Flex (Ampere ARM64) — up to 4 OCPUs and 24 GB RAM on Always Free.
   - **Image:** Ubuntu 22.04 (Minimal).
   - **Networking:** Allow inbound TCP on port 8080 (dashboard) via the security list / NSG.
3. Add your SSH public key. Boot the VM and note its public IP.

---

## 2. Build the ARM64 binary

On your **local machine** (cross-compile):

```bash
make build-arm64
# produces: bin/bot-arm64
```

Then copy it to the VM:

```bash
scp bin/bot-arm64 ubuntu@<VM_IP>:/tmp/bot
```

Alternatively, build directly **on the VM** (Go must be installed):

```bash
git clone <repo> && cd <repo>
go build -o bin/bot ./cmd/bot
```

Or use the **Dockerfile** (build on any machine with Docker and push to a registry, or build on the VM):

```bash
docker build -f deploy/Dockerfile -t tradebot:latest .
```

---

## 3. Create the tradebot user and directory

SSH into the VM, then:

```bash
sudo useradd --system --no-create-home --shell /usr/sbin/nologin tradebot
sudo mkdir -p /opt/tradebot
sudo chown tradebot:tradebot /opt/tradebot
sudo chmod 750 /opt/tradebot
```

---

## 4. Place the binary, config, and .env

```bash
# binary
sudo cp /tmp/bot /opt/tradebot/bot
sudo chmod +x /opt/tradebot/bot
sudo chown tradebot:tradebot /opt/tradebot/bot

# config — start from the sample
sudo cp deploy/config.sample.yaml /opt/tradebot/config.yaml
sudo chown tradebot:tradebot /opt/tradebot/config.yaml

# env secrets (mode 600 so only root/tradebot can read)
sudo cp .env.example /opt/tradebot/.env
sudo chmod 600 /opt/tradebot/.env
sudo chown tradebot:tradebot /opt/tradebot/.env
```

Edit `/opt/tradebot/.env` and fill in the real values (see step 5 below).

Edit `/opt/tradebot/config.yaml` and set `mode: paper` initially.

---

## 5. Create the Binance API key

1. Log into Binance → **Account → API Management**.
2. Create a new key labelled `tradebot-vm`.
3. Permissions: **Enable Reading** and **Enable Spot & Margin Trading** ONLY.
   - **DO NOT enable withdrawals** under any circumstance.
4. **IP restriction:** add only the Oracle VM's static public IP. Reject all others.
5. Copy the API Key and Secret into `/opt/tradebot/.env`:

```env
BINANCE_API_KEY=<your key>
BINANCE_API_SECRET=<your secret>
```

---

## 6. Install and enable the systemd service

```bash
sudo cp deploy/tradebot.service /etc/systemd/system/tradebot.service
sudo systemctl daemon-reload
sudo systemctl enable tradebot
sudo systemctl start tradebot
```

Check status:

```bash
sudo systemctl status tradebot
```

---

## 7. Verify with the health endpoint

```bash
curl http://localhost:8080/healthz
# expected: {"status":"ok",...}
```

If running from outside the VM, replace `localhost` with the VM's public IP (ensure port 8080 is open in the Oracle security list).

---

## 8. View live logs

```bash
journalctl -u tradebot -f
```

Use `Ctrl+C` to stop following. To view only errors:

```bash
journalctl -u tradebot -p err
```

---

## 9. Back up the database (cron)

The bot stores state in `tradebot.db` (SQLite). Add a daily backup cron:

```bash
sudo crontab -u tradebot -e
```

Add:

```cron
0 3 * * * cp /opt/tradebot/tradebot.db /opt/tradebot/backups/tradebot-$(date +\%Y\%m\%d).db
```

Create the backups directory first:

```bash
sudo mkdir -p /opt/tradebot/backups
sudo chown tradebot:tradebot /opt/tradebot/backups
```

---

## 10. Safe go-live ladder

Follow this staged ladder. Never skip a stage.

### Stage 1: Testnet paper (no real orders, no real funds)

```yaml
mode: paper
exchange: { testnet: true }
control: { autonomous: false }
```

Run for at least 1 week. Watch for strategy logic, risk guard, and Telegram notification correctness.

### Stage 2: Testnet live (real order placement, fake testnet funds)

```yaml
mode: live
exchange: { testnet: true }
control: { autonomous: false }
```

Use your **testnet API key** (from [testnet.binance.vision](https://testnet.binance.vision/)). This places real REST calls against Binance testnet — verifies HMAC signing, order parsing, and fill handling without any financial risk.

Approve each signal manually via Telegram (`/approve`) for at least 50 signals.

### Stage 3: Production live — approve-first, tiny size

```yaml
mode: live
exchange: { testnet: false }
control: { autonomous: false }
risk:
  max_pct_per_trade: 1   # start tiny
  max_open_positions: 1
```

Use your **production API key** (IP-restricted, no withdrawal). Every signal requires Telegram approval. Monitor for at least 2 weeks and validate fills, PnL accounting, and daily loss guards.

### Stage 4: Autonomous (only once trusted)

```yaml
control: { autonomous: true }
```

Flip only after Stage 3 runs without issues. Keep the kill-switch (`/kill` via Telegram) always available — it immediately flattens all positions and halts the bot.

---

## Before enabling autonomous live trading

The following robustness features are **not yet implemented**. Run in approve-first mode (`autonomous: false`) until they land:

- **Order reconciliation on startup/reconnect** — on restart the bot does not query the exchange for open orders or current balances. A crash mid-order could leave an open position that is invisible to the resumed process. Before flipping `autonomous: true`, add startup reconciliation: fetch open orders and account balances from Binance on every `runLive` start and reconcile them with the local portfolio state.

- **429/418 rate-limit backoff** — Binance responds with HTTP 429 (rate limit) or 418 (IP ban) when too many requests are sent. The current client surfaces these as plain errors; there is no exponential backoff or request-rate tracking. Under high candle frequency or many symbols, this can cause cascading failures. Add a retry layer that honours `Retry-After` headers before autonomous operation.

- **clientOrderId-based retry for timed-out orders** — a MARKET order whose HTTP call times out after Binance accepted it cannot be safely retried without risk of doubling the position. The `newClientOrderId` field is now set on every order (for traceability), but the retry logic that would use it to detect and deduplicate a previously-accepted order is not yet in place. Implement idempotent retry using `GET /api/v3/order?origClientOrderId=<id>` before issuing a duplicate.

Until these are in place: keep `control: { autonomous: false }` and approve every signal via Telegram.

---

## Kill-switch

Send `/kill` to the Telegram bot at any time to:
1. Cancel all open orders.
2. Flatten all open positions (market sell).
3. Stop accepting new signals.

To restart after a kill: `sudo systemctl restart tradebot`.

---

## Troubleshooting

| Symptom | Check |
|---|---|
| Service won't start | `journalctl -u tradebot -n 50` — look for missing env vars or config parse errors |
| Orders rejected | Verify API key permissions and IP restriction match the VM IP |
| `/healthz` not reachable | Check Oracle NSG + iptables allow port 8080 |
| High memory use | Check `max_open_positions` and candle buffer size in config |
| Telegram commands not received | Verify `TELEGRAM_BOT_TOKEN` and `TELEGRAM_CHAT_ID` in `.env` |
