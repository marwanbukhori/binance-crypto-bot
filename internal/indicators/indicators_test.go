package indicators

import (
	"math"
	"testing"
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
