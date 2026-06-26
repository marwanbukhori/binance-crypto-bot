package binance

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"tradebot/internal/risk"
)

func (c *Client) SymbolFilters(symbol string) (risk.Filters, error) {
	resp, err := c.http.Get(c.base + "/api/v3/exchangeInfo?" + url.Values{"symbol": {symbol}}.Encode())
	if err != nil {
		return risk.Filters{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
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
