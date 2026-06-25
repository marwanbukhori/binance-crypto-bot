package backtest

import (
	"testing"

	"tradebot/internal/config"
	"tradebot/internal/domain"
	"tradebot/internal/risk"
	"tradebot/internal/strategy"
)

func rampSym(sym string, n int) []domain.Candle {
	cs := make([]domain.Candle, 0, n)
	price := 100.0
	for i := 0; i < 60; i++ { cs = append(cs, domain.Candle{Symbol: sym, Close: 100, High: 101, Low: 99, Closed: true, CloseTime: int64(i)}) }
	for i := 0; i < n-120; i++ { price = 100 + float64(i)*3; cs = append(cs, domain.Candle{Symbol: sym, Close: price, High: price + 3, Low: price - 3, Closed: true, CloseTime: int64(60 + i)}) }
	for i := 0; i < 60; i++ { p := cs[len(cs)-1].Close - float64(i)*3; cs = append(cs, domain.Candle{Symbol: sym, Close: p, High: p + 3, Low: p - 3, Closed: true, CloseTime: int64(len(cs))}) }
	return cs
}

func TestRunMultiProducesReport(t *testing.T) {
	syms := []string{"BTCUSDT"}
	candles := map[string][]domain.Candle{"BTCUSDT": rampSym("BTCUSDT", 240)}
	mk := func(sym string) []strategy.Strategy {
		return []strategy.Strategy{strategy.NewEMACross(sym, "1h", nil)}
	}
	g := risk.NewGate(config.RiskCfg{MaxPctPerTrade: 90, RiskPerTradePct: 5, MaxOpenPositions: 2, PortfolioMaxDeployedPct: 100, TPRewardMult: 1.6}, 0.003)
	guard := risk.NewGuard(config.RiskCfg{DailyLossLimitPct: 50, WeeklyLossLimitPct: 80, HardFlattenDrawdownPct: 90}, 10000)
	cool := risk.NewCooldown(2)
	rep := RunMulti(syms, candles, mk, g, guard, cool, risk.Filters{StepSize: 0.00001, MinQty: 0.00001, MinNotional: 5}, 10000, 0.0015)
	if rep.FinalEquity <= 0 {
		t.Fatalf("final equity must be positive, got %v", rep.FinalEquity)
	}
}
