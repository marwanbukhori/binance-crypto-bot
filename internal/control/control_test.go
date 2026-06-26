package control

import (
	"fmt"
	"sync"
	"testing"

	"tradebot/internal/domain"
)

func TestPauseResume(t *testing.T) {
	c := New()
	if c.Paused() { t.Fatal("starts unpaused") }
	c.Pause(); if !c.Paused() { t.Fatal("should be paused") }
	c.Resume(); if c.Paused() { t.Fatal("should be resumed") }
}

func TestKillIsOneShot(t *testing.T) {
	c := New()
	if c.KillRequested() { t.Fatal("no kill yet") }
	c.RequestKill()
	if !c.KillRequested() { t.Fatal("kill must report once") }
	if c.KillRequested() { t.Fatal("kill must be consumed (one-shot)") }
}

func TestApprovalLifecycle(t *testing.T) {
	c := New()
	tok := c.RequestApproval(domain.Order{Symbol: "BTCUSDT", Side: domain.Buy, Qty: 0.01})
	if tok == "" { t.Fatal("token must be non-empty") }
	if len(c.DrainApproved()) != 0 { t.Fatal("nothing approved yet") }
	c.Approve(tok)
	got := c.DrainApproved()
	if len(got) != 1 || got[0].Symbol != "BTCUSDT" { t.Fatalf("expected the approved order, got %+v", got) }
	if len(c.DrainApproved()) != 0 { t.Fatal("drain must clear") }
}

func TestRejectDropsPending(t *testing.T) {
	c := New()
	tok := c.RequestApproval(domain.Order{Symbol: "ETHUSDT"})
	c.Reject(tok)
	c.Approve(tok) // approving a rejected/unknown token is a no-op
	if len(c.DrainApproved()) != 0 { t.Fatal("rejected order must not execute") }
}

func TestConcurrentSafe(t *testing.T) {
	c := New()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); tok := c.RequestApproval(domain.Order{}); c.Approve(tok) }()
	}
	wg.Wait()
	if len(c.DrainApproved()) != 50 { t.Fatalf("want 50 approved, got %d", len(c.DrainApproved())) }
}

func TestStatusRoundTrip(t *testing.T) {
	c := New()
	if got := c.Status(); got != "no status yet" {
		t.Fatalf("default status: want %q, got %q", "no status yet", got)
	}
	c.SetStatus("equity 1000.00 | realized 50.00 | BTC:0.01")
	if got := c.Status(); got != "equity 1000.00 | realized 50.00 | BTC:0.01" {
		t.Fatalf("unexpected status: %q", got)
	}
}

func TestStatusConcurrentSafe(t *testing.T) {
	c := New()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func(n int) { defer wg.Done(); c.SetStatus(fmt.Sprintf("snap %d", n)) }(i)
		go func() { defer wg.Done(); _ = c.Status() }()
	}
	wg.Wait()
}
