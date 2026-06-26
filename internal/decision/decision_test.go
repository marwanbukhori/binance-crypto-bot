package decision

import (
	"testing"

	"tradebot/internal/domain"
	"tradebot/internal/regime"
)

func TestLowVolChopDisablesTrend(t *testing.T) {
	if KindEnabled(regime.LowVolChop, "trend") {
		t.Fatal("trend must be disabled in LowVolChop")
	}
	if !KindEnabled(regime.LowVolChop, "reversion") {
		t.Fatal("reversion must stay enabled in LowVolChop")
	}
}

func TestChooseSkipsDisabledKind(t *testing.T) {
	cands := []Candidate{
		{Kind: "trend", Signal: domain.Signal{Action: domain.Buy, Reason: "trend buy"}},
		{Kind: "reversion", Signal: domain.Signal{Action: domain.Buy, Reason: "rev buy"}},
	}
	got := Choose(regime.LowVolChop, false, cands)
	if got == nil || got.Reason != "rev buy" {
		t.Fatalf("expected reversion buy in LowVolChop, got %+v", got)
	}
}

func TestChooseExitAlwaysAllowed(t *testing.T) {
	cands := []Candidate{{Kind: "trend", Signal: domain.Signal{Action: domain.Sell, Reason: "exit"}}}
	got := Choose(regime.LowVolChop, true, cands)
	if got == nil || got.Action != domain.Sell {
		t.Fatalf("exit must be allowed in any regime, got %+v", got)
	}
}

func TestChooseNoStackWhenInPosition(t *testing.T) {
	cands := []Candidate{{Kind: "trend", Signal: domain.Signal{Action: domain.Buy, Reason: "buy"}}}
	if got := Choose(regime.TrendingUp, true, cands); got != nil {
		t.Fatalf("must not open a second position while in one, got %+v", got)
	}
}

func TestPickReturnsChosenCandidate(t *testing.T) {
	cands := []Candidate{
		{Kind: "trend", Name: "ema_cross_trend", Signal: domain.Signal{Action: domain.Buy, Reason: "t"}},
		{Kind: "reversion", Name: "rsi_bb_reversion", Signal: domain.Signal{Action: domain.Buy, Reason: "r"}},
	}
	got := Pick(regime.LowVolChop, false, cands)
	if got == nil || got.Name != "rsi_bb_reversion" {
		t.Fatalf("LowVolChop must pick reversion, got %+v", got)
	}
}
