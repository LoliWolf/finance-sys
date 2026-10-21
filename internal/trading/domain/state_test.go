package domain

import "testing"

func TestOrderStateTransitions(t *testing.T) {
	if !CanTransitionOrder("SUBMITTED", "PARTIALLY_FILLED") {
		t.Fatal("expected submitted to partially filled transition")
	}
	if CanTransitionOrder("FILLED", "SUBMITTED") {
		t.Fatal("terminal status must not move backwards")
	}
	if !IsTerminalOrderStatus("FILLED") || IsTerminalOrderStatus("UNKNOWN") {
		t.Fatal("unexpected terminal status classification")
	}
}
