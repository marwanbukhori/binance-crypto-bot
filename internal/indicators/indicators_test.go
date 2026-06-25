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

func TestRSIAllGainsIs100(t *testing.T) {
	vals := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	r := RSI(vals, 14)
	if !math.IsNaN(r[13]) { t.Fatal("RSI before period must be NaN at index 13") }
	if r[15] < 99.9 { t.Fatalf("all-gains RSI must be ~100, got %v", r[15]) }
}

func TestRSIMidRange(t *testing.T) {
	vals := []float64{44, 44.34, 44.09, 44.15, 43.61, 44.33, 44.83, 45.10, 45.42, 45.84, 46.08, 45.89, 46.03, 45.61, 46.28, 46.28}
	r := RSI(vals, 14)
	if math.IsNaN(r[15]) || r[15] <= 0 || r[15] >= 100 {
		t.Fatalf("RSI out of range: %v", r[15])
	}
}

func TestMACDCrossSign(t *testing.T) {
	vals := make([]float64, 60)
	for i := range vals { vals[i] = 100 + float64(i) } // steady uptrend
	macd, sig, hist := MACD(vals, 12, 26, 9)
	last := len(vals) - 1
	if math.IsNaN(macd[last]) || math.IsNaN(sig[last]) || math.IsNaN(hist[last]) {
		t.Fatal("MACD should be defined at the end of a long series")
	}
	if macd[last] <= 0 {
		t.Fatalf("uptrend MACD line should be positive, got %v", macd[last])
	}
	if math.Abs((macd[last]-sig[last])-hist[last]) > 1e-9 {
		t.Fatal("hist must equal macd - sig")
	}
}

func TestBollingerBandsAroundMean(t *testing.T) {
	vals := []float64{10, 12, 11, 13, 12, 14, 13, 15, 14, 16}
	mid, up, lo := Bollinger(vals, 5, 2.0)
	i := len(vals) - 1
	if math.IsNaN(mid[i]) || math.IsNaN(up[i]) || math.IsNaN(lo[i]) {
		t.Fatal("bands undefined at end")
	}
	if !(lo[i] < mid[i] && mid[i] < up[i]) {
		t.Fatalf("expected lo<mid<up, got %v %v %v", lo[i], mid[i], up[i])
	}
	// symmetric around mid
	if math.Abs((up[i]-mid[i])-(mid[i]-lo[i])) > 1e-9 {
		t.Fatal("bands must be symmetric around mid")
	}
}
