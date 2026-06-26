package learning

import (
	"testing"

	"tradebot/internal/store"
)

func TestScoreByStrategyAggregates(t *testing.T) {
	trades := []store.Trade{ // newest-first, as ListTrades returns
		{Strategy: "ema_cross_trend", Regime: "TrendingUp", NetPnL: -2},
		{Strategy: "ema_cross_trend", Regime: "TrendingUp", NetPnL: 5},
		{Strategy: "ema_cross_trend", Regime: "Ranging", NetPnL: 1},
	}
	all := ScoreByStrategy(trades)
	if len(all) != 1 { t.Fatalf("want 1 strategy score, got %d", len(all)) }
	s := all[0]
	if s.Trades != 3 || s.Wins != 2 { t.Fatalf("counts: %+v", s) }
	if s.NetPnL < 3.99 || s.NetPnL > 4.01 { t.Fatalf("netPnL=%v want 4", s.NetPnL) }
}

func TestScoreByRegimeSplits(t *testing.T) {
	trades := []store.Trade{
		{Strategy: "ema_cross_trend", Regime: "TrendingUp", NetPnL: 5},
		{Strategy: "ema_cross_trend", Regime: "Ranging", NetPnL: -3},
	}
	sc := ScoreByRegime(trades)
	if len(sc) != 2 { t.Fatalf("want 2 regime groups, got %d", len(sc)) }
}
