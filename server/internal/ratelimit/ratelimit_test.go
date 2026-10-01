package ratelimit

import (
	"testing"
	"time"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time          { return c.t }
func (c *clock) advance(d time.Duration) { c.t = c.t.Add(d) }

func TestBurstThenRefill(t *testing.T) {
	c := &clock{t: time.Unix(0, 0)}
	l := newWithClock(60, 3, c.now) // 1 token per minute, burst 3

	for i := range 3 {
		if ok, _ := l.Allow("a"); !ok {
			t.Fatalf("request %d within burst was refused", i)
		}
	}
	ok, wait := l.Allow("a")
	if ok || wait != time.Minute {
		t.Fatalf("4th request: ok=%v wait=%v, want refused with 1m", ok, wait)
	}

	c.advance(30 * time.Second)
	if ok, wait := l.Allow("a"); ok || wait != 30*time.Second {
		t.Fatalf("half refilled: ok=%v wait=%v, want refused with 30s", ok, wait)
	}
	c.advance(30 * time.Second)
	if ok, _ := l.Allow("a"); !ok {
		t.Fatal("refused after a full refill interval")
	}
}

func TestKeysAreIndependent(t *testing.T) {
	c := &clock{t: time.Unix(0, 0)}
	l := newWithClock(60, 1, c.now)
	if ok, _ := l.Allow("a"); !ok {
		t.Fatal("a refused")
	}
	if ok, _ := l.Allow("b"); !ok {
		t.Fatal("b refused because of a")
	}
	if ok, _ := l.Allow("a"); ok {
		t.Fatal("a allowed beyond burst")
	}
}

func TestRefillIsCappedAtBurst(t *testing.T) {
	c := &clock{t: time.Unix(0, 0)}
	l := newWithClock(60, 2, c.now)
	c.advance(24 * time.Hour)
	for range 2 {
		if ok, _ := l.Allow("a"); !ok {
			t.Fatal("refused within burst")
		}
	}
	if ok, _ := l.Allow("a"); ok {
		t.Fatal("idle time accumulated beyond burst")
	}
}

func TestIdleBucketsAreSwept(t *testing.T) {
	c := &clock{t: time.Unix(0, 0)}
	l := newWithClock(60, 2, c.now)
	l.Allow("a")
	l.Allow("b")
	l.Allow("b")
	c.advance(90 * time.Second) // a is full again; b has 1.5 tokens
	l.Allow("c")                // triggers the sweep
	if l.Len() != 2 {
		t.Fatalf("tracked keys = %d, want 2 (b and c)", l.Len())
	}
}
