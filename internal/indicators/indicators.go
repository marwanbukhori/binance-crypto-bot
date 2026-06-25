package indicators

import (
	"math"
	"tradebot/internal/domain"
)

// SMA returns the simple moving average; indices < period-1 are NaN.
func SMA(vals []float64, period int) []float64 {
	out := make([]float64, len(vals))
	for i := range out {
		out[i] = math.NaN()
	}
	if period <= 0 {
		return out
	}
	var sum float64
	for i, v := range vals {
		sum += v
		if i >= period {
			sum -= vals[i-period]
		}
		if i >= period-1 {
			out[i] = sum / float64(period)
		}
	}
	return out
}

// EMA seeds at index period-1 with the SMA of the first `period` values, then
// applies multiplier 2/(period+1). Indices < period-1 are NaN.
func EMA(vals []float64, period int) []float64 {
	out := make([]float64, len(vals))
	for i := range out {
		out[i] = math.NaN()
	}
	if period <= 0 || len(vals) < period {
		return out
	}
	k := 2.0 / float64(period+1)
	var seed float64
	for i := 0; i < period; i++ {
		seed += vals[i]
	}
	prev := seed / float64(period)
	out[period-1] = prev
	for i := period; i < len(vals); i++ {
		prev = vals[i]*k + prev*(1-k)
		out[i] = prev
	}
	return out
}

func trueRange(c, prev domain.Candle) float64 {
	hl := c.High - c.Low
	hc := math.Abs(c.High - prev.Close)
	lc := math.Abs(c.Low - prev.Close)
	return math.Max(hl, math.Max(hc, lc))
}

// ATR is Wilder's Average True Range. Indices < period are NaN (needs a prior candle).
func ATR(cs []domain.Candle, period int) []float64 {
	out := make([]float64, len(cs))
	for i := range out {
		out[i] = math.NaN()
	}
	if period <= 0 || len(cs) <= period {
		return out
	}
	var sum float64
	for i := 1; i <= period; i++ {
		sum += trueRange(cs[i], cs[i-1])
	}
	prev := sum / float64(period)
	out[period] = prev
	for i := period + 1; i < len(cs); i++ {
		tr := trueRange(cs[i], cs[i-1])
		prev = (prev*float64(period-1) + tr) / float64(period)
		out[i] = prev
	}
	return out
}

// RSI is Wilder's Relative Strength Index in [0,100]; indices < period are NaN.
func RSI(vals []float64, period int) []float64 {
	out := make([]float64, len(vals))
	for i := range out {
		out[i] = math.NaN()
	}
	if period <= 0 || len(vals) <= period {
		return out
	}
	var gain, loss float64
	for i := 1; i <= period; i++ {
		ch := vals[i] - vals[i-1]
		if ch >= 0 {
			gain += ch
		} else {
			loss -= ch
		}
	}
	avgGain := gain / float64(period)
	avgLoss := loss / float64(period)
	rsi := func(g, l float64) float64 {
		if l == 0 {
			return 100
		}
		rs := g / l
		return 100 - 100/(1+rs)
	}
	out[period] = rsi(avgGain, avgLoss)
	for i := period + 1; i < len(vals); i++ {
		ch := vals[i] - vals[i-1]
		g, l := 0.0, 0.0
		if ch >= 0 {
			g = ch
		} else {
			l = -ch
		}
		avgGain = (avgGain*float64(period-1) + g) / float64(period)
		avgLoss = (avgLoss*float64(period-1) + l) / float64(period)
		out[i] = rsi(avgGain, avgLoss)
	}
	return out
}

// ADX is Wilder's Average Directional Index in [0,100]. Pre-warm-up positions are NaN.
func ADX(cs []domain.Candle, period int) []float64 {
	out := make([]float64, len(cs))
	for i := range out {
		out[i] = math.NaN()
	}
	n := len(cs)
	if period <= 0 || n <= 2*period {
		return out
	}
	plusDM := make([]float64, n)
	minusDM := make([]float64, n)
	tr := make([]float64, n)
	for i := 1; i < n; i++ {
		up := cs[i].High - cs[i-1].High
		down := cs[i-1].Low - cs[i].Low
		if up > down && up > 0 {
			plusDM[i] = up
		}
		if down > up && down > 0 {
			minusDM[i] = down
		}
		tr[i] = trueRange(cs[i], cs[i-1])
	}
	// Wilder-smoothed sums over `period`, seeded at index `period`.
	var sTR, sP, sM float64
	for i := 1; i <= period; i++ {
		sTR += tr[i]; sP += plusDM[i]; sM += minusDM[i]
	}
	dx := make([]float64, n)
	calcDX := func(sTR, sP, sM float64) float64 {
		if sTR == 0 {
			return 0
		}
		pDI := 100 * sP / sTR
		mDI := 100 * sM / sTR
		if pDI+mDI == 0 {
			return 0
		}
		return 100 * math.Abs(pDI-mDI) / (pDI + mDI)
	}
	dx[period] = calcDX(sTR, sP, sM)
	for i := period + 1; i < n; i++ {
		sTR = sTR - sTR/float64(period) + tr[i]
		sP = sP - sP/float64(period) + plusDM[i]
		sM = sM - sM/float64(period) + minusDM[i]
		dx[i] = calcDX(sTR, sP, sM)
	}
	// ADX = Wilder average of DX over `period`, first value at index 2*period.
	var sumDX float64
	for i := period; i < 2*period; i++ {
		sumDX += dx[i]
	}
	adx := sumDX / float64(period)
	out[2*period-1] = adx
	for i := 2 * period; i < n; i++ {
		adx = (adx*float64(period-1) + dx[i]) / float64(period)
		out[i] = adx
	}
	return out
}
