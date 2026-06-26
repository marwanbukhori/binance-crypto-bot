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

func TestTradesAndStats(t *testing.T) {
	s, _ := Open(":memory:")
	defer s.Close()
	s.RecordTrade(Trade{TS: 1, Symbol: "BTCUSDT", Strategy: "ema_cross_trend", Reason: "TP", EntryPx: 100, ExitPx: 104, Qty: 1, NetPnL: 3.5})
	s.RecordTrade(Trade{TS: 2, Symbol: "BTCUSDT", Strategy: "ema_cross_trend", Reason: "SL", EntryPx: 104, ExitPx: 102, Qty: 1, NetPnL: -2.2})
	ts, err := s.ListTrades(10)
	if err != nil || len(ts) != 2 { t.Fatalf("ListTrades=%d err=%v", len(ts), err) }
	if ts[0].TS != 2 { t.Fatalf("newest first expected, got TS=%d", ts[0].TS) }
	st, err := s.PerfStats()
	if err != nil { t.Fatalf("stats: %v", err) }
	if st.Trades != 2 || st.Wins != 1 { t.Fatalf("bad counts: %+v", st) }
	if st.WinRate < 0.49 || st.WinRate > 0.51 { t.Fatalf("winRate=%v want 0.5", st.WinRate) }
	if st.NetPnL < 1.29 || st.NetPnL > 1.31 { t.Fatalf("netPnL=%v want 1.3", st.NetPnL) }
}

func TestEquitySeriesAscending(t *testing.T) {
	s, _ := Open(":memory:")
	defer s.Close()
	s.RecordPnLSnapshot(2, 1010, 10)
	s.RecordPnLSnapshot(1, 1000, 0)
	eq, err := s.EquitySeries(10)
	if err != nil || len(eq) != 2 { t.Fatalf("equity=%d err=%v", len(eq), err) }
	if eq[0].TS != 1 || eq[1].TS != 2 { t.Fatalf("must be ascending: %+v", eq) }
}

func TestTradeRegimeRoundTrips(t *testing.T) {
	s, _ := Open(":memory:")
	defer s.Close()
	s.RecordTrade(Trade{TS: 1, Symbol: "BTCUSDT", Strategy: "ema_cross_trend", Regime: "TrendingUp", Reason: "TP", NetPnL: 4})
	ts, _ := s.ListTrades(5)
	if len(ts) != 1 || ts[0].Regime != "TrendingUp" {
		t.Fatalf("regime not persisted: %+v", ts)
	}
}
