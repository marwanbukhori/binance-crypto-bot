package risk

// Cooldown blocks new entries on a symbol for N closed candles after a losing exit.
type Cooldown struct {
	n        int
	lastLoss map[string]int
}

func NewCooldown(candles int) *Cooldown {
	return &Cooldown{n: candles, lastLoss: map[string]int{}}
}

func (c *Cooldown) NoteLoss(symbol string, idx int) { c.lastLoss[symbol] = idx }

// Blocked is true while idx is within n candles after the last loss (inclusive).
func (c *Cooldown) Blocked(symbol string, idx int) bool {
	last, ok := c.lastLoss[symbol]
	if !ok {
		return false
	}
	return idx-last <= c.n && idx > last
}
