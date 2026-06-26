package telegram

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestTelegramNotifierAskApprovalSendsButtons(t *testing.T) {
	var mu sync.Mutex
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		mu.Lock(); body = r.Form.Get("reply_markup"); mu.Unlock()
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	c := NewClient("tok").WithHTTP(srv.Client(), srv.URL)
	n := NewTelegramNotifier(c, 42)
	if err := n.AskApproval("tok7", "BUY 0.01 BTC"); err != nil { t.Fatalf("ask: %v", err) }
	mu.Lock(); defer mu.Unlock()
	if !strings.Contains(body, "approve:tok7") || !strings.Contains(body, "reject:tok7") {
		t.Fatalf("approval buttons missing: %s", body)
	}
}

func TestNoopNotifier(t *testing.T) {
	var n Notifier = NoopNotifier{}
	if err := n.Info("x"); err != nil { t.Fatal(err) }
	if err := n.AskApproval("t", "x"); err != nil { t.Fatal(err) }
}
