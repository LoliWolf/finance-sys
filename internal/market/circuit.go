package market

import (
	"sync"
	"time"
)

type circuitBreaker struct {
	mu               sync.Mutex
	failures         int
	failureThreshold int
	halfOpenAfter    time.Duration
	openedAt         time.Time
}

func newCircuitBreaker(threshold int, halfOpenAfter time.Duration) *circuitBreaker {
	return &circuitBreaker{
		failureThreshold: threshold,
		halfOpenAfter:    halfOpenAfter,
	}
}

func (c *circuitBreaker) Allow() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.failures < c.failureThreshold {
		return true
	}
	if time.Since(c.openedAt) >= c.halfOpenAfter {
		return true
	}
	return false
}

func (c *circuitBreaker) Success() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.failures = 0
	c.openedAt = time.Time{}
}

func (c *circuitBreaker) Failure() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.failures++
	if c.failures >= c.failureThreshold {
		c.openedAt = time.Now()
	}
}
