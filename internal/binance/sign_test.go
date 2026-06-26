package binance

import "testing"

func TestSignKnownVector(t *testing.T) {
	secret := "NhqPtmdSJYdKjVHjA7PZj4Mge3R5YNiP1e3UZjInClVN65XAbvqqM6A7H5fATj0"
	query := "symbol=LTCBTC&side=BUY&type=LIMIT&timeInForce=GTC&quantity=1&price=0.1&recvWindow=5000&timestamp=1499827319559"
	want := "b89008e7051ffbf2242be7dc5ae67fd146e6430688627b802c0cbec146e46aef"
	if got := Sign(query, secret); got != want {
		t.Fatalf("Sign mismatch:\n got %s\nwant %s", got, want)
	}
}
