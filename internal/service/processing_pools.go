package service

import (
	"context"
	"fmt"
)

// ProcessingPools owns the process-wide OCR and LLM semaphores. Every
// document entry point reaches them through DocumentService.
type ProcessingPools struct {
	ocr *concurrencyPool
	llm *concurrencyPool
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

func (p *ProcessingPools) AcquireLLM(ctx context.Context) (func(), error) {
	if p == nil || p.llm == nil {
		return func() {}, nil
	}
	return p.llm.acquire(ctx)
}

func (p *concurrencyPool) acquire(ctx context.Context) (func(), error) {
	select {
	case p.slots <- struct{}{}:
		return func() { <-p.slots }, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("wait for global %s pool: %w", p.name, ctx.Err())
	}
}
