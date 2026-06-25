package risk

import "testing"

func TestCooldownBlocksNCandles(t *testing.T) {
	c := NewCooldown(2)
	if c.Blocked("BTCUSDT", 10) {
		t.Fatal("no loss recorded yet -> not blocked")
	}
	c.NoteLoss("BTCUSDT", 10)
	if !c.Blocked("BTCUSDT", 11) { t.Fatal("idx 11 within 2 candles of loss@10 -> blocked") }
	if !c.Blocked("BTCUSDT", 12) { t.Fatal("idx 12: 12-10=2, still < ...? boundary") }
	if c.Blocked("BTCUSDT", 13) { t.Fatal("idx 13: 13-10=3 >= 2... not blocked") }
}

func TestCooldownPerSymbol(t *testing.T) {
	c := NewCooldown(2)
	c.NoteLoss("BTCUSDT", 5)
	if c.Blocked("ETHUSDT", 6) {
		t.Fatal("ETH has no loss -> not blocked")
	}
}
