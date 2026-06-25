package domain

// Action is a trade direction. Spot is long-only: Sell only reduces a position to cash.
type Action int

const (
	Hold Action = iota
	Buy
	Sell
)

func (a Action) String() string {
	switch a {
	case Buy:
		return "BUY"
	case Sell:
		return "SELL"
	default:
		return "HOLD"
	}
}

// Candle is an OHLCV bar. Times are unix milliseconds.
type Candle struct {
	Symbol, Timeframe              string
	OpenTime                       int64
	Open, High, Low, Close, Volume float64
	CloseTime                      int64
	Closed                         bool
}

// Signal is a strategy's decision on a closed candle. StopDist/TPDist are price-distance hints (0 if none).
type Signal struct {
	Symbol   string
	Action   Action
	Reason   string
	StopDist float64
	TPDist   float64
	Time     int64
}

// Intent is a proposed action handed to the risk gate.
type Intent struct {
	Symbol   string
	Action   Action
	Reason   string
	Price    float64 // reference price (candle close)
	StopDist float64
	TPDist   float64
	Time     int64
}

// Order is a risk-approved instruction for the executor.
type Order struct {
	Symbol            string
	Side              Action
	Qty               float64
	Price             float64
	StopPrice         float64
	TPPrice           float64
	Type              string // "MARKET"
	Reason            string
	BindingConstraint string // "risk" | "notional" | "portfolio" | "free"
	EffectiveRiskPct  float64
	Time              int64
}

// Fill is the executor's report of an executed order.
type Fill struct {
	Symbol string
	Side   Action
	Qty    float64
	Price  float64
	Fee    float64
	Time   int64
}

// Position is the currently held base-asset amount and its average entry price.
type Position struct {
	Symbol   string
	Qty      float64
	AvgEntry float64
}
