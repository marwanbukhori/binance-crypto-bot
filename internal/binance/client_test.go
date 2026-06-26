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
