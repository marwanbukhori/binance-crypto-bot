package backtest

import (
	"testing"

	"tradebot/internal/config"
	"tradebot/internal/risk"
	"tradebot/internal/strategy"
)

var defaultRiskCfg = config.RiskCfg{
	MaxPctPerTrade: 90, RiskPerTradePct: 5, MaxOpenPositions: 1,
	PortfolioMaxDeployedPct: 100, TPRewardMult: 1.6,
}

func TestSweepRunsAllFeeLevels(t *testing.T) {
	s := strategy.NewEMACross("BTCUSDT", "1h", nil)
	rows := Sweep(s, defaultRiskCfg, risk.Filters{StepSize: 0.00001, MinQty: 0.00001, MinNotional: 5}, ramp(), 10000, []float64{0.20, 0.30, 0.45})
	if len(rows) != 3 {
		t.Fatalf("want 3 sweep rows, got %d", len(rows))
	}
	for _, r := range rows {
		if r.FeeRoundTripPct <= 0 {
			t.Fatalf("fee not recorded: %+v", r)
		}
	}
}

// TestSweepHighFeeGateRejectsAllTrades asserts that when the round-trip fee is
// high enough to push net R:R below 1.3 for every signal, the gate rejects all
// entries and the sweep row produces zero completed trades.
//
// The ramp() candles have ATR ≈ 5; with tp_reward_mult=1.6 the signal's
// TPDist ≈ 8 and StopDist ≈ 10.  A round-trip fee of 100% (fee fraction = 1.0)
// makes feeAbs = 1.0 × price >> TPDist, collapsing netRR well below 1.3.
func TestSweepHighFeeGateRejectsAllTrades(t *testing.T) {
	s := strategy.NewEMACross("BTCUSDT", "1h", nil)
	// Use an extreme fee (100% round-trip) so the R:R gate always rejects.
	rows := Sweep(s, defaultRiskCfg, risk.Filters{StepSize: 0.00001, MinQty: 0.00001, MinNotional: 5}, ramp(), 10000, []float64{100.0})
	if len(rows) != 1 {
		t.Fatalf("want 1 sweep row, got %d", len(rows))
	}
	r := rows[0]
	// With the gate rejecting every buy, there must be zero completed trades.
	// Run the underlying report to check NumTrades directly.
	g := risk.NewGate(defaultRiskCfg, 100.0/100)
	rep := Run(s, g, risk.Filters{StepSize: 0.00001, MinQty: 0.00001, MinNotional: 5}, ramp(), 10000, 100.0/2/100)
	if rep.NumTrades != 0 {
		t.Fatalf("expected 0 trades when fee pushes R:R below gate, got %d", rep.NumTrades)
	}
	// The sweep row should report zero or negative net PnL with no profitable trades.
	if r.Profitable {
		t.Fatalf("sweep row at 100%% fee should not be profitable")
	}
}
