package marketdata

import (
	"testing"

	"tradebot/internal/domain"
)

func TestBufferTrimsAndReturnsHistory(t *testing.T) {
	b := NewBuffer(3)
	for i := 0; i < 5; i++ {
		b.Add(domain.Candle{Symbol: "BTCUSDT", Close: float64(i), CloseTime: int64(i), Closed: true})
	}
	h := b.History("BTCUSDT")
	if len(h) != 3 { t.Fatalf("want trimmed to 3, got %d", len(h)) }
	if h[0].Close != 2 || h[2].Close != 4 { t.Fatalf("want newest 3 (2,3,4), got %v..%v", h[0].Close, h[2].Close) }
}

func TestBufferPerSymbol(t *testing.T) {
	b := NewBuffer(10)
	b.Add(domain.Candle{Symbol: "BTCUSDT", Close: 1})
	b.Add(domain.Candle{Symbol: "ETHUSDT", Close: 2})
	if len(b.History("BTCUSDT")) != 1 || len(b.History("ETHUSDT")) != 1 {
		t.Fatal("symbols must be isolated")
	}
}
