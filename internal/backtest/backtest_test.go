package backtest

import (
	"testing"

	"tradebot/internal/config"
	"tradebot/internal/domain"
	"tradebot/internal/risk"
	"tradebot/internal/strategy"
)

func ramp() []domain.Candle {
	closes := []float64{}
	for i := 0; i < 40; i++ { closes = append(closes, 100) }
	for i := 0; i < 60; i++ { closes = append(closes, 100+float64(i)*4) }
	for i := 0; i < 40; i++ { closes = append(closes, 340-float64(i)*4) }
	cs := make([]domain.Candle, len(closes))
	for i, c := range closes {
		cs[i] = domain.Candle{Symbol: "BTCUSDT", High: c + 5, Low: c - 5, Close: c, Closed: true, CloseTime: int64(i)}
	}
	return cs
}

func TestRunProducesTradesAndEquity(t *testing.T) {
	g := risk.NewGate(config.RiskCfg{MaxPctPerTrade: 90, RiskPerTradePct: 5, MaxOpenPositions: 1, PortfolioMaxDeployedPct: 100, TPRewardMult: 1.6}, 0.003)
	s := strategy.NewEMACross("BTCUSDT", "1h", nil)
	rep := Run(s, g, risk.Filters{StepSize: 0.00001, MinQty: 0.00001, MinNotional: 5}, ramp(), 10000, 0.0015)
	if rep.NumTrades < 1 {
		t.Fatalf("expected >=1 completed trade, got %d", rep.NumTrades)
	}
	if rep.FinalEquity <= 0 {
		t.Fatalf("final equity must be positive, got %v", rep.FinalEquity)
	}
	if rep.WinRate < 0 || rep.WinRate > 1 {
		t.Fatalf("win rate out of range: %v", rep.WinRate)
	}
	if rep.MaxDrawdownPct < 0 {
		t.Fatalf("max drawdown must be >=0, got %v", rep.MaxDrawdownPct)
	}
}
