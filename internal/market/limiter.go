package market

import (
	"context"
	"time"
)

type rateLimiter struct {
	tokens chan struct{}
}

func newRateLimiter(rps int, burst int) *rateLimiter {
	if rps <= 0 {
		rps = 1
	}
	if burst <= 0 {
		burst = 1
	}
	limiter := &rateLimiter{
		tokens: make(chan struct{}, burst),
	}
	for i := 0; i < burst; i++ {
		limiter.tokens <- struct{}{}
	}
	interval := time.Second / time.Duration(rps)
	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			select {
			case limiter.tokens <- struct{}{}:
			default:
			}
		}
	}()
	return limiter
}

func (l *rateLimiter) Wait(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-l.tokens:
		return nil
	}
}
