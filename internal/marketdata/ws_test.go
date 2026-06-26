package marketdata

import (
	"testing"
	"time"

	"tradebot/internal/domain"
)

func TestParseKlineEventRealMessage(t *testing.T) {
	// A REAL Binance kline payload includes f/L (trade ids, numbers), n, q, V, Q, B.
	// Go's case-insensitive JSON matching maps "L" (number) onto the "l" (low, string)
	// field unless an exact-case field absorbs it — which previously errored on every
	// live message, so the bot emitted zero candles. This fixture guards that regression.
	msg := []byte(`{"e":"kline","E":1782463362019,"s":"BTCUSDT","k":{"t":1782463320000,"T":1782463379999,"s":"BTCUSDT","i":"1m","f":6449398985,"L":6449400214,"o":"60228.34000000","c":"60257.62000000","h":"60300.00000000","l":"60200.00000000","v":"3.75895000","n":1230,"x":true,"q":"226000.0","V":"1.5","Q":"90000.0","B":"0"}}`)
	c, closed, err := parseKlineEvent(msg)
	if err != nil {
		t.Fatalf("real kline message must parse without error, got %v", err)
	}
	if !closed {
		t.Fatal("x:true must be reported closed")
	}
	if c.Close != 60257.62 || c.Low != 60200 || c.High != 60300 || c.Volume != 3.75895 {
		t.Fatalf("OHLCV must come from price fields, not trade-id/taker collisions: %+v", c)
	}
}

func TestParseKlineEventClosed(t *testing.T) {
	msg := []byte(`{"e":"kline","E":123,"s":"BTCUSDT","k":{"t":100,"T":159999,"i":"1h","o":"60000","c":"60500","h":"60800","l":"59900","v":"12.3","x":true}}`)
	c, closed, err := parseKlineEvent(msg)
	if err != nil { t.Fatalf("parse: %v", err) }
	if !closed { t.Fatal("x:true must be a closed candle") }
	if c.Symbol != "BTCUSDT" || c.Close != 60500 || c.High != 60800 || c.Low != 59900 || c.Open != 60000 {
		t.Fatalf("bad candle: %+v", c)
	}
	if c.Timeframe != "1h" || c.CloseTime != 159999 || !c.Closed {
		t.Fatalf("bad meta: %+v", c)
	}
}

func TestParseKlineEventOpenIgnored(t *testing.T) {
	msg := []byte(`{"e":"kline","s":"BTCUSDT","k":{"t":100,"T":159999,"i":"1h","o":"1","c":"2","h":"3","l":"0","v":"1","x":false}}`)
	_, closed, err := parseKlineEvent(msg)
	if err != nil { t.Fatalf("parse: %v", err) }
	if closed { t.Fatal("x:false must NOT be reported closed") }
}

func TestParseNonKlineIgnored(t *testing.T) {
	_, closed, err := parseKlineEvent([]byte(`{"result":null,"id":1}`))
	if err != nil || closed { t.Fatalf("subscription ack must be ignored, got closed=%v err=%v", closed, err) }
}

func TestChanStreamDeliversCandles(t *testing.T) {
	ch := make(chan domain.Candle, 1)
	s := NewChanStream(ch)
	ch <- domain.Candle{Symbol: "BTCUSDT", Close: 1, Closed: true}
	select {
	case c := <-s.Candles():
		if c.Symbol != "BTCUSDT" { t.Fatalf("bad candle %+v", c) }
	case <-time.After(time.Second):
		t.Fatal("expected a candle")
	}
	if err := s.Close(); err != nil { t.Fatalf("close: %v", err) }
}

func TestWSStreamURL(t *testing.T) {
	if got := streamURL(true, []string{"BTCUSDT", "ETHUSDT"}, "1h"); got != "wss://testnet.binance.vision/stream?streams=btcusdt@kline_1h/ethusdt@kline_1h" {
		t.Fatalf("bad testnet url: %s", got)
	}
}
