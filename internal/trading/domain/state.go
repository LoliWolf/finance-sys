package domain

var orderTransitions = map[string]map[string]struct{}{
	"DISPATCH_PENDING": {"BRIDGE_QUEUED": {}, "SUBMITTED": {}, "PARTIALLY_FILLED": {}, "FILLED": {}, "CANCELED": {}, "REJECTED": {}, "UNKNOWN": {}},
	"BRIDGE_QUEUED":    {"SUBMITTED": {}, "PARTIALLY_FILLED": {}, "FILLED": {}, "CANCELED": {}, "REJECTED": {}, "UNKNOWN": {}},
	"SUBMITTED":        {"SUBMITTED": {}, "PARTIALLY_FILLED": {}, "FILLED": {}, "CANCELED": {}, "REJECTED": {}, "UNKNOWN": {}},
	"PARTIALLY_FILLED": {"PARTIALLY_FILLED": {}, "FILLED": {}, "CANCELED": {}, "UNKNOWN": {}},
	"UNKNOWN":          {"SUBMITTED": {}, "PARTIALLY_FILLED": {}, "FILLED": {}, "CANCELED": {}, "REJECTED": {}, "UNKNOWN": {}},
}

func CanTransitionOrder(from, to string) bool {
	if from == to {
		return true
	}
	allowed, ok := orderTransitions[from]
	if !ok {
		return false
	}
	_, ok = allowed[to]
	return ok
}

func IsTerminalOrderStatus(status string) bool {
	switch status {
	case "FILLED", "CANCELED", "REJECTED":
		return true
	default:
		return false
	}
}
