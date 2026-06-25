package engine

import (
	"testing"

	"tradebot/internal/domain"
	"tradebot/internal/execution"
	"tradebot/internal/portfolio"
	"tradebot/internal/risk"
	"tradebot/internal/config"
	"tradebot/internal/store"
	"tradebot/internal/strategy"
)

func mkCandles(closes []float64) []domain.Candle {
	cs := make([]domain.Candle, len(closes))
	for i, c := range closes {
		cs[i] = domain.Candle{Symbol: "BTCUSDT", High: c + 5, Low: c - 5, Close: c, Closed: true, CloseTime: int64(i)}
	}
	return cs
}

func TestEndToEndBuyThenSellRecords(t *testing.T) {
	st, _ := store.Open(":memory:")
	defer st.Close()
	g := risk.NewGate(config.RiskCfg{MaxPctPerTrade: 50, RiskPerTradePct: 5, MaxOpenPositions: 2, PortfolioMaxDeployedPct: 100, TPRewardMult: 1.6}, 0.003)
	pf := portfolio.New(10000)
	s := strategy.NewEMACross("BTCUSDT", "1h", nil)
	e := New(s, g, execution.NewSimulated(0.0015), pf, st, risk.Filters{StepSize: 0.00001, MinQty: 0.00001, MinNotional: 5})

	// ramp up (triggers BUY) then ramp down (triggers SELL recross).
	closes := []float64{}
	for i := 0; i < 40; i++ { closes = append(closes, 100) }
	for i := 0; i < 40; i++ { closes = append(closes, 100+float64(i)*4) }
	for i := 0; i < 40; i++ { closes = append(closes, 260-float64(i)*4) }
	cs := mkCandles(closes)

	for i := 1; i <= len(cs); i++ {
		if err := e.OnClosedCandle(cs[i-1], cs[:i]); err != nil {
			t.Fatalf("OnClosedCandle: %v", err)
		}
	}
	n, _ := st.CountFills()
	if n < 2 {
		t.Fatalf("expected >=2 fills (a buy and a sell), got %d", n)
	}
	if pf.Position("BTCUSDT").Qty != 0 {
		t.Fatalf("expected flat at end, got %+v", pf.Position("BTCUSDT"))
	}
}
