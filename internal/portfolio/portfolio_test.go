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

// TestPartialSellExpensesBuyFeeProportionally verifies I2: selling half the position
// expenses only half the accumulated buy fee, not the entire fee.
func TestPartialSellExpensesBuyFeeProportionally(t *testing.T) {
	p := New(10000)
	// Buy 0.02 BTC at 60000, fee = 1.80 (full buy fee)
	p.Apply(domain.Fill{Symbol: "BTCUSDT", Side: domain.Buy, Qty: 0.02, Price: 60000, Fee: 1.80})

	// Sell half (0.01) at 61000, sell fee = 0.61
	p.Apply(domain.Fill{Symbol: "BTCUSDT", Side: domain.Sell, Qty: 0.01, Price: 61000, Fee: 0.61})
	// Expected realized: (61000-60000)*0.01 - (1.80*0.01/0.02) - 0.61
	//                   = 10 - 0.90 - 0.61 = 8.49
	wantRealized := (61000.0-60000.0)*0.01 - (1.80 * 0.01 / 0.02) - 0.61
	if math.Abs(p.Realized()-wantRealized) > 1e-9 {
		t.Fatalf("partial realized=%v want %.6f", p.Realized(), wantRealized)
	}
	// Remaining position should still hold 0.01 BTC with 0.90 accumulated buy fee
	pos := p.Position("BTCUSDT")
	if math.Abs(pos.Qty-0.01) > 1e-12 {
		t.Fatalf("remaining qty=%v want 0.01", pos.Qty)
	}

	// Sell the remaining 0.01 at 62000, sell fee = 0.62
	p.Apply(domain.Fill{Symbol: "BTCUSDT", Side: domain.Sell, Qty: 0.01, Price: 62000, Fee: 0.62})
	// Expected second realized: (62000-60000)*0.01 - 0.90 - 0.62 = 20 - 0.90 - 0.62 = 18.48
	wantTotal := wantRealized + (62000.0-60000.0)*0.01 - (1.80*0.01/0.02) - 0.62
	if math.Abs(p.Realized()-wantTotal) > 1e-9 {
		t.Fatalf("total realized after full exit=%v want %.6f", p.Realized(), wantTotal)
	}
	// Must be flat
	if pos2 := p.Position("BTCUSDT"); pos2.Qty > 1e-12 {
		t.Fatalf("expected flat after second sell, got qty=%v", pos2.Qty)
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
