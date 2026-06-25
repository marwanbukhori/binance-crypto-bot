package portfolio

import (
	"math"
	"testing"

	"tradebot/internal/domain"
)

func TestBuyThenSellRealizesPnL(t *testing.T) {
	p := New(1000)
	p.Apply(domain.Fill{Symbol: "BTCUSDT", Side: domain.Buy, Qty: 0.01, Price: 60000, Fee: 0.9})
	if math.Abs(p.Cash()-(1000-600-0.9)) > 1e-9 {
		t.Fatalf("cash after buy=%v", p.Cash())
	}
	if pos := p.Position("BTCUSDT"); math.Abs(pos.Qty-0.01) > 1e-12 || pos.AvgEntry != 60000 {
		t.Fatalf("position wrong: %+v", pos)
	}
	p.Apply(domain.Fill{Symbol: "BTCUSDT", Side: domain.Sell, Qty: 0.01, Price: 61000, Fee: 0.915})
	// realized = (61000-60000)*0.01 - buyFee - sellFee = 10 - 0.9 - 0.915 = 8.185
	if math.Abs(p.Realized()-8.185) > 1e-6 {
		t.Fatalf("realized=%v want 8.185", p.Realized())
	}
	if pos := p.Position("BTCUSDT"); pos.Qty != 0 {
		t.Fatalf("expected flat, got %+v", pos)
	}
}

func TestEquityMarksOpenPosition(t *testing.T) {
	p := New(1000)
	p.Apply(domain.Fill{Symbol: "BTCUSDT", Side: domain.Buy, Qty: 0.01, Price: 60000, Fee: 0})
	eq := p.Equity(map[string]float64{"BTCUSDT": 62000})
	// cash 400 + 0.01*62000 = 400 + 620 = 1020
	if math.Abs(eq-1020) > 1e-9 {
		t.Fatalf("equity=%v want 1020", eq)
	}
}
