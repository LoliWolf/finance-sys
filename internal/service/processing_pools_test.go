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
