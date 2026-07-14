package scheduler

import (
	"sync"
	"time"
)

// Ticker is a reusable time-driven signal that can be stopped.
type Ticker interface {
	Chan() <-chan time.Time
	Stop()
}

// Clock abstracts time for the scheduler so tests can control timing.
type Clock interface {
	Now() time.Time
	After(time.Duration) <-chan time.Time
	NewTicker(time.Duration) Ticker
}

// RealClock uses the wall clock.
type RealClock struct{}

func NewRealClock() Clock { return RealClock{} }

func (RealClock) Now() time.Time { return time.Now() }

func (RealClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

func (RealClock) NewTicker(d time.Duration) Ticker { return &realTicker{time.NewTicker(d)} }

type realTicker struct{ t *time.Ticker }

func (t *realTicker) Chan() <-chan time.Time { return t.t.C }
func (t *realTicker) Stop()                  { t.t.Stop() }

// FakeClock is a test-only clock that can be advanced manually.
// It is safe for concurrent use by the worker and tests.
type FakeClock struct {
	mu      sync.Mutex
	now     time.Time
	tickers []*fakeTicker
}

func NewFakeClock(now time.Time) *FakeClock {
	return &FakeClock{now: now}
}

func (c *FakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *FakeClock) After(time.Duration) <-chan time.Time {
	// Tests drive the worker by calling Wake and Advance; this channel never fires.
	return make(<-chan time.Time)
}

func (c *FakeClock) NewTicker(d time.Duration) Ticker {
	c.mu.Lock()
	t := &fakeTicker{
		clock:  c,
		period: d,
		next:   c.now.Add(d),
		C:      make(chan time.Time, 1),
	}
	c.tickers = append(c.tickers, t)
	c.mu.Unlock()
	return t
}

func (c *FakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	now := c.now
	tickers := make([]*fakeTicker, len(c.tickers))
	copy(tickers, c.tickers)
	c.mu.Unlock()

	for _, t := range tickers {
		t.advance(now)
	}
}

type fakeTicker struct {
	clock  *FakeClock
	period time.Duration
	next   time.Time
	C      chan time.Time

	mu      sync.Mutex
	stopped bool
}

func (t *fakeTicker) Chan() <-chan time.Time { return t.C }

func (t *fakeTicker) Stop() {
	t.mu.Lock()
	t.stopped = true
	t.mu.Unlock()
}

func (t *fakeTicker) advance(now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.stopped {
		return
	}
	for !t.next.After(now) {
		select {
		case t.C <- t.next:
		default:
		}
		t.next = t.next.Add(t.period)
	}
}
