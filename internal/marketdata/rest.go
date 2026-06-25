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
