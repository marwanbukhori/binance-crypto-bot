package strategy

import (
	"testing"

	"tradebot/internal/domain"
)

func TestRSIBBKindAndWarmup(t *testing.T) {
	s := NewRSIBBReversion("BTCUSDT", "1h", nil)
	if s.Kind() != "reversion" || s.Name() != "rsi_bb_reversion" {
		t.Fatalf("bad identity: %s/%s", s.Name(), s.Kind())
	}
	if s.Warmup() < 200 {
		t.Fatalf("warmup must cover SMA200, got %d", s.Warmup())
	}
}

func TestRSIBBNoSignalBeforeWarmup(t *testing.T) {
	s := NewRSIBBReversion("BTCUSDT", "1h", nil)
	cs := make([]domain.Candle, 50)
	for i := range cs { cs[i] = domain.Candle{Close: 100, High: 101, Low: 99, Closed: true} }
	if sig := s.Evaluate(cs, false); sig != nil {
		t.Fatalf("expected nil before warmup, got %+v", sig)
	}
}

func TestRSIBBBuysOversoldInRange(t *testing.T) {
	s := NewRSIBBReversion("BTCUSDT", "1h", nil)
	// 220 candles: 100 oscillating at ~200, then 110 oscillating at ~300 so SMA200
	// settles around 250, keeping ADX low (range-bound). A sharp dip from ~300 toward
	// ~260 pushes RSI oversold and close below the lower Bollinger band, while price
	// remains well above SMA200 (~255) and ADX stays below 25.
	cs := make([]domain.Candle, 0, 260)
	// First 100 candles: oscillate at ~200 (seeds SMA200 lower half)
	for i := 0; i < 100; i++ {
		c := 200.0 + float64(i%4)*0.5 - 1.0
		cs = append(cs, domain.Candle{Symbol: "BTCUSDT", Close: c, High: c + 1, Low: c - 1, Closed: true, CloseTime: int64(i)})
	}
	// Next 110 candles: oscillate at ~300 (SMA200 ends up ~250, ADX stays low)
	for i := 0; i < 110; i++ {
		c := 300.0 + float64(i%4)*0.5 - 1.0
		cs = append(cs, domain.Candle{Symbol: "BTCUSDT", Close: c, High: c + 1, Low: c - 1, Closed: true, CloseTime: int64(100 + i)})
	}
	// Sharp dip (oversold) over 10 candles: close falls from ~299 to ~259
	price := 299.0
	for i := 0; i < 10; i++ {
		price -= 4
		cs = append(cs, domain.Candle{Symbol: "BTCUSDT", Close: price, High: price + 1, Low: price - 1, Closed: true, CloseTime: int64(210 + i)})
	}
	var got *domain.Signal
	for i := s.Warmup(); i <= len(cs); i++ {
		if sig := s.Evaluate(cs[:i], false); sig != nil && sig.Action == domain.Buy {
			got = sig
			break
		}
	}
	if got == nil {
		t.Fatal("expected an oversold BUY while price is still above SMA200")
	}
	if got.StopDist <= 0 {
		t.Fatalf("BUY must carry an ATR stop, got %v", got.StopDist)
	}
}
