package execution

import (
	"tradebot/internal/binance"
	"tradebot/internal/domain"
)

// LiveExecutor places real market orders via the signed Binance client.
type LiveExecutor struct{ c *binance.Client }

func NewLive(c *binance.Client) *LiveExecutor { return &LiveExecutor{c: c} }

func (e *LiveExecutor) Execute(o domain.Order, _ domain.Candle) (domain.Fill, error) {
	fill, err := e.c.NewMarketOrder(o.Symbol, o.Side.String(), o.Qty)
	if err != nil {
		return domain.Fill{}, err
	}
	return domain.Fill{Symbol: o.Symbol, Side: o.Side, Qty: fill.Qty, Price: fill.Price, Fee: fill.Commission, Time: o.Time}, nil
}
