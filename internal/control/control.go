package control

import (
	"strconv"
	"sync"

	"tradebot/internal/domain"
)

type Controller struct {
	mu         sync.Mutex
	paused     bool
	killReq    bool
	seq        int64
	pending    map[string]domain.Order
	approved   []domain.Order
	statusSnap string
}

func New() *Controller {
	return &Controller{pending: map[string]domain.Order{}}
}

func (c *Controller) Pause()  { c.mu.Lock(); c.paused = true; c.mu.Unlock() }
func (c *Controller) Resume() { c.mu.Lock(); c.paused = false; c.mu.Unlock() }

func (c *Controller) Paused() bool {
	c.mu.Lock(); defer c.mu.Unlock()
	return c.paused
}

func (c *Controller) RequestKill() { c.mu.Lock(); c.killReq = true; c.mu.Unlock() }

func (c *Controller) KillRequested() bool {
	c.mu.Lock(); defer c.mu.Unlock()
	k := c.killReq
	c.killReq = false
	return k
}

func (c *Controller) RequestApproval(o domain.Order) string {
	c.mu.Lock(); defer c.mu.Unlock()
	c.seq++
	tok := strconv.FormatInt(c.seq, 10)
	c.pending[tok] = o
	return tok
}

func (c *Controller) Approve(token string) {
	c.mu.Lock(); defer c.mu.Unlock()
	if o, ok := c.pending[token]; ok {
		c.approved = append(c.approved, o)
		delete(c.pending, token)
	}
}

func (c *Controller) Reject(token string) {
	c.mu.Lock(); defer c.mu.Unlock()
	delete(c.pending, token)
}

func (c *Controller) DrainApproved() []domain.Order {
	c.mu.Lock(); defer c.mu.Unlock()
	out := c.approved
	c.approved = nil
	return out
}

// SetStatus stores a snapshot string produced by the engine goroutine so the
// poller goroutine can read portfolio state without touching engine-owned data.
func (c *Controller) SetStatus(s string) {
	c.mu.Lock(); c.statusSnap = s; c.mu.Unlock()
}

// Status returns the last snapshot set by the engine goroutine.
// Returns "no status yet" when no snapshot has been published.
func (c *Controller) Status() string {
	c.mu.Lock(); defer c.mu.Unlock()
	if c.statusSnap == "" {
		return "no status yet"
	}
	return c.statusSnap
}
