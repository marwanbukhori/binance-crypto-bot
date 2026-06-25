package backtest

import (
	"testing"

	"tradebot/internal/config"
	"tradebot/internal/risk"
	"tradebot/internal/strategy"
)

func TestSweepRunsAllFeeLevels(t *testing.T) {
	g := risk.NewGate(config.RiskCfg{MaxPctPerTrade: 90, RiskPerTradePct: 5, MaxOpenPositions: 1, PortfolioMaxDeployedPct: 100, TPRewardMult: 1.6}, 0.003)
	s := strategy.NewEMACross("BTCUSDT", "1h", nil)
	rows := Sweep(s, g, risk.Filters{StepSize: 0.00001, MinQty: 0.00001, MinNotional: 5}, ramp(), 10000, []float64{0.20, 0.30, 0.45})
	if len(rows) != 3 {
		t.Fatalf("want 3 sweep rows, got %d", len(rows))
	}
	for _, r := range rows {
		if r.FeeRoundTripPct <= 0 {
			t.Fatalf("fee not recorded: %+v", r)
		}
	}
}
