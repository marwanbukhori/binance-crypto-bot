package marketdata

import (
	"testing"
	"time"

	"tradebot/internal/domain"
)

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
