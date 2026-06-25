package strategy

import (
	"testing"

	"tradebot/internal/domain"
)

func TestMACDMomentumIdentity(t *testing.T) {
	s := NewMACDMomentum("BTCUSDT", "1h", nil)
	if s.Name() != "macd_momentum" || s.Kind() != "momentum" {
		t.Fatalf("bad identity %s/%s", s.Name(), s.Kind())
	}
	if s.Warmup() < 100 { t.Fatalf("warmup must cover EMA100, got %d", s.Warmup()) }
}

func TestMACDMomentumBuysOnUptrendCross(t *testing.T) {
	s := NewMACDMomentum("BTCUSDT", "1h", nil)
	// Phase 1: 120 flat candles to warm up EMA100.
	// Phase 2: gentle zigzag uptrend (2 up + 1 small down) building ADX and EMA100.
	// Phase 3: shallow pullback to reset MACD below signal line.
	// Phase 4: new uptrend with controlled zigzag -> MACD crosses up above EMA100 with ADX>20 and RSI<=75.
	cs := make([]domain.Candle, 0, 250)
	for i := 0; i < 120; i++ {
		cs = append(cs, domain.Candle{Symbol: "BTCUSDT", Close: 100, High: 101, Low: 99, Closed: true, CloseTime: int64(i)})
	}
	p := 100.0
	for i := 0; i < 60; i++ {
		if i%3 == 2 {
			p -= 0.1
		} else {
			p += 0.4
		}
		cs = append(cs, domain.Candle{Symbol: "BTCUSDT", Close: p, High: p + 0.3, Low: p - 0.3, Closed: true, CloseTime: int64(120 + i)})
	}
	for i := 0; i < 15; i++ {
		p -= 0.25
		cs = append(cs, domain.Candle{Symbol: "BTCUSDT", Close: p, High: p + 0.2, Low: p - 0.2, Closed: true, CloseTime: int64(180 + i)})
	}
	for i := 0; i < 40; i++ {
		if i%4 == 3 {
			p -= 0.3
		} else {
			p += 0.5
		}
		cs = append(cs, domain.Candle{Symbol: "BTCUSDT", Close: p, High: p + 0.3, Low: p - 0.3, Closed: true, CloseTime: int64(195 + i)})
	}
	var got *domain.Signal
	for i := s.Warmup(); i <= len(cs); i++ {
		if sig := s.Evaluate(cs[:i], false); sig != nil && sig.Action == domain.Buy {
			got = sig
			break
		}
	}
	if got == nil { t.Fatal("expected a momentum BUY on the accelerating uptrend") }
	if got.StopDist <= 0 { t.Fatalf("BUY needs ATR stop, got %v", got.StopDist) }
}
