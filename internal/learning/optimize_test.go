package learning

import (
	"testing"

	"tradebot/internal/config"
	"tradebot/internal/domain"
	"tradebot/internal/risk"
	"tradebot/internal/strategy"
)

func synth(n int) []domain.Candle {
	cs := make([]domain.Candle, 0, n)
	price := 100.0
	for i := 0; i < n; i++ {
		// alternating trend segments so different adx_min values differ
		if (i/50)%2 == 0 { price += 1.5 } else { price -= 0.3 }
		cs = append(cs, domain.Candle{Symbol: "BTCUSDT", Close: price, High: price + 3, Low: price - 3, Closed: true, CloseTime: int64(i) * 3600_000})
	}
	return cs
}

func TestOptimizePicksAndReportsOOS(t *testing.T) {
	g := risk.NewGate(config.RiskCfg{MaxPctPerTrade: 90, RiskPerTradePct: 5, MaxOpenPositions: 1, PortfolioMaxDeployedPct: 100, TPRewardMult: 1.6}, 0.003)
	mk := func(p map[string]float64) strategy.Strategy { return strategy.NewEMACross("BTCUSDT", "1h", p) }
	grid := map[string][]float64{"adx_min": {20, 25, 30}, "tp_reward_mult": {1.6, 2.0}}
	res := Optimize(synth(400), mk, grid, g, risk.Filters{StepSize: 0.00001, MinQty: 0.00001, MinNotional: 5}, 10000, 0.0015, 0.7)
	if res.BestParams == nil { t.Fatal("expected best params") }
	if _, ok := res.BestParams["adx_min"]; !ok { t.Fatalf("best params missing adx_min: %+v", res.BestParams) }
}
