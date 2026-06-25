package engine

import (
	"tradebot/internal/domain"
	"tradebot/internal/execution"
	"tradebot/internal/portfolio"
	"tradebot/internal/risk"
	"tradebot/internal/store"
	"tradebot/internal/strategy"
)

type Engine struct {
	strat strategy.Strategy
	gate  *risk.Gate
	exec  execution.Executor
	pf    *portfolio.Portfolio
	store *store.Store
	filt  risk.Filters
}

func New(s strategy.Strategy, g *risk.Gate, ex execution.Executor, pf *portfolio.Portfolio, st *store.Store, f risk.Filters) *Engine {
	return &Engine{strat: s, gate: g, exec: ex, pf: pf, store: st, filt: f}
}

// OnClosedCandle drives one tick of the pipeline for a just-closed candle.
// history is closed candles ascending, newest last (== c).
func (e *Engine) OnClosedCandle(c domain.Candle, history []domain.Candle) error {
	pos := e.pf.Position(c.Symbol)
	inPos := pos.Qty > 0
	sig := e.strat.Evaluate(history, inPos)
	if sig == nil {
		return nil
	}
	acct := risk.Account{
		Equity:        e.pf.Equity(map[string]float64{c.Symbol: c.Close}),
		FreeUSDT:      e.pf.Cash(),
		PositionQty:   pos.Qty,
		OpenPositions: e.openCount(),
	}
	in := domain.Intent{
		Symbol: sig.Symbol, Action: sig.Action, Reason: sig.Reason,
		Price: c.Close, StopDist: sig.StopDist, TPDist: sig.TPDist, Time: c.CloseTime,
	}
	order, err := e.gate.Evaluate(in, acct, e.filt)
	if err != nil {
		return nil // rejected: logged by caller in real engine; skip for skeleton
	}
	if err := e.store.RecordOrder(order); err != nil {
		return err
	}
	fill, err := e.exec.Execute(order, c)
	if err != nil {
		return err
	}
	e.pf.Apply(fill)
	return e.store.RecordFill(fill)
}

func (e *Engine) openCount() int {
	// Skeleton tracks a single symbol; count is 1 if that position is open.
	// Generalized in Phase 2's multi-symbol engine.
	if e.pf.Position("BTCUSDT").Qty > 0 {
		return 1
	}
	return 0
}
