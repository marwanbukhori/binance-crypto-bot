package learning

import (
	"sort"

	"tradebot/internal/backtest"
	"tradebot/internal/domain"
	"tradebot/internal/risk"
	"tradebot/internal/strategy"
)

type OptResult struct {
	BestParams              map[string]float64
	TrainNetPnL, TestNetPnL float64
	TestTrades              int
}

// combos expands a grid into the cartesian product of param maps (sorted keys for determinism).
func combos(grid map[string][]float64) []map[string]float64 {
	keys := make([]string, 0, len(grid))
	for k := range grid {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := []map[string]float64{{}}
	for _, k := range keys {
		var next []map[string]float64
		for _, base := range out {
			for _, v := range grid[k] {
				m := map[string]float64{}
				for bk, bv := range base {
					m[bk] = bv
				}
				m[k] = v
				next = append(next, m)
			}
		}
		out = next
	}
	return out
}

func Optimize(candles []domain.Candle, mk func(map[string]float64) strategy.Strategy, grid map[string][]float64, g *risk.Gate, f risk.Filters, startCash, feeRate, trainFrac float64) OptResult {
	if len(candles) < 10 {
		return OptResult{}
	}
	split := int(float64(len(candles)) * trainFrac)
	train, test := candles[:split], candles[split:]
	var res OptResult
	best := -1e18
	for _, combo := range combos(grid) {
		rep := backtest.Run(mk(combo), g, f, train, startCash, feeRate)
		if rep.NetPnL > best {
			best = rep.NetPnL
			res.BestParams = combo
			res.TrainNetPnL = rep.NetPnL
		}
	}
	if res.BestParams != nil {
		testRep := backtest.Run(mk(res.BestParams), g, f, test, startCash, feeRate)
		res.TestNetPnL = testRep.NetPnL
		res.TestTrades = testRep.NumTrades
	}
	return res
}
