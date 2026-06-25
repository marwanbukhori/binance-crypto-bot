package strategy

import (
	"fmt"
	"math"
	"testing"

	"tradebot/internal/domain"
	"tradebot/internal/indicators"
)

func TestCheckADXValues(t *testing.T) {
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
	
	emaF := indicators.EMA(closes, 9)
	emaS := indicators.EMA(closes, 21)
	adx := indicators.ADX(cs, 14)
	
	for i := 28; i < len(cs); i++ {
		if math.IsNaN(emaF[i]) || math.IsNaN(emaF[i-1]) || math.IsNaN(emaS[i]) || math.IsNaN(emaS[i-1]) || math.IsNaN(adx[i]) {
			continue
		}
		crossUp := emaF[i-1] <= emaS[i-1] && emaF[i] > emaS[i]
		fmt.Printf("i=%d close=%.0f emaF=%.2f emaS=%.2f adx=%.2f crossUp=%v adx>=25=%v\n", i, closes[i], emaF[i], emaS[i], adx[i], crossUp, adx[i]>=25)
	}
}
