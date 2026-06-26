package marketdata

import "tradebot/internal/domain"

// Buffer keeps a rolling, ascending candle history per symbol, trimmed to maxLen.
type Buffer struct {
	maxLen int
	hist   map[string][]domain.Candle
}

func NewBuffer(maxLen int) *Buffer {
	return &Buffer{maxLen: maxLen, hist: map[string][]domain.Candle{}}
}

func (b *Buffer) Add(c domain.Candle) []domain.Candle {
	h := append(b.hist[c.Symbol], c)
	if len(h) > b.maxLen {
		h = h[len(h)-b.maxLen:]
	}
	b.hist[c.Symbol] = h
	return h
}

func (b *Buffer) History(symbol string) []domain.Candle { return b.hist[symbol] }

// Seed preloads a symbol's history (e.g. from REST warmup), trimmed to maxLen.
func (b *Buffer) Seed(symbol string, candles []domain.Candle) {
	h := candles
	if len(h) > b.maxLen {
		h = h[len(h)-b.maxLen:]
	}
	b.hist[symbol] = append([]domain.Candle(nil), h...)
}
