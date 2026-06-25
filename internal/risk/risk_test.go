package risk

import (
	"math"
	"testing"

	"tradebot/internal/config"
	"tradebot/internal/domain"
)

func gate() *Gate {
	return NewGate(config.RiskCfg{
		MaxPctPerTrade: 5, RiskPerTradePct: 1, MaxOpenPositions: 2,
		PortfolioMaxDeployedPct: 10, TPRewardMult: 1.6,
	}, 0.003)
}

func TestSizingWorkedExample(t *testing.T) {
	// equity 1000, 1% risk, BTC 60000, stop_dist 1800 (3% wide).
	in := domain.Intent{Symbol: "BTCUSDT", Action: domain.Buy, Price: 60000, StopDist: 1800, TPDist: 2880}
	a := Account{Equity: 1000, FreeUSDT: 1000}
	f := Filters{StepSize: 0.000001, MinQty: 0.00001, MinNotional: 10}
	o, err := gate().Evaluate(in, a, f)
	if err != nil {
		t.Fatalf("unexpected reject: %v", err)
	}
	if math.Abs(o.Qty-0.000833) > 1e-6 {
		t.Fatalf("qty=%v want ~0.000833", o.Qty)
	}
	if o.BindingConstraint != "notional" {
		t.Fatalf("binding=%q want notional", o.BindingConstraint)
	}
	if math.Abs(o.EffectiveRiskPct-0.15) > 0.02 {
		t.Fatalf("effective risk=%v%% want ~0.15%%", o.EffectiveRiskPct)
	}
}

func TestRejectsBelowMinNotional(t *testing.T) {
	in := domain.Intent{Symbol: "BTCUSDT", Action: domain.Buy, Price: 60000, StopDist: 1800, TPDist: 2880}
	a := Account{Equity: 1000, FreeUSDT: 1000}
	f := Filters{StepSize: 0.000001, MinQty: 0.00001, MinNotional: 100} // 50 USDT notional < 100
	if _, err := gate().Evaluate(in, a, f); err == nil {
		t.Fatal("expected reject below MIN_NOTIONAL")
	}
}

func TestRejectsBadRiskReward(t *testing.T) {
	// TP barely above stop -> post-fee R:R < 1.3.
	in := domain.Intent{Symbol: "BTCUSDT", Action: domain.Buy, Price: 60000, StopDist: 1800, TPDist: 1900}
	a := Account{Equity: 1000, FreeUSDT: 1000}
	f := Filters{StepSize: 0.000001, MinQty: 0.00001, MinNotional: 10}
	if _, err := gate().Evaluate(in, a, f); err == nil {
		t.Fatal("expected reject on poor R:R")
	}
}

func TestSellExitsFullPosition(t *testing.T) {
	in := domain.Intent{Symbol: "BTCUSDT", Action: domain.Sell, Price: 61000}
	a := Account{Equity: 1000, FreeUSDT: 500, PositionQty: 0.000833, OpenPositions: 1}
	f := Filters{StepSize: 0.000001, MinQty: 0.00001, MinNotional: 10}
	o, err := gate().Evaluate(in, a, f)
	if err != nil {
		t.Fatalf("sell rejected: %v", err)
	}
	if o.Side != domain.Sell || math.Abs(o.Qty-0.000833) > 1e-9 {
		t.Fatalf("sell qty=%v want full 0.000833", o.Qty)
	}
}

func TestRejectsWhenMaxPositionsReached(t *testing.T) {
	in := domain.Intent{Symbol: "ETHUSDT", Action: domain.Buy, Price: 3000, StopDist: 90, TPDist: 144}
	a := Account{Equity: 1000, FreeUSDT: 1000, OpenPositions: 2}
	f := Filters{StepSize: 0.0001, MinQty: 0.001, MinNotional: 10}
	if _, err := gate().Evaluate(in, a, f); err == nil {
		t.Fatal("expected reject at max open positions")
	}
}
