package marketdata

import (
	"fmt"
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

// TestBackfillNon200ReturnsError asserts that a non-200 HTTP response from the
// klines endpoint causes Backfill to return an error (I4 fix).
func TestBackfillNon200ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"code":-1003,"msg":"Too many requests"}`)
	}))
	defer srv.Close()
	c := NewClient(false).WithHTTP(srv.Client(), srv.URL)
	_, err := c.Backfill("BTCUSDT", "1m", 0, 200000)
	if err == nil {
		t.Fatal("expected error on non-200 response, got nil")
	}
}

func TestBackfillPaginates(t *testing.T) {
	// Server returns 2 candles per page, advancing by startTime, then an empty page.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := r.URL.Query().Get("startTime")
		switch start {
		case "0":
			fmt.Fprint(w, `[[0,"1","2","0","1.5","1",59999,"x",1,"x","x","0"],[60000,"1.5","2.5","1","2","1",119999,"x",1,"x","x","0"]]`)
		case "120000":
			fmt.Fprint(w, `[[120000,"2","3","1.5","2.5","1",179999,"x",1,"x","x","0"]]`)
		default:
			fmt.Fprint(w, `[]`)
		}
	}))
	defer srv.Close()
	c := NewClient(false).WithHTTP(srv.Client(), srv.URL)
	cs, err := c.Backfill("BTCUSDT", "1m", 0, 200000)
	if err != nil {
		t.Fatalf("Backfill: %v", err)
	}
	if len(cs) != 3 {
		t.Fatalf("want 3 deduped candles, got %d", len(cs))
	}
	if cs[0].OpenTime != 0 || cs[2].OpenTime != 120000 {
		t.Fatalf("bad pagination order: %+v", cs)
	}
}
