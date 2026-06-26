package learning

import (
	"sort"

	"tradebot/internal/store"
)

type Score struct {
	Strategy, Regime                    string
	Trades, Wins                        int
	WinRate, NetPnL, Expectancy, MaxDrawdown float64
}

// reversed returns trades oldest->newest (ListTrades gives newest-first).
func reversed(ts []store.Trade) []store.Trade {
	out := make([]store.Trade, len(ts))
	for i := range ts {
		out[len(ts)-1-i] = ts[i]
	}
	return out
}

func score(key func(store.Trade) string, label func(store.Trade) (string, string), trades []store.Trade) []Score {
	groups := map[string][]store.Trade{}
	var order []string
	for _, t := range trades {
		k := key(t)
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], t)
	}
	var out []Score
	for _, k := range order {
		g := reversed(groups[k])
		var s Score
		s.Strategy, s.Regime = label(groups[k][0])
		var cum, peak, maxDD float64
		for _, t := range g {
			s.Trades++
			s.NetPnL += t.NetPnL
			if t.NetPnL > 0 {
				s.Wins++
			}
			cum += t.NetPnL
			if cum > peak {
				peak = cum
			}
			if peak-cum > maxDD {
				maxDD = peak - cum
			}
		}
		s.MaxDrawdown = maxDD
		if s.Trades > 0 {
			s.WinRate = float64(s.Wins) / float64(s.Trades)
			s.Expectancy = s.NetPnL / float64(s.Trades)
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Strategy != out[j].Strategy {
			return out[i].Strategy < out[j].Strategy
		}
		return out[i].Regime < out[j].Regime
	})
	return out
}

func ScoreByStrategy(trades []store.Trade) []Score {
	return score(func(t store.Trade) string { return t.Strategy },
		func(t store.Trade) (string, string) { return t.Strategy, "ALL" }, trades)
}

func ScoreByRegime(trades []store.Trade) []Score {
	return score(func(t store.Trade) string { return t.Strategy + "|" + t.Regime },
		func(t store.Trade) (string, string) { return t.Strategy, t.Regime }, trades)
}
