package regime

import (
	"math"

	"tradebot/internal/domain"
	"tradebot/internal/indicators"
)

type Regime int

const (
	Unknown Regime = iota
	TrendingUp
	TrendingDown
	Ranging
	HighVol
	LowVolChop
)

func (r Regime) String() string {
	switch r {
	case TrendingUp:
		return "TrendingUp"
	case TrendingDown:
		return "TrendingDown"
	case Ranging:
		return "Ranging"
	case HighVol:
		return "HighVol"
	case LowVolChop:
		return "LowVolChop"
	default:
		return "Unknown"
	}
}

const (
	lowVolBW  = 0.015 // normalized BB width below this = low volatility
	highVolBW = 0.08  // normalized BB width above this = high volatility
)

// Classify labels the market regime from the closed-candle history.
func Classify(h []domain.Candle) Regime {
	if len(h) < 60 {
		return Unknown
	}
	closes := make([]float64, len(h))
	for i, c := range h {
		closes[i] = c.Close
	}
	adx := indicators.ADX(h, 14)
	mid, up, lo := indicators.Bollinger(closes, 20, 2.0)
	sma := indicators.SMA(closes, 50)
	i := len(h) - 1
	if math.IsNaN(adx[i]) || math.IsNaN(mid[i]) || math.IsNaN(sma[i]) || math.IsNaN(up[i]) || math.IsNaN(lo[i]) || mid[i] == 0 {
		return Unknown
	}
	bw := (up[i] - lo[i]) / mid[i]
	switch {
	case adx[i] < 20 && bw < lowVolBW:
		return LowVolChop
	case adx[i] >= 25:
		if closes[i] > sma[i] {
			return TrendingUp
		}
		return TrendingDown
	case bw > highVolBW:
		return HighVol
	default:
		return Ranging
	}
}
