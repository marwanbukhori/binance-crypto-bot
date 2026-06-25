package backtest

import (
	"tradebot/internal/decision"
	"tradebot/internal/domain"
	"tradebot/internal/regime"
	"tradebot/internal/risk"
	"tradebot/internal/strategy"
)

type StratFactory func(symbol string) []strategy.Strategy

type posState struct {
	order    domain.Order
	entryIdx int
}

// RunMulti replays multiple symbols through strategies + regime + decision + guard + cooldown.
func RunMulti(symbols []string, candles map[string][]domain.Candle, mkStrats StratFactory,
	g *risk.Gate, guard *risk.Guard, cool *risk.Cooldown, f risk.Filters, startCash, feeRate float64) Report {

	cash := startCash
	strats := map[string][]strategy.Strategy{}
	open := map[string]*posState{}
	maxLen := 0
	for _, sym := range symbols {
		strats[sym] = mkStrats(sym)
		if len(candles[sym]) > maxLen {
			maxLen = len(candles[sym])
		}
	}
	var rep Report
	peak := startCash

	markEquity := func(idx int) float64 {
		eq := cash
		for sym, ps := range open {
			cs := candles[sym]
			if idx-1 < len(cs) {
				eq += ps.order.Qty * cs[idx-1].Close
			}
		}
		return eq
	}
	closeTrade := func(sym string, ps *posState, exitPx float64, ts int64, reason string) float64 {
		proceeds := ps.order.Qty*exitPx - feeRate*ps.order.Qty*exitPx
		entryCost := ps.order.Qty*ps.order.Price + feeRate*ps.order.Qty*ps.order.Price
		net := proceeds - entryCost
		cash += proceeds
		rep.Trades = append(rep.Trades, Trade{EntryTime: ps.order.Time, ExitTime: ts, EntryPx: ps.order.Price, ExitPx: exitPx, Qty: ps.order.Qty, NetPnL: net, ExitReason: reason})
		rep.NumTrades++
		rep.NetPnL += net
		if net > 0 {
			rep.Wins++
		}
		delete(open, sym)
		return net
	}

	for i := 1; i <= maxLen; i++ {
		guard.Mark(markEquity(i))
		for _, sym := range symbols {
			cs := candles[sym]
			if i > len(cs) {
				continue
			}
			c := cs[i-1]
			hist := cs[:i]

			// 1) Manage open position: intrabar TP/SL from the candle after entry.
			if ps, ok := open[sym]; ok {
				if guard.ShouldFlatten() {
					if net := closeTrade(sym, ps, c.Close, c.CloseTime, "flatten"); net < 0 {
						cool.NoteLoss(sym, i-1)
					}
				} else if i-1 > ps.entryIdx {
					if kind, px := CheckExit(ps.order, c); kind == ExitStop {
						if net := closeTrade(sym, ps, px, c.CloseTime, "SL"); net < 0 {
							cool.NoteLoss(sym, i-1)
						}
					} else if kind == ExitTP {
						closeTrade(sym, ps, px, c.CloseTime, "TP")
					}
				}
			}

			// 2) Strategy candidates + regime + decision.
			_, inPos := open[sym]
			var cands []decision.Candidate
			for _, st := range strats[sym] {
				if sig := st.Evaluate(hist, inPos); sig != nil {
					cands = append(cands, decision.Candidate{Kind: st.Kind(), Signal: *sig})
				}
			}
			reg := regime.Classify(hist)
			sig := decision.Choose(reg, inPos, cands)
			if sig == nil {
				continue
			}
			if sig.Action == domain.Sell {
				if ps, ok := open[sym]; ok {
					if net := closeTrade(sym, ps, c.Close, c.CloseTime, "rule"); net < 0 {
						cool.NoteLoss(sym, i-1)
					}
				}
				continue
			}
			// Buy: respect guard + cooldown.
			if !guard.AllowEntry() || cool.Blocked(sym, i-1) {
				continue
			}
			// I1: compute deployed notional from all currently open positions.
			var deployed float64
			for s, ps := range open {
				scs := candles[s]
				if i-1 < len(scs) {
					deployed += ps.order.Qty * scs[i-1].Close
				} else {
					deployed += ps.order.Qty * ps.order.Price
				}
			}
			acct := risk.Account{Equity: cash, FreeUSDT: cash - deployed, DeployedNotional: deployed, OpenPositions: len(open)}
			in := domain.Intent{Symbol: sym, Action: domain.Buy, Price: c.Close, StopDist: sig.StopDist, TPDist: sig.TPDist, Reason: sig.Reason, Time: c.CloseTime}
			if o, err := g.Evaluate(in, acct, f); err == nil {
				cash -= o.Qty*o.Price + feeRate*o.Qty*o.Price
				open[sym] = &posState{order: o, entryIdx: i - 1}
			}
		}

		eq := markEquity(i)
		if eq > peak {
			peak = eq
		}
		if peak > 0 {
			if dd := (peak - eq) / peak * 100; dd > rep.MaxDrawdownPct {
				rep.MaxDrawdownPct = dd
			}
		}
	}

	// Force-close any dangling positions at their last close.
	for sym, ps := range open {
		cs := candles[sym]
		closeTrade(sym, ps, cs[len(cs)-1].Close, cs[len(cs)-1].CloseTime, "eod")
	}
	rep.FinalEquity = cash
	if rep.NumTrades > 0 {
		rep.WinRate = float64(rep.Wins) / float64(rep.NumTrades)
		rep.Expectancy = rep.NetPnL / float64(rep.NumTrades)
	}
	return rep
}
