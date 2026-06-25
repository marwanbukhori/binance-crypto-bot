package strategy

import "tradebot/internal/domain"

// Strategy consumes closed candles (ascending, newest last) and may emit a Signal.
type Strategy interface {
	Name() string
	Kind() string // "trend" | "reversion" | "breakout" | "momentum"
	Symbol() string
	Timeframe() string
	Warmup() int
	Evaluate(history []domain.Candle, inPosition bool) *domain.Signal
}
