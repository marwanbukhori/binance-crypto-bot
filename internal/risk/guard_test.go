package risk

import (
	"testing"

	"tradebot/internal/config"
)

func guardCfg() config.RiskCfg {
	return config.RiskCfg{DailyLossLimitPct: 3, WeeklyLossLimitPct: 8, HardFlattenDrawdownPct: 6}
}

func TestKillSwitchTripsOnDailyLoss(t *testing.T) {
	g := NewGuard(guardCfg(), 1000)
	g.StartDay(1000)
	g.StartWeek(1000)
	g.Mark(980) // -2% : ok
	if g.Killed() || !g.AllowEntry() {
		t.Fatal("should not be killed at -2%")
	}
	g.Mark(969) // -3.1% : trip
	if !g.Killed() || g.AllowEntry() {
		t.Fatalf("should be killed at -3.1%%, dailyLoss=%.2f", g.DailyLossPct())
	}
}

func TestHardFlattenThreshold(t *testing.T) {
	g := NewGuard(guardCfg(), 1000)
	g.StartDay(1000)
	g.StartWeek(1000)
	g.Mark(945) // -5.5%
	if g.ShouldFlatten() {
		t.Fatal("should not flatten at -5.5%")
	}
	g.Mark(939) // -6.1%
	if !g.ShouldFlatten() {
		t.Fatal("should flatten at -6.1%")
	}
}

func TestResetReArms(t *testing.T) {
	g := NewGuard(guardCfg(), 1000)
	g.StartDay(1000); g.StartWeek(1000)
	g.Mark(900)
	if !g.Killed() { t.Fatal("expected killed") }
	g.Reset()
	if g.Killed() || !g.AllowEntry() { t.Fatal("Reset must re-arm") }
}

func TestRollTimeResetsDailyBaselineAndReArms(t *testing.T) {
	g := NewGuard(guardCfg(), 1000)
	day1 := int64(1_700_000_000_000) // some ms
	g.RollTime(day1, 1000)
	g.Mark(960) // -4% -> killed
	if !g.Killed() { t.Fatal("expected kill at -4%") }
	day2 := day1 + 24*3600*1000 // next UTC day
	g.RollTime(day2, 960)       // new day: baseline reset to 960, re-armed
	if g.Killed() { t.Fatal("new day must re-arm the kill-switch") }
	g.Mark(950) // -1.04% vs 960 -> ok
	if g.Killed() { t.Fatalf("should be ok, dailyLoss=%.2f", g.DailyLossPct()) }
}

func TestRollTimeSameDayNoReset(t *testing.T) {
	g := NewGuard(guardCfg(), 1000)
	base := int64(1_700_000_000_000)
	g.RollTime(base, 1000)
	g.Mark(970)
	g.RollTime(base+3600_000, 970) // +1h, same UTC day
	if g.DailyLossPct() < 2.9 { t.Fatalf("baseline must NOT reset same day, dailyLoss=%.2f", g.DailyLossPct()) }
}
