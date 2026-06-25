package strategy

import (
	"fmt"
	"testing"

	"tradebot/internal/domain"
	"tradebot/internal/indicators"
)

func TestDebugCross(t *testing.T) {
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
	
	for n := 29; n <= len(cs); n++ {
		h := cs[:n]
		closesSlice := make([]float64, len(h))
		for i, c := range h {
			closesSlice[i] = c.Close
		}
		emaF := indicators.EMA(closesSlice, 9)
		emaS := indicators.EMA(closesSlice, 21)
		adx := indicators.ADX(h, 14)
		i := len(h) - 1
		
		crossUp := emaF[i-1] <= emaS[i-1] && emaF[i] > emaS[i]
		if crossUp || adx[i] >= 20 {
			fmt.Printf("n=%d close=%.0f emaF_prev=%.4f emaS_prev=%.4f emaF=%.4f emaS=%.4f adx=%.2f crossUp=%v\n",
				n, h[i].Close, emaF[i-1], emaS[i-1], emaF[i], emaS[i], adx[i], crossUp)
		}
	}
}
