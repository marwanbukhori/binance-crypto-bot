package binance

import (
	"encoding/json"
	"fmt"
	"io"
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
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		return fmt.Errorf("binance %s: %d %s", req.URL.Path, resp.StatusCode, string(b))
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
		"symbol":           {symbol},
		"side":             {side},
		"type":             {"MARKET"},
		"quantity":         {strconv.FormatFloat(qty, 'f', -1, 64)},
		"newClientOrderId": {fmt.Sprintf("tb-%d", c.nowMs())},
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
