package strategy

import (
	"fmt"
	"math"
	"testing"

	"tradebot/internal/domain"
	"tradebot/internal/indicators"
)

func TestDebugCrossover(t *testing.T) {
	closes := make([]float64, 0, 80)
	for i := 0; i < 40; i++ {
		closes = append(closes, 100)
	}
	for i := 0; i < 40; i++ {
		closes = append(closes, 100+float64(i)*3)
	}
	cs := make([]domain.Candle, len(closes))
	for i, c := range closes {
		cs[i] = domain.Candle{High: c + 1, Low: c - 1, Close: c, Closed: true, CloseTime: int64(i)}
	}

	s := NewEMACross("BTCUSDT", "1h", nil)
	warmup := s.Warmup()
	fmt.Printf("Warmup: %d, total candles: %d\n", warmup, len(cs))

	for i := warmup; i <= len(cs); i++ {
		h := cs[:i]
		hlen := len(h)

		closesSlice := make([]float64, hlen)
		for j, c := range h {
			closesSlice[j] = c.Close
		}
		emaF := indicators.EMA(closesSlice, 9)
		emaS := indicators.EMA(closesSlice, 21)
		adx := indicators.ADX(h, 14)
		idx := hlen - 1

		if math.IsNaN(emaF[idx]) || math.IsNaN(emaF[idx-1]) || math.IsNaN(emaS[idx]) || math.IsNaN(emaS[idx-1]) || math.IsNaN(adx[idx]) {
			fmt.Printf("i=%d: NaN values present\n", i)
			continue
		}

		crossUp := emaF[idx-1] <= emaS[idx-1] && emaF[idx] > emaS[idx]
		if i <= warmup+3 || crossUp {
			fmt.Printf("i=%d: emaF[prev]=%.4f emaS[prev]=%.4f emaF[cur]=%.4f emaS[cur]=%.4f adx=%.4f crossUp=%v\n",
				i, emaF[idx-1], emaS[idx-1], emaF[idx], emaS[idx], adx[idx], crossUp)
		}
	}
}
