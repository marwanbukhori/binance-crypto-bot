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
