package decision

import (
	"tradebot/internal/domain"
	"tradebot/internal/regime"
)

// KindEnabled reports whether a strategy of the given kind may OPEN a position in the regime.
// In LowVolChop only mean-reversion is allowed; trend/breakout/momentum are disabled there.
func KindEnabled(r regime.Regime, kind string) bool {
	if r == regime.LowVolChop {
		return kind == "reversion"
	}
	return true
}

type Candidate struct {
	Kind   string
	Signal domain.Signal
}

// Choose selects one action for a symbol. Exits are always allowed (any regime).
// Entries are gated by regime and blocked entirely while already in a position
// (same-symbol netting: never stack a second long).
func Choose(r regime.Regime, inPosition bool, cands []Candidate) *domain.Signal {
	if inPosition {
		for _, c := range cands {
			if c.Signal.Action == domain.Sell {
				s := c.Signal
				return &s
			}
		}
		return nil
	}
	for _, c := range cands {
		if c.Signal.Action == domain.Buy && KindEnabled(r, c.Kind) {
			s := c.Signal
			return &s
		}
	}
	return nil
}
