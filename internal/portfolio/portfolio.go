package portfolio

import "tradebot/internal/domain"

type Portfolio struct {
	cash     float64
	realized float64
	pos      map[string]domain.Position
	fees     map[string]float64 // accumulated buy fees per symbol
}

func New(cashUSDT float64) *Portfolio {
	return &Portfolio{cash: cashUSDT, pos: map[string]domain.Position{}, fees: map[string]float64{}}
}

func (p *Portfolio) Apply(f domain.Fill) {
	pos := p.pos[f.Symbol]
	switch f.Side {
	case domain.Buy:
		newQty := pos.Qty + f.Qty
		if newQty > 0 {
			pos.AvgEntry = (pos.AvgEntry*pos.Qty + f.Price*f.Qty) / newQty
		}
		pos.Qty = newQty
		pos.Symbol = f.Symbol
		p.cash -= f.Price*f.Qty + f.Fee
		p.fees[f.Symbol] += f.Fee
	case domain.Sell:
		buyFee := p.fees[f.Symbol]
		p.realized += (f.Price-pos.AvgEntry)*f.Qty - buyFee - f.Fee
		p.fees[f.Symbol] = 0
		pos.Qty -= f.Qty
		p.cash += f.Price*f.Qty - f.Fee
		if pos.Qty <= 1e-12 {
			pos = domain.Position{Symbol: f.Symbol}
		}
	}
	p.pos[f.Symbol] = pos
}

func (p *Portfolio) Cash() float64     { return p.cash }
func (p *Portfolio) Realized() float64 { return p.realized }

func (p *Portfolio) Position(symbol string) domain.Position { return p.pos[symbol] }

func (p *Portfolio) Equity(mark map[string]float64) float64 {
	eq := p.cash
	for sym, pos := range p.pos {
		eq += pos.Qty * mark[sym]
	}
	return eq
}
