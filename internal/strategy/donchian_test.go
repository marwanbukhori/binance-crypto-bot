package strategy

import (
	"testing"

	"tradebot/internal/domain"
)

func TestDonchianBreakoutBuysNewHigh(t *testing.T) {
	s := NewDonchianBreakout("BTCUSDT", "4h", nil)
	if s.Kind() != "breakout" { t.Fatalf("kind=%s", s.Kind()) }
	cs := make([]domain.Candle, 0, 60)
	// 40 candles ranging tightly, then a breakout candle with volatility expansion.
	for i := 0; i < 40; i++ {
		cs = append(cs, domain.Candle{Symbol: "BTCUSDT", Close: 100, High: 101, Low: 99, Closed: true, CloseTime: int64(i)})
	}
	// breakout: a big high-range candle closing above the prior 20-high (101).
	cs = append(cs, domain.Candle{Symbol: "BTCUSDT", Close: 115, High: 118, Low: 100, Closed: true, CloseTime: 40})
	var got *domain.Signal
	for i := s.Warmup(); i <= len(cs); i++ {
		if sig := s.Evaluate(cs[:i], false); sig != nil && sig.Action == domain.Buy {
			got = sig
		}
	}
	if got == nil { t.Fatal("expected a breakout BUY") }
	if got.StopDist <= 0 { t.Fatalf("BUY needs an ATR stop, got %v", got.StopDist) }
}

func TestDonchianExitsOnChannelLow(t *testing.T) {
	s := NewDonchianBreakout("BTCUSDT", "4h", nil)
	cs := make([]domain.Candle, 0, 60)
	for i := 0; i < 40; i++ {
		cs = append(cs, domain.Candle{Symbol: "BTCUSDT", Close: 100 + float64(i), High: 102 + float64(i), Low: 98 + float64(i), Closed: true, CloseTime: int64(i)})
	}
	// a candle closing below the prior-10 low should signal SELL when in position.
	cs = append(cs, domain.Candle{Symbol: "BTCUSDT", Close: 120, High: 121, Low: 119, Closed: true, CloseTime: 40})
	sig := s.Evaluate(cs, true)
	if sig == nil || sig.Action != domain.Sell {
		t.Fatalf("expected SELL on channel-low exit, got %+v", sig)
	}
}
