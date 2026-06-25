package strategy

import (
	"math"

	"tradebot/internal/domain"
	"tradebot/internal/indicators"
)

type MACDMomentum struct {
	symbol, timeframe                       string
	fast, slow, signal, trendEMA, rsiP, adxP, atrP int
	rsiMax, adxMin, slATRMult, tpRewardMult float64
	lastExitTime                            int64
}

func NewMACDMomentum(symbol, timeframe string, p map[string]float64) *MACDMomentum {
	get := func(k string, d float64) float64 {
		if v, ok := p[k]; ok {
			return v
		}
		return d
	}
	return &MACDMomentum{
		symbol: symbol, timeframe: timeframe,
		fast: 12, slow: 26, signal: 9, trendEMA: 100, rsiP: 14, adxP: 14, atrP: 14,
		rsiMax:    get("rsi_max", 75),
		adxMin:    get("adx_min", 20),
		slATRMult: get("sl_atr_mult", 2.0),
		tpRewardMult: get("tp_reward_mult", 1.6),
		lastExitTime: -1,
	}
}

func (s *MACDMomentum) Name() string      { return "macd_momentum" }
func (s *MACDMomentum) Kind() string      { return "momentum" }
func (s *MACDMomentum) Symbol() string    { return s.symbol }
func (s *MACDMomentum) Timeframe() string { return s.timeframe }
func (s *MACDMomentum) Warmup() int       { return s.trendEMA + 1 }

func (s *MACDMomentum) Evaluate(h []domain.Candle, inPosition bool) *domain.Signal {
	if len(h) < s.Warmup() {
		return nil
	}
	closes := make([]float64, len(h))
	for i, c := range h {
		closes[i] = c.Close
	}
	macd, sig, hist := indicators.MACD(closes, s.fast, s.slow, s.signal)
	ema := indicators.EMA(closes, s.trendEMA)
	rsi := indicators.RSI(closes, s.rsiP)
	adx := indicators.ADX(h, s.adxP)
	atr := indicators.ATR(h, s.atrP)
	i := len(h) - 1
	if i < 1 || math.IsNaN(macd[i]) || math.IsNaN(macd[i-1]) || math.IsNaN(sig[i]) || math.IsNaN(sig[i-1]) || math.IsNaN(hist[i]) || math.IsNaN(hist[i-1]) || math.IsNaN(ema[i]) || math.IsNaN(rsi[i]) || math.IsNaN(adx[i]) || math.IsNaN(atr[i]) {
		return nil
	}
	crossUp := macd[i-1] <= sig[i-1] && macd[i] > sig[i]
	crossDown := macd[i-1] >= sig[i-1] && macd[i] < sig[i]
	c := h[i].Close
	if inPosition {
		if crossDown {
			s.lastExitTime = h[i].CloseTime
			return &domain.Signal{Symbol: s.symbol, Action: domain.Sell, Time: h[i].CloseTime, Reason: "MACD recross exit"}
		}
		return nil
	}
	// 1-candle debounce after an exit.
	if s.lastExitTime >= 0 && h[i-1].CloseTime == s.lastExitTime {
		return nil
	}
	histRising := hist[i] > hist[i-1]
	if crossUp && histRising && c > ema[i] && adx[i] >= s.adxMin && rsi[i] <= s.rsiMax && hist[i] > 0 {
		stop := s.slATRMult * atr[i]
		return &domain.Signal{
			Symbol: s.symbol, Action: domain.Buy, Time: h[i].CloseTime,
			StopDist: stop, TPDist: s.tpRewardMult * stop,
			Reason: "MACD cross up + hist rising + >EMA100 + ADX ok",
		}
	}
	return nil
}
