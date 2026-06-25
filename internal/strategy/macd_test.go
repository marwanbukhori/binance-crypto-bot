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

// buildMACDFixture returns a candle slice that first produces a Buy signal, then
// on the next candle produces a cross-down Sell, suitable for testing exit + debounce.
// It reuses the same warm-up + uptrend phases as the Buy test, then appends candles
// that force a decisive cross-down.
func buildMACDFixture() []domain.Candle {
	cs := make([]domain.Candle, 0, 300)
	// 120 flat warm-up candles.
	for i := 0; i < 120; i++ {
		cs = append(cs, domain.Candle{Symbol: "BTCUSDT", Close: 100, High: 101, Low: 99, Closed: true, CloseTime: int64(i)})
	}
	// Uptrend phase to generate cross-up + ADX.
	p := 100.0
	for i := 0; i < 60; i++ {
		if i%3 == 2 {
			p -= 0.1
		} else {
			p += 0.4
		}
		cs = append(cs, domain.Candle{Symbol: "BTCUSDT", Close: p, High: p + 0.3, Low: p - 0.3, Closed: true, CloseTime: int64(120 + i)})
	}
	// Brief pullback to reset MACD below signal.
	for i := 0; i < 15; i++ {
		p -= 0.25
		cs = append(cs, domain.Candle{Symbol: "BTCUSDT", Close: p, High: p + 0.2, Low: p - 0.2, Closed: true, CloseTime: int64(180 + i)})
	}
	// Fresh uptrend to generate Buy signal.
	for i := 0; i < 40; i++ {
		if i%4 == 3 {
			p -= 0.3
		} else {
			p += 0.5
		}
		cs = append(cs, domain.Candle{Symbol: "BTCUSDT", Close: p, High: p + 0.3, Low: p - 0.3, Closed: true, CloseTime: int64(195 + i)})
	}
	return cs
}

// TestMACDMomentumSellOnCrossDown verifies that when inPosition=true and MACD crosses
// back below signal, Evaluate returns a Sell signal.
func TestMACDMomentumSellOnCrossDown(t *testing.T) {
	s := NewMACDMomentum("BTCUSDT", "1h", nil)
	base := buildMACDFixture()

	// Find the Buy candle index first (must be in position to test Sell).
	buyIdx := -1
	for i := s.Warmup(); i <= len(base); i++ {
		if sig := s.Evaluate(base[:i], false); sig != nil && sig.Action == domain.Buy {
			buyIdx = i
			break
		}
	}
	if buyIdx < 0 {
		t.Fatal("fixture did not produce a Buy — cannot test Sell path")
	}

	// Now extend with a sharp downtrend to force cross-down while inPosition=true.
	cs := make([]domain.Candle, len(base))
	copy(cs, base)
	p := cs[len(cs)-1].Close
	for i := 0; i < 30; i++ {
		p -= 1.5
		ts := cs[len(cs)-1].CloseTime + 1
		cs = append(cs, domain.Candle{Symbol: "BTCUSDT", Close: p, High: p + 0.5, Low: p - 0.5, Closed: true, CloseTime: ts})
	}

	var gotSell *domain.Signal
	for i := buyIdx + 1; i <= len(cs); i++ {
		if sig := s.Evaluate(cs[:i], true); sig != nil && sig.Action == domain.Sell {
			gotSell = sig
			break
		}
	}
	if gotSell == nil {
		t.Fatal("expected a SELL on cross-down while inPosition=true, got none")
	}
}

// TestMACDMomentumDebounce verifies that immediately after an exit, the next candle
// does not produce a Buy even if conditions would otherwise allow it.
func TestMACDMomentumDebounce(t *testing.T) {
	s := NewMACDMomentum("BTCUSDT", "1h", nil)
	base := buildMACDFixture()

	// Walk until we find a Buy.
	buyIdx := -1
	for i := s.Warmup(); i <= len(base); i++ {
		if sig := s.Evaluate(base[:i], false); sig != nil && sig.Action == domain.Buy {
			buyIdx = i
			break
		}
	}
	if buyIdx < 0 {
		t.Fatal("fixture did not produce a Buy — cannot test debounce")
	}

	// Append a big drop to force a Sell on the very next candle.
	cs := make([]domain.Candle, len(base))
	copy(cs, base)
	p := cs[len(cs)-1].Close
	for i := 0; i < 30; i++ {
		p -= 1.5
		ts := cs[len(cs)-1].CloseTime + 1
		cs = append(cs, domain.Candle{Symbol: "BTCUSDT", Close: p, High: p + 0.5, Low: p - 0.5, Closed: true, CloseTime: ts})
	}

	// Find the Sell signal (sets lastExitTime).
	sellIdx := -1
	for i := buyIdx + 1; i <= len(cs); i++ {
		if sig := s.Evaluate(cs[:i], true); sig != nil && sig.Action == domain.Sell {
			sellIdx = i
			break
		}
	}
	if sellIdx < 0 {
		t.Fatal("no Sell found — cannot verify debounce")
	}

	// The candle immediately after the Sell must be suppressed (debounce).
	if sellIdx+1 <= len(cs) {
		sig := s.Evaluate(cs[:sellIdx+1], false)
		if sig != nil && sig.Action == domain.Buy {
			t.Fatal("debounce failed: Buy was returned on the candle immediately after exit")
		}
	}
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
