package learning

import (
	"math/rand"
	"testing"

	"tradebot/internal/store"
)

func TestTradeReturns(t *testing.T) {
	r := TradeReturns([]store.Trade{{EntryPx: 100, Qty: 2, NetPnL: 4}, {EntryPx: 0, Qty: 1, NetPnL: 1}})
	if len(r) != 1 { t.Fatalf("zero-notional trade must be skipped, got %d", len(r)) }
	if r[0] < 0.0199 || r[0] > 0.0201 { t.Fatalf("return=%v want 0.02", r[0]) }
}

func TestMonteCarloProducesOrderedCone(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	// mildly positive edge: +2% and -1% returns
	p := MonteCarlo([]float64{0.02, 0.02, -0.01}, 1000, 20, 500, 20, rng)
	if p.Steps != 20 || len(p.P50) != 20 { t.Fatalf("bad shape: %+v", p.Steps) }
	if !(p.TermP5 <= p.TermP50 && p.TermP50 <= p.TermP95) {
		t.Fatalf("percentiles must be ordered: %v %v %v", p.TermP5, p.TermP50, p.TermP95)
	}
	if p.RiskOfRuinPct < 0 || p.RiskOfRuinPct > 100 {
		t.Fatalf("risk of ruin out of range: %v", p.RiskOfRuinPct)
	}
}

func TestMonteCarloEmpty(t *testing.T) {
	p := MonteCarlo(nil, 1000, 10, 10, 20, rand.New(rand.NewSource(1)))
	if p.Steps != 0 || p.P50 != nil { t.Fatalf("empty returns -> zero projection, got %+v", p) }
}
