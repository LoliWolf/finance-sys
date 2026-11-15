package service

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// ProcessingPools owns the process-wide OCR and LLM semaphores. Every
// document entry point reaches them through DocumentService.
type ProcessingPools struct {
	ocr     *concurrencyPool
	llm     *concurrencyPool
	llmRate *rateGate
}

// rateGate spaces out starts so at most qps() of them begin per second. qps is
// read on every call, so a Nacos change applies without a restart; <= 0 means
// unlimited.
type rateGate struct {
	qps  func() float64
	now  func() time.Time
	mu   sync.Mutex
	next time.Time
}

type concurrencyPool struct {
	name  string
	slots chan struct{}
}

func NewProcessingPools(ocrMaxConcurrency int, llmMaxConcurrency int) (*ProcessingPools, error) {
	ocr, err := newConcurrencyPool("ocr", ocrMaxConcurrency)
	if err != nil {
		return nil, err
	}
	llm, err := newConcurrencyPool("llm", llmMaxConcurrency)
	if err != nil {
		return nil, err
	}
	return &ProcessingPools{ocr: ocr, llm: llm}, nil
}

func newConcurrencyPool(name string, capacity int) (*concurrencyPool, error) {
	if capacity <= 0 {
		return nil, fmt.Errorf("%s concurrency must be positive", name)
	}
	return &concurrencyPool{name: name, slots: make(chan struct{}, capacity)}, nil
}

func (p *ProcessingPools) AcquireOCR(ctx context.Context) (func(), error) {
	if p == nil || p.ocr == nil {
		return func() {}, nil
	}
	return p.ocr.acquire(ctx)
}

// LimitLLMRate caps how many LLM operations may start per second. qps is
// consulted on every acquisition.
func (p *ProcessingPools) LimitLLMRate(qps func() float64) {
	if p == nil || qps == nil {
		return
	}
	p.llmRate = &rateGate{qps: qps, now: time.Now}
}

// AcquireLLM takes a concurrency slot first and only then waits for its turn
// in the rate gate, so requests that queued behind busy slots cannot all start
// in the same instant once slots free up.
func (p *ProcessingPools) AcquireLLM(ctx context.Context) (func(), error) {
	if p == nil || p.llm == nil {
		return func() {}, nil
	}
	release, err := p.llm.acquire(ctx)
	if err != nil {
		return nil, err
	}
	if err := p.llmRate.wait(ctx); err != nil {
		release()
		return nil, fmt.Errorf("wait for global llm rate limit: %w", err)
	}
	return release, nil
}

func (g *rateGate) wait(ctx context.Context) error {
	if g == nil {
		return nil
	}
	qps := g.qps()
	if qps <= 0 {
		return nil
	}
	gap := time.Duration(float64(time.Second) / qps)
	g.mu.Lock()
	now := g.now()
	start := g.next
	if start.Before(now) {
		start = now
	}
	g.next = start.Add(gap)
	g.mu.Unlock()
	delay := start.Sub(now)
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *concurrencyPool) acquire(ctx context.Context) (func(), error) {
	select {
	case p.slots <- struct{}{}:
		return func() { <-p.slots }, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("wait for global %s pool: %w", p.name, ctx.Err())
	}
}
