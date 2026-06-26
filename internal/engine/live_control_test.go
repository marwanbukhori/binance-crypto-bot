package engine

import (
	"strconv"
	"testing"

	"tradebot/internal/config"
	"tradebot/internal/control"
	"tradebot/internal/domain"
	"tradebot/internal/execution"
	"tradebot/internal/marketdata"
	"tradebot/internal/portfolio"
	"tradebot/internal/risk"
	"tradebot/internal/store"
	"tradebot/internal/strategy"
	"tradebot/internal/telegram"
)

func newLive(t *testing.T, autonomous bool) (*Live, *control.Controller, *store.Store) {
	st, _ := store.Open(":memory:")
	buf := marketdata.NewBuffer(400)
	gate := risk.NewGate(config.RiskCfg{MaxPctPerTrade: 90, RiskPerTradePct: 5, MaxOpenPositions: 1, PortfolioMaxDeployedPct: 100, TPRewardMult: 1.6}, 0.003)
	guard := risk.NewGuard(config.RiskCfg{DailyLossLimitPct: 90, WeeklyLossLimitPct: 95, HardFlattenDrawdownPct: 99}, 10000)
	pf := portfolio.New(10000)
	mk := func(sym string) []strategy.Strategy { return []strategy.Strategy{strategy.NewEMACross(sym, "1h", nil)} }
	l := NewLive([]string{"BTCUSDT"}, mk, gate, guard, risk.NewCooldown(2), execution.NewSimulated(0.0015), pf, st, risk.Filters{StepSize: 0.00001, MinQty: 0.00001, MinNotional: 5}, buf)
	ctrl := control.New()
	l.SetControl(ctrl, telegram.NoopNotifier{}, autonomous)
	return l, ctrl, st
}

func feedUptrendThenFlat(l *Live) {
	closes := []float64{}
	for i := 0; i < 40; i++ { closes = append(closes, 100) }
	for i := 0; i < 50; i++ { closes = append(closes, 100+float64(i)*4) }
	for i, c := range closes {
		_ = l.OnCandle(domain.Candle{Symbol: "BTCUSDT", Close: c, High: c + 5, Low: c - 5, Closed: true, CloseTime: int64(i) * 3600_000})
	}
}

func TestApproveFirstHoldsUntilApproved(t *testing.T) {
	l, ctrl, st := newLive(t, false) // approve-first
	defer st.Close()
	feedUptrendThenFlat(l)
	if n, _ := st.CountFills(); n != 0 {
		t.Fatalf("approve-first must not auto-execute; got %d fills", n)
	}
	// approve everything pending, then one more candle to drain+execute
	for _, o := range drainPending(ctrl) { ctrl.Approve(o) }
	_ = l.OnCandle(domain.Candle{Symbol: "BTCUSDT", Close: 300, High: 305, Low: 295, Closed: true, CloseTime: 999 * 3600_000})
	if n, _ := st.CountFills(); n < 1 {
		t.Fatalf("approved order should have executed, got %d fills", n)
	}
}

func TestPausedBlocksEntries(t *testing.T) {
	l, ctrl, st := newLive(t, true) // autonomous
	defer st.Close()
	ctrl.Pause()
	feedUptrendThenFlat(l)
	if n, _ := st.CountFills(); n != 0 {
		t.Fatalf("paused engine must not enter; got %d fills", n)
	}
}

func drainPending(ctrl *control.Controller) []string {
	// tokens are sequential ("1","2",...); approve a generous range (use strconv.Itoa).
	out := []string{}
	for i := 1; i <= 50; i++ { out = append(out, strconv.Itoa(i)) }
	return out
}
