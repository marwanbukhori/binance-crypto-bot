package strategy

import (
	"fmt"
	"math"

	"tradebot/internal/domain"
	"tradebot/internal/indicators"
)

// EMACross is the ema_cross_trend strategy: EMA9>EMA21 cross with ADX>=adxMin.
type EMACross struct {
	symbol, timeframe               string
	fast, slow, adxPeriod, atrPeriod int
	adxMin, slATRMult, tpRewardMult  float64
}

func NewEMACross(symbol, timeframe string, p map[string]float64) *EMACross {
	get := func(k string, d float64) float64 {
		if v, ok := p[k]; ok {
			return v
		}
		return d
	}
	return &EMACross{
		symbol: symbol, timeframe: timeframe,
		fast: 9, slow: 21, adxPeriod: 14, atrPeriod: 14,
		adxMin:       get("adx_min", 25),
		slATRMult:    get("sl_atr_mult", 2.0),
		tpRewardMult: get("tp_reward_mult", 1.6),
	}
}

func (s *EMACross) Name() string      { return "ema_cross_trend" }
func (s *EMACross) Symbol() string    { return s.symbol }
func (s *EMACross) Timeframe() string { return s.timeframe }

// Warmup is the longest indicator window: ADX needs 2*period candles.
func (s *EMACross) Warmup() int { return 2*s.adxPeriod + 1 }

func (s *EMACross) Evaluate(h []domain.Candle, inPosition bool) *domain.Signal {
	if len(h) < s.Warmup() {
		return nil
	}
	closes := make([]float64, len(h))
	for i, c := range h {
		closes[i] = c.Close
	}
	emaF := indicators.EMA(closes, s.fast)
	emaS := indicators.EMA(closes, s.slow)
	adx := indicators.ADX(h, s.adxPeriod)
	atr := indicators.ATR(h, s.atrPeriod)
	i := len(h) - 1
	if math.IsNaN(emaF[i]) || math.IsNaN(emaF[i-1]) || math.IsNaN(emaS[i]) || math.IsNaN(emaS[i-1]) || math.IsNaN(adx[i]) || math.IsNaN(atr[i]) {
		return nil
	}
	crossUp := emaF[i] > emaS[i]
	crossDown := emaF[i] < emaS[i]
	now := h[i]
	if !inPosition && crossUp && adx[i] >= s.adxMin {
		stop := s.slATRMult * atr[i]
		return &domain.Signal{
			Symbol: s.symbol, Action: domain.Buy, Time: now.CloseTime,
			StopDist: stop, TPDist: s.tpRewardMult * stop,
			Reason: fmt.Sprintf("EMA9>EMA21 cross + ADX %.1f>=%.0f", adx[i], s.adxMin),
		}
	}
	if inPosition && crossDown {
		return &domain.Signal{
			Symbol: s.symbol, Action: domain.Sell, Time: now.CloseTime,
			Reason: "EMA9<EMA21 recross exit",
		}
	}
	return nil
}
