// Package ratelimit is a small per-key token-bucket limiter.
package ratelimit

import (
	"sync"
	"time"
)

// sweepEvery bounds how often idle buckets are pruned, keeping memory
// proportional to recently active keys.
const sweepEvery = time.Minute

// Limiter allows Burst events per key at once, refilled at Rate per second.
type Limiter struct {
	rate  float64
	burst float64
	now   func() time.Time

	mu        sync.Mutex
	buckets   map[string]*bucket
	lastSweep time.Time
}

type bucket struct {
	tokens float64
	last   time.Time
}

// New returns a limiter allowing burst events per key, refilling perHour
// tokens per hour.
func New(perHour, burst int) *Limiter {
	return newWithClock(perHour, burst, time.Now)
}

func newWithClock(perHour, burst int, now func() time.Time) *Limiter {
	return &Limiter{
		rate:      float64(perHour) / 3600,
		burst:     float64(burst),
		now:       now,
		buckets:   make(map[string]*bucket),
		lastSweep: now(),
	}
}

// Allow consumes a token for key. If none is available it returns false and
// how long until one will be.
func (l *Limiter) Allow(key string) (ok bool, retryAfter time.Duration) {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()

	if now.Sub(l.lastSweep) >= sweepEvery {
		l.sweep(now)
	}

	b, found := l.buckets[key]
	if !found {
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}
	b.tokens = min(l.burst, b.tokens+now.Sub(b.last).Seconds()*l.rate)
	b.last = now

	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	wait := time.Duration((1 - b.tokens) / l.rate * float64(time.Second))
	return false, wait
}

// sweep forgets buckets that would be full again by now: they behave exactly
// like a key never seen before.
func (l *Limiter) sweep(now time.Time) {
	for key, b := range l.buckets {
		if b.tokens+now.Sub(b.last).Seconds()*l.rate >= l.burst {
			delete(l.buckets, key)
		}
	}
	l.lastSweep = now
}

// Len reports how many keys are being tracked.
func (l *Limiter) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}
