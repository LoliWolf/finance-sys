package domain

import (
	"context"
	"time"
)

type contextKey string

const tradeDateContextKey contextKey = "trade_date"

func ContextWithTradeDate(ctx context.Context, tradeDate time.Time) context.Context {
	return context.WithValue(ctx, tradeDateContextKey, tradeDate)
}

func TradeDateFromContext(ctx context.Context) (time.Time, bool) {
	value, ok := ctx.Value(tradeDateContextKey).(time.Time)
	if !ok || value.IsZero() {
		return time.Time{}, false
	}
	year, month, day := value.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, value.Location()), true
}
