package learning

import (
	"math/rand"
	"sort"

	"tradebot/internal/store"
)

type Projection struct {
	Steps                                int
	P5, P50, P95                         []float64
	TermP5, TermP50, TermP95, RiskOfRuinPct float64
}

func TradeReturns(trades []store.Trade) []float64 {
	var out []float64
	for _, t := range trades {
		notional := t.EntryPx * t.Qty
		if notional == 0 {
			continue
		}
		out = append(out, t.NetPnL/notional)
	}
	return out
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(p / 100 * float64(len(sorted)-1))
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func MonteCarlo(returns []float64, startEquity float64, steps, sims int, ruinDrawdownPct float64, rng *rand.Rand) Projection {
	if len(returns) == 0 || steps <= 0 || sims <= 0 {
		return Projection{}
	}
	// equityAtStep[step] = slice of `sims` equities at that step
	equityAtStep := make([][]float64, steps)
	for i := range equityAtStep {
		equityAtStep[i] = make([]float64, sims)
	}
	ruinFloor := startEquity * (1 - ruinDrawdownPct/100)
	ruined := 0
	for s := 0; s < sims; s++ {
		eq := startEquity
		hitRuin := false
		for step := 0; step < steps; step++ {
			eq *= 1 + returns[rng.Intn(len(returns))]
			if eq <= ruinFloor {
				hitRuin = true
			}
			equityAtStep[step][s] = eq
		}
		if hitRuin {
			ruined++
		}
	}
	p := Projection{Steps: steps, P5: make([]float64, steps), P50: make([]float64, steps), P95: make([]float64, steps)}
	for step := 0; step < steps; step++ {
		col := equityAtStep[step]
		sort.Float64s(col)
		p.P5[step] = percentile(col, 5)
		p.P50[step] = percentile(col, 50)
		p.P95[step] = percentile(col, 95)
	}
	p.TermP5, p.TermP50, p.TermP95 = p.P5[steps-1], p.P50[steps-1], p.P95[steps-1]
	p.RiskOfRuinPct = 100 * float64(ruined) / float64(sims)
	return p
}
