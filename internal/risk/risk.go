package risk

import (
	"fmt"
	"math"

	"tradebot/internal/config"
	"tradebot/internal/domain"
)

type Account struct {
	Equity, FreeUSDT, DeployedNotional float64
	OpenPositions                      int
	PositionQty                        float64
}

type Filters struct{ StepSize, MinQty, MinNotional float64 }

type Gate struct {
	cfg config.RiskCfg
	fee float64 // round-trip fraction for the R:R gate, e.g. 0.003
	rr  float64 // min post-fee reward:risk
}

// freeCashBuffer is the haircut applied to free USDT when computing max buy qty,
// matching spec §6: free_usdt × 0.995 to leave a thin cash reserve.
const freeCashBuffer = 0.995

func NewGate(c config.RiskCfg, feeFraction float64) *Gate {
	return &Gate{cfg: c, fee: feeFraction, rr: 1.3}
}

func floorTo(v, step float64) float64 {
	if step <= 0 {
		return v
	}
	return math.Floor(v/step) * step
}

func (g *Gate) Evaluate(in domain.Intent, a Account, f Filters) (domain.Order, error) {
	if in.Action == domain.Sell {
		if a.PositionQty <= 0 {
			return domain.Order{}, fmt.Errorf("sell with no position")
		}
		qty := floorTo(a.PositionQty, f.StepSize)
		return domain.Order{
			Symbol: in.Symbol, Side: domain.Sell, Qty: qty, Price: in.Price,
			Type: "MARKET", Reason: in.Reason, BindingConstraint: "exit", Time: in.Time,
		}, nil
	}

	if a.OpenPositions >= g.cfg.MaxOpenPositions {
		return domain.Order{}, fmt.Errorf("max open positions (%d) reached", g.cfg.MaxOpenPositions)
	}
	if in.StopDist <= 0 || in.Price <= 0 {
		return domain.Order{}, fmt.Errorf("buy needs positive price and stop distance")
	}

	// Post-fee net reward:risk gate (spec §6).
	// M1: g.fee is the full round-trip fraction applied to each side intentionally
	// per spec §6 formula — do NOT halve it to a per-side fee here.
	feeAbs := g.fee * in.Price
	netRR := (in.TPDist - feeAbs) / (in.StopDist + feeAbs)
	if netRR < g.rr {
		return domain.Order{}, fmt.Errorf("post-fee R:R %.2f < %.2f", netRR, g.rr)
	}

	riskUSDT := a.Equity * g.cfg.RiskPerTradePct / 100
	qtyRisk := riskUSDT / in.StopDist
	qtyCap := (a.Equity * g.cfg.MaxPctPerTrade / 100) / in.Price
	roomPortfolio := a.Equity*g.cfg.PortfolioMaxDeployedPct/100 - a.DeployedNotional
	qtyPortfolio := math.Max(0, roomPortfolio) / in.Price
	qtyFree := (a.FreeUSDT * freeCashBuffer) / in.Price

	qty := qtyRisk
	binding := "risk"
	if qtyCap < qty {
		qty, binding = qtyCap, "notional"
	}
	if qtyPortfolio < qty {
		qty, binding = qtyPortfolio, "portfolio"
	}
	if qtyFree < qty {
		qty, binding = qtyFree, "free"
	}

	qty = floorTo(qty, f.StepSize)
	if qty < f.MinQty || qty <= 0 {
		return domain.Order{}, fmt.Errorf("qty %.8f below MinQty %.8f", qty, f.MinQty)
	}
	if qty*in.Price < f.MinNotional*1.01 {
		return domain.Order{}, fmt.Errorf("notional %.2f below MinNotional %.2f", qty*in.Price, f.MinNotional)
	}

	effRisk := qty * in.StopDist / a.Equity * 100
	return domain.Order{
		Symbol: in.Symbol, Side: domain.Buy, Qty: qty, Price: in.Price,
		StopPrice: in.Price - in.StopDist, TPPrice: in.Price + in.TPDist,
		Type: "MARKET", Reason: in.Reason, BindingConstraint: binding,
		EffectiveRiskPct: effRisk, Time: in.Time,
	}, nil
}
