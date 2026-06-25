package backtest

import "tradebot/internal/domain"

type ExitKind int

const (
	NoExit ExitKind = iota
	ExitStop
	ExitTP
)

// CheckExit applies pessimistic intrabar exit-precedence for a long position.
// If the candle straddles both stop and TP, the stop is assumed to fill first.
func CheckExit(o domain.Order, c domain.Candle) (ExitKind, float64) {
	hitStop := o.StopPrice > 0 && c.Low <= o.StopPrice
	hitTP := o.TPPrice > 0 && c.High >= o.TPPrice
	switch {
	case hitStop:
		return ExitStop, o.StopPrice
	case hitTP:
		return ExitTP, o.TPPrice
	default:
		return NoExit, 0
	}
}
