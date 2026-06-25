package domain

import "testing"

func TestActionString(t *testing.T) {
	cases := map[Action]string{Hold: "HOLD", Buy: "BUY", Sell: "SELL"}
	for a, want := range cases {
		if got := a.String(); got != want {
			t.Errorf("Action(%d).String() = %q, want %q", a, got, want)
		}
	}
}

func TestPositionZeroValue(t *testing.T) {
	var p Position
	if p.Qty != 0 || p.AvgEntry != 0 {
		t.Fatal("zero Position must have zero Qty and AvgEntry")
	}
}
