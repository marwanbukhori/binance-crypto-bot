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
		// I4: check HTTP status before decoding (mirrors Klines).
		if resp.StatusCode != 200 {
			resp.Body.Close()
			return nil, fmt.Errorf("backfill status %d", resp.StatusCode)
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
		next := lastClose + 1
		// I4: guard against non-advancing pagination to prevent infinite loops.
		if next <= cur {
			return nil, fmt.Errorf("backfill pagination stalled: lastClose %d did not advance cur %d", lastClose, cur)
		}
		cur = next
	}
	return out, nil
}
