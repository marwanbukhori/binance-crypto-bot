package strategy

import (
	"fmt"
	"math"

	"tradebot/internal/domain"
	"tradebot/internal/indicators"
)

type RSIBBReversion struct {
	symbol, timeframe                                   string
	rsiPeriod, bbPeriod, smaTrend, adxPeriod, atrPeriod int
	bbStdev, oversold, exitRSI, adxMax, minEdgePct      float64
	slATRMult, tpRewardMult                             float64
}

func NewRSIBBReversion(symbol, timeframe string, p map[string]float64) *RSIBBReversion {
	get := func(k string, d float64) float64 {
		if v, ok := p[k]; ok {
			return v
		}
		return d
	}
	return &RSIBBReversion{
		symbol: symbol, timeframe: timeframe,
		rsiPeriod: 14, bbPeriod: 20, smaTrend: 200, adxPeriod: 14, atrPeriod: 14,
		bbStdev:   get("bb_stdev", 2.0),
		oversold:  get("oversold", 30),
		exitRSI:   get("exit_rsi", 60),
		adxMax:    get("adx_max", 25),
		minEdgePct: get("min_edge_pct", 1.2),
		slATRMult: get("sl_atr_mult", 2.0),
		tpRewardMult: get("tp_reward_mult", 1.6),
	}
}

func (s *RSIBBReversion) Name() string      { return "rsi_bb_reversion" }
func (s *RSIBBReversion) Kind() string      { return "reversion" }
func (s *RSIBBReversion) Symbol() string    { return s.symbol }
func (s *RSIBBReversion) Timeframe() string { return s.timeframe }
func (s *RSIBBReversion) Warmup() int       { return s.smaTrend + 1 }

func (s *RSIBBReversion) Evaluate(h []domain.Candle, inPosition bool) *domain.Signal {
	if len(h) < s.Warmup() {
		return nil
	}
	closes := make([]float64, len(h))
	for i, c := range h {
		closes[i] = c.Close
	}
	rsi := indicators.RSI(closes, s.rsiPeriod)
	mid, _, lower := indicators.Bollinger(closes, s.bbPeriod, s.bbStdev)
	sma := indicators.SMA(closes, s.smaTrend)
	adx := indicators.ADX(h, s.adxPeriod)
	atr := indicators.ATR(h, s.atrPeriod)
	i := len(h) - 1
	if math.IsNaN(rsi[i]) || math.IsNaN(mid[i]) || math.IsNaN(lower[i]) || math.IsNaN(sma[i]) || math.IsNaN(adx[i]) || math.IsNaN(atr[i]) {
		return nil
	}
	c := h[i].Close
	if inPosition {
		if c >= mid[i] || rsi[i] >= s.exitRSI {
			return &domain.Signal{Symbol: s.symbol, Action: domain.Sell, Time: h[i].CloseTime, Reason: "reversion mean/RSI exit"}
		}
		return nil
	}
	// entry: oversold + below lower band + above SMA200 (hard gate) + not strongly trending + min edge.
	edgePct := (mid[i] - c) / c * 100
	if rsi[i] <= s.oversold && c <= lower[i] && c > sma[i] && adx[i] < s.adxMax && edgePct >= s.minEdgePct {
		stop := s.slATRMult * atr[i]
		return &domain.Signal{
			Symbol: s.symbol, Action: domain.Buy, Time: h[i].CloseTime,
			StopDist: stop, TPDist: s.tpRewardMult * stop,
			Reason: fmt.Sprintf("RSI %.1f<=%.0f + <lowerBB + >SMA200 + edge %.2f%%", rsi[i], s.oversold, edgePct),
		}
	}
	return nil
}
