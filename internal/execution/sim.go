package execution

import "tradebot/internal/domain"

// Simulated fills market orders at the candle close and models a per-side fee.
type Simulated struct{ feeRate float64 }

func NewSimulated(feeRate float64) *Simulated { return &Simulated{feeRate: feeRate} }

func (s *Simulated) Execute(o domain.Order, c domain.Candle) (domain.Fill, error) {
	price := c.Close
	return domain.Fill{
		Symbol: o.Symbol, Side: o.Side, Qty: o.Qty, Price: price,
		Fee: s.feeRate * o.Qty * price, Time: c.CloseTime,
	}, nil
}
