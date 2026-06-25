package strategy

import (
	"math"

	"tradebot/internal/domain"
	"tradebot/internal/indicators"
)

type DonchianBreakout struct {
	symbol, timeframe                     string
	entryLookback, exitLookback, atrP, expN int
	slATRMult, tpPct                      float64
}

func NewDonchianBreakout(symbol, timeframe string, p map[string]float64) *DonchianBreakout {
	get := func(k string, d float64) float64 {
		if v, ok := p[k]; ok {
			return v
		}
		return d
	}
	return &DonchianBreakout{
		symbol: symbol, timeframe: timeframe,
		entryLookback: int(get("entry_lookback", 20)),
		exitLookback:  int(get("exit_lookback", 10)),
		atrP:          14,
		expN:          20,
		slATRMult:     get("sl_atr_mult", 2.5),
		tpPct:         get("tp_pct", 4.0),
	}
}

func (s *DonchianBreakout) Name() string      { return "donchian_breakout" }
func (s *DonchianBreakout) Kind() string      { return "breakout" }
func (s *DonchianBreakout) Symbol() string    { return s.symbol }
func (s *DonchianBreakout) Timeframe() string { return s.timeframe }
func (s *DonchianBreakout) Warmup() int       { return s.entryLookback + s.expN + 1 }

func (s *DonchianBreakout) Evaluate(h []domain.Candle, inPosition bool) *domain.Signal {
	if len(h) < s.Warmup() {
		return nil
	}
	i := len(h) - 1
	atr := indicators.ATR(h, s.atrP)
	if math.IsNaN(atr[i]) {
		return nil
	}
	if inPosition {
		// exit: close below the lowest close of the prior exitLookback candles.
		low := math.Inf(1)
		for j := i - s.exitLookback; j < i; j++ {
			if h[j].Close < low {
				low = h[j].Close
			}
		}
		if h[i].Close < low {
			return &domain.Signal{Symbol: s.symbol, Action: domain.Sell, Time: h[i].CloseTime, Reason: "10-bar channel-low exit"}
		}
		return nil
	}
	// entry: new entryLookback-high close + ATR expansion vs avg of last expN.
	high := math.Inf(-1)
	for j := i - s.entryLookback; j < i; j++ {
		if h[j].Close > high {
			high = h[j].Close
		}
	}
	var sumATR float64
	cnt := 0
	for j := i - s.expN; j < i; j++ {
		if !math.IsNaN(atr[j]) {
			sumATR += atr[j]
			cnt++
		}
	}
	avgATR := math.Inf(1)
	if cnt > 0 {
		avgATR = sumATR / float64(cnt)
	}
	if h[i].Close > high && atr[i] > avgATR {
		stop := s.slATRMult * atr[i]
		return &domain.Signal{
			Symbol: s.symbol, Action: domain.Buy, Time: h[i].CloseTime,
			StopDist: stop, TPDist: h[i].Close * s.tpPct / 100,
			Reason: "20-bar breakout + ATR expansion",
		}
	}
	return nil
}
