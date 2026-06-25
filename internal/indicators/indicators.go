package indicators

import "math"

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
