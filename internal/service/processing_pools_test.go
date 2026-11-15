package service

import (
	"context"
	"testing"
	"time"
)

func TestProcessingPoolsEnforceGlobalOCRCapacity(t *testing.T) {
	pools, err := NewProcessingPools(2, 20)
	if err != nil {
		t.Fatal(err)
	}
	releaseOne, err := pools.AcquireOCR(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer releaseOne()
	releaseTwo, err := pools.AcquireOCR(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	acquired := make(chan func(), 1)
	go func() {
		release, acquireErr := pools.AcquireOCR(context.Background())
		if acquireErr == nil {
			acquired <- release
		}
	}()
	select {
	case release := <-acquired:
		release()
		t.Fatal("third OCR operation acquired before a slot was released")
	case <-time.After(50 * time.Millisecond):
	}
	releaseTwo()
	select {
	case release := <-acquired:
		release()
	case <-time.After(time.Second):
		t.Fatal("third OCR operation did not acquire after release")
	}
}

func TestProcessingPoolsRejectInvalidCapacity(t *testing.T) {
	if _, err := NewProcessingPools(0, 20); err == nil {
		t.Fatal("expected invalid OCR capacity error")
	}
	if _, err := NewProcessingPools(2, 0); err == nil {
		t.Fatal("expected invalid LLM capacity error")
	}
}

func TestProcessingPoolsLimitLLMStartRate(t *testing.T) {
	pools, err := NewProcessingPools(1, 20)
	if err != nil {
		t.Fatal(err)
	}
	qps := 20.0 // 50ms apart
	pools.LimitLLMRate(func() float64 { return qps })
	started := time.Now()
	for i := 0; i < 5; i++ {
		release, err := pools.AcquireLLM(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		release()
	}
	// Five starts at 20 QPS need at least four 50ms gaps.
	if elapsed := time.Since(started); elapsed < 190*time.Millisecond {
		t.Fatalf("5 starts at 20 QPS took %v; the rate gate did not space them", elapsed)
	}

	qps = 0 // unlimited after a config change
	started = time.Now()
	for i := 0; i < 20; i++ {
		release, err := pools.AcquireLLM(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		release()
	}
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("unlimited rate still waited %v", elapsed)
	}
}

func TestProcessingPoolsRateWaitHonorsCancellationAndFreesSlot(t *testing.T) {
	pools, err := NewProcessingPools(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	pools.LimitLLMRate(func() float64 { return 0.5 }) // 2s apart
	release, err := pools.AcquireLLM(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	release()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := pools.AcquireLLM(ctx); err == nil {
		t.Fatal("expected the rate wait to stop on context cancellation")
	}
	// The canceled caller must have returned its concurrency slot.
	select {
	case pools.llm.slots <- struct{}{}:
		<-pools.llm.slots
	default:
		t.Fatal("canceled rate wait leaked the LLM slot")
	}
}
