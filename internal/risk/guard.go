package risk

import "tradebot/internal/config"

// Guard enforces daily/weekly loss limits, the kill-switch, and the hard-flatten ceiling.
type Guard struct {
	dailyLimit, weeklyLimit, flattenLimit float64
	dayStart, weekStart, cur              float64
	killed                                bool
}

func NewGuard(cfg config.RiskCfg, startEquity float64) *Guard {
	return &Guard{
		dailyLimit:   cfg.DailyLossLimitPct,
		weeklyLimit:  cfg.WeeklyLossLimitPct,
		flattenLimit: cfg.HardFlattenDrawdownPct,
		dayStart:     startEquity,
		weekStart:    startEquity,
		cur:          startEquity,
	}
}

func (g *Guard) StartDay(equity float64)  { g.dayStart = equity }
func (g *Guard) StartWeek(equity float64) { g.weekStart = equity }

func lossPct(start, cur float64) float64 {
	if start <= 0 {
		return 0
	}
	return (start - cur) / start * 100
}

func (g *Guard) DailyLossPct() float64  { return lossPct(g.dayStart, g.cur) }
func (g *Guard) WeeklyLossPct() float64 { return lossPct(g.weekStart, g.cur) }

func (g *Guard) Mark(equity float64) {
	g.cur = equity
	if g.DailyLossPct() >= g.dailyLimit || g.WeeklyLossPct() >= g.weeklyLimit {
		g.killed = true
	}
}

func (g *Guard) AllowEntry() bool    { return !g.killed }
func (g *Guard) Killed() bool        { return g.killed }
func (g *Guard) ShouldFlatten() bool { return g.DailyLossPct() >= g.flattenLimit }
func (g *Guard) Reset()              { g.killed = false }
