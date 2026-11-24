package links

import (
	"sync"
	"time"
)

type ipRateLimiter struct {
	mu     sync.Mutex
	counts map[string]int
	resets map[string]time.Time
	limit  int
	window time.Duration
}

func newIPRateLimiter(limit int, window time.Duration) *ipRateLimiter {
	return &ipRateLimiter{
		counts: make(map[string]int),
		resets: make(map[string]time.Time),
		limit:  limit,
		window: window,
	}
}

func (l *ipRateLimiter) Allow(ip string) bool {
	if l == nil || l.limit <= 0 {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	reset, ok := l.resets[ip]
	if !ok || now.After(reset) {
		l.counts[ip] = 1
		l.resets[ip] = now.Add(l.window)
		return true
	}

	if l.counts[ip] >= l.limit {
		return false
	}
	l.counts[ip]++
	return true
}
