package execution

import "tradebot/internal/domain"

// Executor turns an approved Order into a Fill against a candle.
type Executor interface {
	Execute(o domain.Order, c domain.Candle) (domain.Fill, error)
}
