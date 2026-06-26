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
