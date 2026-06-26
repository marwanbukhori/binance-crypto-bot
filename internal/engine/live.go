package engine

import (
	"context"

	"tradebot/internal/decision"
	"tradebot/internal/domain"
	"tradebot/internal/execution"
	"tradebot/internal/marketdata"
	"tradebot/internal/portfolio"
	"tradebot/internal/regime"
	"tradebot/internal/risk"
	"tradebot/internal/store"
	"tradebot/internal/strategy"
)

type Live struct {
	syms   []string
	strats map[string][]strategy.Strategy
	gate   *risk.Gate
	guard  *risk.Guard
	cool   *risk.Cooldown
	exec   execution.Executor
	pf     *portfolio.Portfolio
	store  *store.Store
	filt   risk.Filters
	buf    *marketdata.Buffer
	open     map[string]*domain.Order
	idx      map[string]int // per-symbol candle counter for cooldown
	entryIdx map[string]int // per-symbol candle index at which the position was opened
}

func NewLive(syms []string, mkStrats func(string) []strategy.Strategy, gate *risk.Gate, guard *risk.Guard, cool *risk.Cooldown, ex execution.Executor, pf *portfolio.Portfolio, st *store.Store, f risk.Filters, buf *marketdata.Buffer) *Live {
	strats := map[string][]strategy.Strategy{}
	for _, s := range syms {
		strats[s] = mkStrats(s)
	}
	return &Live{syms: syms, strats: strats, gate: gate, guard: guard, cool: cool, exec: ex, pf: pf, store: st, filt: f, buf: buf, open: map[string]*domain.Order{}, idx: map[string]int{}, entryIdx: map[string]int{}}
}

func (l *Live) Run(ctx context.Context, s marketdata.Stream) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case c, ok := <-s.Candles():
			if !ok {
				return nil
			}
			if err := l.OnCandle(c); err != nil {
				return err
			}
		}
	}
}

func (l *Live) OnCandle(c domain.Candle) error {
	if !c.Closed {
		return nil
	}
	sym := c.Symbol
	hist := l.buf.Add(c)
	i := l.idx[sym]
	l.idx[sym] = i + 1

	// mark equity: current symbol at its candle close; other open symbols at their
	// latest known close (from the buffer), falling back to entry price if no history.
	latestClose := func(s string, fallback float64) float64 {
		if h := l.buf.History(s); len(h) > 0 {
			return h[len(h)-1].Close
		}
		return fallback
	}
	mark := map[string]float64{}
	for s, o := range l.open {
		if s == sym {
			mark[s] = c.Close
		} else {
			mark[s] = latestClose(s, o.Price)
		}
	}
	mark[sym] = c.Close
	eq := l.pf.Equity(mark)
	l.guard.RollTime(c.CloseTime, eq)
	l.guard.Mark(eq)
	if l.guard.Killed() {
		_ = l.store.SaveKillState(true, "loss limit", c.CloseTime)
	}

	pos := l.pf.Position(sym)
	inPos := pos.Qty > 0

	// 1) manage open position: hard-flatten or TP/SL on this candle.
	if o, ok := l.open[sym]; ok {
		flatten := l.guard.ShouldFlatten()
		// Guard: TP/SL may only trigger on a candle *after* the entry candle, matching
		// RunMulti's `i-1 > ps.entryIdx` guard to prevent look-ahead same-bar exits.
		pastEntry := i > l.entryIdx[sym]
		hitStop := pastEntry && o.StopPrice > 0 && c.Low <= o.StopPrice
		hitTP := pastEntry && o.TPPrice > 0 && c.High >= o.TPPrice
		if flatten || hitStop || hitTP {
			px := c.Close
			reason := "flatten"
			if !flatten && hitStop {
				px, reason = o.StopPrice, "SL"
			} else if !flatten && hitTP {
				px, reason = o.TPPrice, "TP"
			}
			exit := domain.Order{Symbol: sym, Side: domain.Sell, Qty: pos.Qty, Price: px, Type: "MARKET", Reason: reason, Time: c.CloseTime}
			fill, err := l.exec.Execute(exit, domain.Candle{Symbol: sym, Close: px, CloseTime: c.CloseTime})
			if err != nil {
				return err
			}
			before := l.pf.Realized()
			l.pf.Apply(fill)
			if l.pf.Realized() < before {
				l.cool.NoteLoss(sym, i)
			}
			_ = l.store.RecordOrder(exit)
			_ = l.store.RecordFill(fill)
			delete(l.open, sym)
			inPos = false
		}
	}

	// 2) strategy candidates + regime + decision.
	var cands []decision.Candidate
	for _, stg := range l.strats[sym] {
		if sig := stg.Evaluate(hist, inPos); sig != nil {
			_ = l.store.RecordSignal(sym, stg.Name(), sig.Action.String(), sig.Reason, c.CloseTime)
			cands = append(cands, decision.Candidate{Kind: stg.Kind(), Signal: *sig})
		}
	}
	reg := regime.Classify(hist)
	sig := decision.Choose(reg, inPos, cands)
	_ = l.store.RecordPnLSnapshot(c.CloseTime, eq, l.pf.Realized())
	if sig == nil {
		return nil
	}
	if sig.Action == domain.Sell {
		if _, ok := l.open[sym]; ok {
			exit := domain.Order{Symbol: sym, Side: domain.Sell, Qty: pos.Qty, Price: c.Close, Type: "MARKET", Reason: sig.Reason, Time: c.CloseTime}
			fill, err := l.exec.Execute(exit, c)
			if err != nil {
				return err
			}
			before := l.pf.Realized()
			l.pf.Apply(fill)
			if l.pf.Realized() < before {
				l.cool.NoteLoss(sym, i)
			}
			_ = l.store.RecordOrder(exit)
			_ = l.store.RecordFill(fill)
			delete(l.open, sym)
		}
		return nil
	}
	// Buy
	if !l.guard.AllowEntry() || l.cool.Blocked(sym, i) {
		return nil
	}
	var deployed float64
	for s, o := range l.open {
		var m float64
		if s == sym {
			m = c.Close
		} else {
			m = latestClose(s, o.Price)
		}
		deployed += o.Qty * m
	}
	acct := risk.Account{Equity: l.pf.Cash() + deployed, FreeUSDT: l.pf.Cash(), DeployedNotional: deployed, OpenPositions: len(l.open)}
	in := domain.Intent{Symbol: sym, Action: domain.Buy, Price: c.Close, StopDist: sig.StopDist, TPDist: sig.TPDist, Reason: sig.Reason, Time: c.CloseTime}
	o, err := l.gate.Evaluate(in, acct, l.filt)
	if err != nil {
		return nil // rejected (logged via signals)
	}
	fill, err := l.exec.Execute(o, c)
	if err != nil {
		return err
	}
	l.pf.Apply(fill)
	oo := o
	l.open[sym] = &oo
	l.entryIdx[sym] = i
	_ = l.store.RecordOrder(o)
	return l.store.RecordFill(fill)
}
