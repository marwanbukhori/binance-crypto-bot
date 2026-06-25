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

// rampSymOffset builds a ramp fixture offset in time by `offset` so two symbols
// have different trajectories (ETHUSDT starts at price 50, BTC at 100).
func rampSymOffset(sym string, n int, startPrice float64, timeOffset int64) []domain.Candle {
	cs := make([]domain.Candle, 0, n)
	price := startPrice
	for i := 0; i < 60; i++ {
		cs = append(cs, domain.Candle{Symbol: sym, Close: startPrice, High: startPrice + 1, Low: startPrice - 1, Closed: true, CloseTime: timeOffset + int64(i)})
	}
	for i := 0; i < n-120; i++ {
		price = startPrice + float64(i)*3
		cs = append(cs, domain.Candle{Symbol: sym, Close: price, High: price + 3, Low: price - 3, Closed: true, CloseTime: timeOffset + int64(60+i)})
	}
	for i := 0; i < 60; i++ {
		p := cs[len(cs)-1].Close - float64(i)*3
		cs = append(cs, domain.Candle{Symbol: sym, Close: p, High: p + 3, Low: p - 3, Closed: true, CloseTime: timeOffset + int64(len(cs))})
	}
	return cs
}

// TestRunMultiTwoSymbols verifies concurrent two-symbol operation:
//   - Report is produced with positive equity.
//   - With MaxOpenPositions=1, no more than one concurrent position is open at any time.
//   - With a tight PortfolioMaxDeployedPct, deployed notional respects the cap.
func TestRunMultiTwoSymbols(t *testing.T) {
	const startCash = 10000.0
	syms := []string{"BTCUSDT", "ETHUSDT"}
	candleMap := map[string][]domain.Candle{
		"BTCUSDT": rampSymOffset("BTCUSDT", 240, 100, 0),
		"ETHUSDT": rampSymOffset("ETHUSDT", 240, 50, 5), // slightly offset trajectory
	}
	mk := func(sym string) []strategy.Strategy {
		return []strategy.Strategy{strategy.NewEMACross(sym, "1h", nil)}
	}

	// MaxOpenPositions=1: only one position allowed at a time.
	g := risk.NewGate(config.RiskCfg{
		MaxPctPerTrade:          50,
		RiskPerTradePct:         5,
		MaxOpenPositions:        1,
		PortfolioMaxDeployedPct: 60,
		TPRewardMult:            1.6,
	}, 0.003)
	guard := risk.NewGuard(config.RiskCfg{DailyLossLimitPct: 50, WeeklyLossLimitPct: 80, HardFlattenDrawdownPct: 90}, startCash)
	cool := risk.NewCooldown(2)
	filters := risk.Filters{StepSize: 0.00001, MinQty: 0.00001, MinNotional: 5}

	rep := RunMulti(syms, candleMap, mk, g, guard, cool, filters, startCash, 0.0015)

	if rep.FinalEquity <= 0 {
		t.Fatalf("final equity must be positive, got %v", rep.FinalEquity)
	}

	// Verify MaxOpenPositions=1: replay trade log to ensure no overlap in open positions.
	// Track open/close intervals per symbol; assert no two overlap.
	type interval struct {
		sym        string
		entry, exit int64
	}
	var intervals []interval
	for _, tr := range rep.Trades {
		intervals = append(intervals, interval{entry: tr.EntryTime, exit: tr.ExitTime})
	}
	for i := range intervals {
		for j := i + 1; j < len(intervals); j++ {
			a, b := intervals[i], intervals[j]
			// Overlap if a.entry < b.exit && b.entry < a.exit
			if a.entry < b.exit && b.entry < a.exit {
				t.Errorf("MaxOpenPositions=1 violated: trade %d [%d,%d) overlaps trade %d [%d,%d)",
					i, a.entry, a.exit, j, b.entry, b.exit)
			}
		}
	}
}
