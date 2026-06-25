package backtest

import (
	"tradebot/internal/config"
	"tradebot/internal/domain"
	"tradebot/internal/risk"
	"tradebot/internal/strategy"
)

type Trade struct {
	EntryTime, ExitTime int64
	EntryPx, ExitPx, Qty, NetPnL float64
	ExitReason string
}

type Report struct {
	Trades                                            []Trade
	NumTrades, Wins                                   int
	WinRate, NetPnL, Expectancy, MaxDrawdownPct, FinalEquity float64
}

func Run(s strategy.Strategy, g *risk.Gate, f risk.Filters, candles []domain.Candle, startCash, feeRate float64) Report {
	cash := startCash
	var open *domain.Order // active entry order (carries stop/TP); nil when flat
	var entryIdx int
	var rep Report
	peak := startCash

	equityAt := func(i int) float64 {
		if open == nil {
			return cash
		}
		return cash + open.Qty*candles[i].Close
	}
	closeTrade := func(exitPx float64, exitTime int64, reason string) {
		proceeds := open.Qty*exitPx - feeRate*open.Qty*exitPx
		entryCost := open.Qty*open.Price + feeRate*open.Qty*open.Price
		net := proceeds - entryCost
		cash += proceeds
		rep.Trades = append(rep.Trades, Trade{
			EntryTime: open.Time, ExitTime: exitTime, EntryPx: open.Price,
			ExitPx: exitPx, Qty: open.Qty, NetPnL: net, ExitReason: reason,
		})
		rep.NumTrades++
		rep.NetPnL += net
		if net > 0 {
			rep.Wins++
		}
		open = nil
	}

	for i := 1; i <= len(candles); i++ {
		c := candles[i-1]
		hist := candles[:i]

		// 1) Manage an open position: TP/SL active from the candle AFTER entry.
		if open != nil && i-1 > entryIdx {
			if kind, px := CheckExit(*open, c); kind == ExitStop {
				closeTrade(px, c.CloseTime, "SL")
			} else if kind == ExitTP {
				closeTrade(px, c.CloseTime, "TP")
			}
		}

		// 2) Strategy decision.
		inPos := open != nil
		if sig := s.Evaluate(hist, inPos); sig != nil {
			if sig.Action == domain.Buy && open == nil {
				acct := risk.Account{Equity: cash, FreeUSDT: cash, OpenPositions: 0}
				in := domain.Intent{Symbol: sig.Symbol, Action: domain.Buy, Price: c.Close,
					StopDist: sig.StopDist, TPDist: sig.TPDist, Reason: sig.Reason, Time: c.CloseTime}
				if o, err := g.Evaluate(in, acct, f); err == nil {
					cash -= o.Qty*o.Price + feeRate*o.Qty*o.Price
					oo := o
					open = &oo
					entryIdx = i - 1
				}
			} else if sig.Action == domain.Sell && open != nil {
				closeTrade(c.Close, c.CloseTime, "rule")
			}
		}

		// 3) Equity + drawdown.
		eq := equityAt(i - 1)
		if eq > peak {
			peak = eq
		}
		if peak > 0 {
			if dd := (peak - eq) / peak * 100; dd > rep.MaxDrawdownPct {
				rep.MaxDrawdownPct = dd
			}
		}
	}

	// Close any dangling position at the last close for reporting.
	if open != nil {
		closeTrade(candles[len(candles)-1].Close, candles[len(candles)-1].CloseTime, "eod")
		// I3: include the final forced-close equity point in MaxDrawdownPct.
		eq := cash
		if eq > peak {
			peak = eq
		}
		if peak > 0 {
			if dd := (peak - eq) / peak * 100; dd > rep.MaxDrawdownPct {
				rep.MaxDrawdownPct = dd
			}
		}
	}
	rep.FinalEquity = cash
	if rep.NumTrades > 0 {
		rep.WinRate = float64(rep.Wins) / float64(rep.NumTrades)
		rep.Expectancy = rep.NetPnL / float64(rep.NumTrades)
	}
	return rep
}

type SweepRow struct {
	FeeRoundTripPct, NetPnL, Expectancy, WinRate float64
	Profitable                                   bool
}

// Sweep runs the backtest at several round-trip fee levels (percent).
// C1: a fresh Gate is built for each fee level so that the R:R gate uses the
// same fee fraction as the fill simulation — sweeping both legs together.
func Sweep(s strategy.Strategy, cfg config.RiskCfg, f risk.Filters, candles []domain.Candle, startCash float64, feesRoundTrip []float64) []SweepRow {
	rows := make([]SweepRow, 0, len(feesRoundTrip))
	for _, rt := range feesRoundTrip {
		g := risk.NewGate(cfg, rt/100) // round-trip fraction for the gate
		perSide := rt / 2 / 100        // per-side fraction for fills
		rep := Run(s, g, f, candles, startCash, perSide)
		rows = append(rows, SweepRow{
			FeeRoundTripPct: rt, NetPnL: rep.NetPnL,
			Expectancy: rep.Expectancy, WinRate: rep.WinRate,
			Profitable: rep.NetPnL > 0,
		})
	}
	return rows
}
