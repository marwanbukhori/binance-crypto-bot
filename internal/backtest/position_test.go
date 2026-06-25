package backtest

import (
	"testing"

	"tradebot/internal/domain"
)

func longOrder() domain.Order {
	return domain.Order{Side: domain.Buy, Price: 100, StopPrice: 98, TPPrice: 104}
}

func TestStopOnly(t *testing.T) {
	k, px := CheckExit(longOrder(), domain.Candle{High: 101, Low: 97, Close: 99})
	if k != ExitStop || px != 98 {
		t.Fatalf("got %v px=%v want ExitStop@98", k, px)
	}
}

func TestTPOnly(t *testing.T) {
	k, px := CheckExit(longOrder(), domain.Candle{High: 105, Low: 99, Close: 104})
	if k != ExitTP || px != 104 {
		t.Fatalf("got %v px=%v want ExitTP@104", k, px)
	}
}

func TestStraddleAssumesStopFirst(t *testing.T) {
	// Candle hits BOTH stop (98) and TP (104). Pessimistic => stop.
	k, px := CheckExit(longOrder(), domain.Candle{High: 105, Low: 97, Close: 102})
	if k != ExitStop || px != 98 {
		t.Fatalf("straddle got %v px=%v want ExitStop@98", k, px)
	}
}

func TestNoExit(t *testing.T) {
	k, _ := CheckExit(longOrder(), domain.Candle{High: 103, Low: 99, Close: 101})
	if k != NoExit {
		t.Fatalf("got %v want NoExit", k)
	}
}
