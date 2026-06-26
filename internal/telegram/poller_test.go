package telegram

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"tradebot/internal/control"
	"tradebot/internal/domain"
)

func orderStub() domain.Order { return domain.Order{Symbol: "BTCUSDT", Side: domain.Buy, Qty: 0.01} }

func TestPollAppliesPauseAndApproval(t *testing.T) {
	var served int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&served, 1) == 1 {
			w.Write([]byte(`{"ok":true,"result":[
			  {"update_id":1,"message":{"text":"/pause","chat":{"id":7}}},
			  {"update_id":2,"callback_query":{"id":"cb","data":"approve:1","message":{"chat":{"id":7}}}}
			]}`))
			return
		}
		w.Write([]byte(`{"ok":true,"result":[]}`)) // subsequent polls empty
	}))
	defer srv.Close()
	c := NewClient("tok").WithHTTP(srv.Client(), srv.URL)
	ctrl := control.New()
	tok := ctrl.RequestApproval(orderStub()) // token "1"
	_ = tok
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go Poll(ctx, c, ctrl, func() string { return "ok" }, func() string { return "none" })
	deadline := time.After(800 * time.Millisecond)
	for {
		select {
		case <-deadline:
			t.Fatalf("controller did not reflect updates: paused=%v approved=%d", ctrl.Paused(), len(ctrl.DrainApproved()))
		default:
			if ctrl.Paused() {
				if len(ctrl.DrainApproved()) == 1 {
					return // success: pause applied AND approval token 1 accepted
				}
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
}
