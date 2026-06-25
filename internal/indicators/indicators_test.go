package indicators

import (
	"math"
	"testing"
	"tradebot/internal/domain"
)

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func TestSMA(t *testing.T) {
	got := SMA([]float64{1, 2, 3, 4, 5}, 3)
	if !math.IsNaN(got[0]) || !math.IsNaN(got[1]) {
		t.Fatal("first period-1 values must be NaN")
	}
	for i, want := range map[int]float64{2: 2, 3: 3, 4: 4} {
		if !approx(got[i], want) {
			t.Errorf("SMA[%d]=%v want %v", i, got[i], want)
		}
	}
}

func TestEMASeedAndStep(t *testing.T) {
	// EMA(3) seeds at index 2 with SMA of first 3 = 2; multiplier = 2/(3+1)=0.5.
	// index 3: 0.5*4 + 0.5*2 = 3 ; index 4: 0.5*5 + 0.5*3 = 4
	got := EMA([]float64{1, 2, 3, 4, 5}, 3)
	if !approx(got[2], 2) || !approx(got[3], 3) || !approx(got[4], 4) {
		t.Fatalf("EMA seq wrong: %v", got)
	}
}

func candles(highs, lows, closes []float64) []domain.Candle {
	cs := make([]domain.Candle, len(closes))
	for i := range closes {
		cs[i] = domain.Candle{High: highs[i], Low: lows[i], Close: closes[i]}
	}
	return cs
}

func TestATRProducesPositiveAfterWarmup(t *testing.T) {
	h := []float64{10, 11, 12, 11, 13, 14, 13, 15}
	l := []float64{9, 9, 10, 9, 11, 12, 11, 13}
	c := []float64{9.5, 10.5, 11.5, 10, 12.5, 13.5, 12, 14.5}
	atr := ATR(candles(h, l, c), 3)
	if !math.IsNaN(atr[1]) {
		t.Fatal("ATR before warmup must be NaN")
	}
	if math.IsNaN(atr[len(atr)-1]) || atr[len(atr)-1] <= 0 {
		t.Fatalf("ATR after warmup must be positive, got %v", atr[len(atr)-1])
	}
}

func TestADXRangeAndTrendDetection(t *testing.T) {
	// Steady uptrend should yield a rising ADX in [0,100].
	n := 40
	h := make([]float64, n); l := make([]float64, n); c := make([]float64, n)
	for i := 0; i < n; i++ {
		base := 100.0 + float64(i) // strict uptrend
		h[i], l[i], c[i] = base+1, base-1, base+0.5
	}
	adx := ADX(candles(h, l, c), 14)
	last := adx[n-1]
	if math.IsNaN(last) || last < 0 || last > 100 {
		t.Fatalf("ADX out of range: %v", last)
	}
	if last < 20 {
		t.Fatalf("strong uptrend should give ADX>=20, got %v", last)
	}
}
