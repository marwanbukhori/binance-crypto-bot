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

func TestKillStatePersists(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil { t.Fatalf("open: %v", err) }
	defer s.Close()
	killed, err := s.LoadKillState()
	if err != nil || killed { t.Fatalf("fresh state must be not-killed, got %v err=%v", killed, err) }
	if err := s.SaveKillState(true, "daily loss", 123); err != nil { t.Fatalf("save: %v", err) }
	killed, err = s.LoadKillState()
	if err != nil || !killed { t.Fatalf("expected killed after save, got %v err=%v", killed, err) }
}

func TestRecordSignalAndSnapshot(t *testing.T) {
	s, _ := Open(":memory:")
	defer s.Close()
	if err := s.RecordSignal("BTCUSDT", "ema_cross_trend", "BUY", "cross", 1); err != nil { t.Fatal(err) }
	if err := s.RecordPnLSnapshot(2, 1000, 5.5); err != nil { t.Fatal(err) }

	n, err := s.CountSignals()
	if err != nil || n != 1 {
		t.Fatalf("CountSignals=%d err=%v, want 1", n, err)
	}
	m, err := s.CountPnLSnapshots()
	if err != nil || m != 1 {
		t.Fatalf("CountPnLSnapshots=%d err=%v, want 1", m, err)
	}
}
