package store

import (
	"testing"

	"tradebot/internal/domain"
)

func TestRecordAndCount(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()

	if err := s.RecordOrder(domain.Order{Symbol: "BTCUSDT", Side: domain.Buy, Qty: 0.01, Price: 60000, BindingConstraint: "notional", EffectiveRiskPct: 0.15}); err != nil {
		t.Fatalf("record order: %v", err)
	}
	if err := s.RecordFill(domain.Fill{Symbol: "BTCUSDT", Side: domain.Buy, Qty: 0.01, Price: 60000, Fee: 0.9}); err != nil {
		t.Fatalf("record fill: %v", err)
	}
	n, err := s.CountFills()
	if err != nil || n != 1 {
		t.Fatalf("CountFills=%d err=%v want 1", n, err)
	}
}
