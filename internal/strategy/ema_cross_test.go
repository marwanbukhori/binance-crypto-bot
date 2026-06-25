package strategy

import (
	"testing"

	"tradebot/internal/domain"
)

func mkCandles(closes []float64) []domain.Candle {
	cs := make([]domain.Candle, len(closes))
	for i, c := range closes {
		cs[i] = domain.Candle{High: c + 1, Low: c - 1, Close: c, Closed: true, CloseTime: int64(i)}
	}
	return cs
}

func TestEMACrossNoSignalBeforeWarmup(t *testing.T) {
	s := NewEMACross("BTCUSDT", "1h", nil)
	short := mkCandles([]float64{1, 2, 3})
	if sig := s.Evaluate(short, false); sig != nil {
		t.Fatalf("expected nil before warmup, got %+v", sig)
	}
}

func TestEMACrossBuysOnUptrendCross(t *testing.T) {
	s := NewEMACross("BTCUSDT", "1h", nil)
	// Long flat then a strong sustained ramp -> EMA9 crosses above EMA21 with ADX>=25.
	closes := make([]float64, 0, 80)
	for i := 0; i < 40; i++ {
		closes = append(closes, 100)
	}
	for i := 0; i < 40; i++ {
		closes = append(closes, 100+float64(i)*3)
	}
	cs := mkCandles(closes)
	var got *domain.Signal
	for i := s.Warmup(); i <= len(cs); i++ {
		if sig := s.Evaluate(cs[:i], false); sig != nil && sig.Action == domain.Buy {
			got = sig
			break
		}
	}
	if got == nil {
		t.Fatal("expected a BUY signal during the uptrend")
	}
	if got.StopDist <= 0 {
		t.Fatalf("BUY must carry an ATR stop distance, got %v", got.StopDist)
	}
}
