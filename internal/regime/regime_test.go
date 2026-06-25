package regime

import (
	"testing"

	"tradebot/internal/domain"
)

func mk(closes []float64) []domain.Candle {
	cs := make([]domain.Candle, len(closes))
	for i, c := range closes {
		cs[i] = domain.Candle{Close: c, High: c + 1, Low: c - 1, Closed: true, CloseTime: int64(i)}
	}
	return cs
}

func TestClassifyTrendingUp(t *testing.T) {
	closes := make([]float64, 160)
	for i := range closes { closes[i] = 100 + float64(i)*2 } // strong steady uptrend
	if r := Classify(mk(closes)); r != TrendingUp {
		t.Fatalf("want TrendingUp, got %s", r)
	}
}

func TestClassifyLowVolChop(t *testing.T) {
	closes := make([]float64, 160)
	for i := range closes {
		// tiny oscillation, no trend -> low ADX, narrow bands
		if i%2 == 0 { closes[i] = 100.05 } else { closes[i] = 99.95 }
	}
	if r := Classify(mk(closes)); r != LowVolChop {
		t.Fatalf("want LowVolChop, got %s", r)
	}
}

func TestClassifyUnknownWhenShort(t *testing.T) {
	if r := Classify(mk([]float64{1, 2, 3})); r != Unknown {
		t.Fatalf("want Unknown, got %s", r)
	}
}
