package execution

import (
	"math"
	"testing"

	"tradebot/internal/domain"
)

func TestSimulatedFillsAtCloseWithFee(t *testing.T) {
	ex := NewSimulated(0.0015)
	o := domain.Order{Symbol: "BTCUSDT", Side: domain.Buy, Qty: 0.001, Price: 60000}
	c := domain.Candle{Close: 60100}
	fill, err := ex.Execute(o, c)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if fill.Price != 60100 {
		t.Fatalf("fill price=%v want candle close 60100", fill.Price)
	}
	wantFee := 0.0015 * 0.001 * 60100
	if math.Abs(fill.Fee-wantFee) > 1e-9 {
		t.Fatalf("fee=%v want %v", fill.Fee, wantFee)
	}
}
