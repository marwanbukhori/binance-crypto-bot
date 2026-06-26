package engine

import (
	"context"
	"testing"
	"time"

	"tradebot/internal/config"
	"tradebot/internal/domain"
	"tradebot/internal/execution"
	"tradebot/internal/marketdata"
	"tradebot/internal/portfolio"
	"tradebot/internal/risk"
	"tradebot/internal/store"
	"tradebot/internal/strategy"
)

func TestLiveRunsPipelineAndRecords(t *testing.T) {
	st, _ := store.Open(":memory:")
	defer st.Close()
	buf := marketdata.NewBuffer(400)
	gate := risk.NewGate(config.RiskCfg{MaxPctPerTrade: 90, RiskPerTradePct: 5, MaxOpenPositions: 1, PortfolioMaxDeployedPct: 100, TPRewardMult: 1.6}, 0.003)
	guard := risk.NewGuard(config.RiskCfg{DailyLossLimitPct: 90, WeeklyLossLimitPct: 95, HardFlattenDrawdownPct: 99}, 10000)
	cool := risk.NewCooldown(2)
	pf := portfolio.New(10000)
	mk := func(sym string) []strategy.Strategy { return []strategy.Strategy{strategy.NewEMACross(sym, "1h", nil)} }
	l := NewLive([]string{"BTCUSDT"}, mk, gate, guard, cool, execution.NewSimulated(0.0015), pf, st, risk.Filters{StepSize: 0.00001, MinQty: 0.00001, MinNotional: 5}, buf)

	ch := make(chan domain.Candle, 256)
	closes := []float64{}
	for i := 0; i < 40; i++ { closes = append(closes, 100) }
	for i := 0; i < 50; i++ { closes = append(closes, 100+float64(i)*4) }
	for i := 0; i < 40; i++ { closes = append(closes, 300-float64(i)*4) }
	for i, c := range closes {
		ch <- domain.Candle{Symbol: "BTCUSDT", Close: c, High: c + 5, Low: c - 5, Closed: true, CloseTime: int64(i) * 3600_000}
	}
	close(ch)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = l.Run(ctx, marketdata.NewChanStream(ch))

	n, _ := st.CountFills()
	if n < 1 { t.Fatalf("expected the live engine to record at least one fill, got %d", n) }
}
