package market

import (
	"context"
	"errors"
	"time"

	"finance-sys/internal/domain"
)

var ErrNotSupported = errors.New("market: operation not supported")

type Provider interface {
	Name() string
	GetSecurityMaster(ctx context.Context, market string, kind string) ([]domain.Security, error)
	GetRealtimeQuotes(ctx context.Context, symbols []string) ([]domain.Quote, error)
	GetDailyBars(ctx context.Context, symbol string, start time.Time, end time.Time, adjust string) ([]domain.DailyBar, error)
	GetMinuteBars(ctx context.Context, symbol string, start time.Time, end time.Time, interval string, adjust string) ([]domain.MinuteBar, error)
	GetTradingCalendar(ctx context.Context, start time.Time, end time.Time) ([]domain.TradingDay, error)
	GetCorporateActions(ctx context.Context, symbol string, start time.Time, end time.Time) ([]domain.CorporateAction, error)
	GetSuspensionStatus(ctx context.Context, symbols []string, tradeDate time.Time) ([]domain.SuspensionStatus, error)
	HealthCheck(ctx context.Context) error
}

type RawArchiver interface {
	Archive(ctx context.Context, provider string, kind string, payload []byte) (string, error)
}
