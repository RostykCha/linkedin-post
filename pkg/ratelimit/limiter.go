package ratelimit

import (
	"context"
	"fmt"
	"sync"

	"golang.org/x/time/rate"
)

// MultiLimiter manages multiple rate limiters for different services
type MultiLimiter struct {
	limiters map[string]*rate.Limiter
	mu       sync.RWMutex
}

// NewMultiLimiter creates a new multi-limiter
func NewMultiLimiter() *MultiLimiter {
	return &MultiLimiter{
		limiters: make(map[string]*rate.Limiter),
	}
}

// AddLimiter adds a new rate limiter for a service
// requestsPerSecond: the rate limit (e.g., 10 means 10 requests per second)
// burst: maximum burst size
func (m *MultiLimiter) AddLimiter(name string, requestsPerSecond float64, burst int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.limiters[name] = rate.NewLimiter(rate.Limit(requestsPerSecond), burst)
}

// Wait blocks until the limiter allows an event
func (m *MultiLimiter) Wait(ctx context.Context, name string) error {
	m.mu.RLock()
	limiter, ok := m.limiters[name]
	m.mu.RUnlock()

	if !ok {
		return fmt.Errorf("limiter %s not found", name)
	}

	return limiter.Wait(ctx)
}

// Allow reports whether an event may happen now
func (m *MultiLimiter) Allow(name string) bool {
	m.mu.RLock()
	limiter, ok := m.limiters[name]
	m.mu.RUnlock()

	if !ok {
		return false
	}

	return limiter.Allow()
}

// Reserve returns a reservation for a future event
func (m *MultiLimiter) Reserve(name string) (*rate.Reservation, error) {
	m.mu.RLock()
	limiter, ok := m.limiters[name]
	m.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("limiter %s not found", name)
	}

	return limiter.Reserve(), nil
}

// Default rate limiter names
const (
	LimiterLinkedIn  = "linkedin"
	LimiterAnthropic = "anthropic"
	LimiterNewsAPI   = "newsapi"
	LimiterTwitter   = "twitter"
	LimiterReddit    = "reddit"
	LimiterRSS       = "rss"
)

// NewDefaultLimiter creates a limiter with default rate limits
func NewDefaultLimiter() *MultiLimiter {
	m := NewMultiLimiter()

	// LinkedIn: 100 requests per day = ~0.0012 per second, burst 5
	m.AddLimiter(LimiterLinkedIn, 100.0/(24*60*60), 5)

	// Anthropic: 10 requests per minute = ~0.17 per second, burst 2
	m.AddLimiter(LimiterAnthropic, 10.0/60, 2)

	// NewsAPI: 100 requests per day = ~0.0012 per second, burst 10
	m.AddLimiter(LimiterNewsAPI, 100.0/(24*60*60), 10)

	// Twitter: 300 requests per 15 min = ~0.33 per second, burst 50
	m.AddLimiter(LimiterTwitter, 300.0/(15*60), 50)

	// Reddit: 60 requests per minute = 1 per second, burst 10
	m.AddLimiter(LimiterReddit, 1, 10)

	// RSS: No strict limit, but be polite - 1 per second, burst 10
	m.AddLimiter(LimiterRSS, 1, 10)

	return m
}
