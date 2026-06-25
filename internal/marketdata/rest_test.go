package marketdata

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestKlinesParsesBinanceArray(t *testing.T) {
	body := `[
	  [1609459200000,"29000.0","29500.0","28900.0","29400.0","123.4",1609462799999,"x",10,"x","x","0"],
	  [1609462800000,"29400.0","29800.0","29300.0","29750.0","98.7", 1609466399999,"x",12,"x","x","0"]
	]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	}))
	defer srv.Close()

	c := NewClient(false).WithHTTP(srv.Client(), srv.URL)
	cs, err := c.Klines("BTCUSDT", "1h", 2)
	if err != nil {
		t.Fatalf("Klines: %v", err)
	}
	if len(cs) != 2 {
		t.Fatalf("want 2 candles, got %d", len(cs))
	}
	if cs[0].Open != 29000 || cs[0].High != 29500 || cs[0].Low != 28900 || cs[0].Close != 29400 {
		t.Fatalf("bad OHLC: %+v", cs[0])
	}
	if !cs[0].Closed || cs[0].Symbol != "BTCUSDT" {
		t.Fatalf("expected closed candle for BTCUSDT: %+v", cs[0])
	}
}
