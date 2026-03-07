package market

import (
	"context"
	"time"

	"finance-sys/internal/domain"
)

type AkshareBridgeProvider struct{}
type BaostockBridgeProvider struct{}
type MCPMarketProvider struct{}

func (AkshareBridgeProvider) Name() string  { return "akshare_bridge" }
func (BaostockBridgeProvider) Name() string { return "baostock_bridge" }
func (MCPMarketProvider) Name() string      { return "mcp_market" }

func (AkshareBridgeProvider) GetSecurityMaster(context.Context, string, string) ([]domain.Security, error) {
	return nil, ErrNotSupported
}
func (BaostockBridgeProvider) GetSecurityMaster(context.Context, string, string) ([]domain.Security, error) {
	return nil, ErrNotSupported
}
func (MCPMarketProvider) GetSecurityMaster(context.Context, string, string) ([]domain.Security, error) {
	return nil, ErrNotSupported
}
func (AkshareBridgeProvider) GetRealtimeQuotes(context.Context, []string) ([]domain.Quote, error) {
	return nil, ErrNotSupported
}
func (BaostockBridgeProvider) GetRealtimeQuotes(context.Context, []string) ([]domain.Quote, error) {
	return nil, ErrNotSupported
}
func (MCPMarketProvider) GetRealtimeQuotes(context.Context, []string) ([]domain.Quote, error) {
	return nil, ErrNotSupported
}
func (AkshareBridgeProvider) GetDailyBars(context.Context, string, time.Time, time.Time, string) ([]domain.DailyBar, error) {
	return nil, ErrNotSupported
}
func (BaostockBridgeProvider) GetDailyBars(context.Context, string, time.Time, time.Time, string) ([]domain.DailyBar, error) {
	return nil, ErrNotSupported
}
func (MCPMarketProvider) GetDailyBars(context.Context, string, time.Time, time.Time, string) ([]domain.DailyBar, error) {
	return nil, ErrNotSupported
}
func (AkshareBridgeProvider) GetMinuteBars(context.Context, string, time.Time, time.Time, string, string) ([]domain.MinuteBar, error) {
	return nil, ErrNotSupported
}
func (BaostockBridgeProvider) GetMinuteBars(context.Context, string, time.Time, time.Time, string, string) ([]domain.MinuteBar, error) {
	return nil, ErrNotSupported
}
func (MCPMarketProvider) GetMinuteBars(context.Context, string, time.Time, time.Time, string, string) ([]domain.MinuteBar, error) {
	return nil, ErrNotSupported
}
func (AkshareBridgeProvider) GetTradingCalendar(context.Context, time.Time, time.Time) ([]domain.TradingDay, error) {
	return nil, ErrNotSupported
}
func (BaostockBridgeProvider) GetTradingCalendar(context.Context, time.Time, time.Time) ([]domain.TradingDay, error) {
	return nil, ErrNotSupported
}
func (MCPMarketProvider) GetTradingCalendar(context.Context, time.Time, time.Time) ([]domain.TradingDay, error) {
	return nil, ErrNotSupported
}
func (AkshareBridgeProvider) GetCorporateActions(context.Context, string, time.Time, time.Time) ([]domain.CorporateAction, error) {
	return nil, ErrNotSupported
}
func (BaostockBridgeProvider) GetCorporateActions(context.Context, string, time.Time, time.Time) ([]domain.CorporateAction, error) {
	return nil, ErrNotSupported
}
func (MCPMarketProvider) GetCorporateActions(context.Context, string, time.Time, time.Time) ([]domain.CorporateAction, error) {
	return nil, ErrNotSupported
}
func (AkshareBridgeProvider) GetSuspensionStatus(context.Context, []string, time.Time) ([]domain.SuspensionStatus, error) {
	return nil, ErrNotSupported
}
func (BaostockBridgeProvider) GetSuspensionStatus(context.Context, []string, time.Time) ([]domain.SuspensionStatus, error) {
	return nil, ErrNotSupported
}
func (MCPMarketProvider) GetSuspensionStatus(context.Context, []string, time.Time) ([]domain.SuspensionStatus, error) {
	return nil, ErrNotSupported
}
func (AkshareBridgeProvider) HealthCheck(context.Context) error  { return ErrNotSupported }
func (BaostockBridgeProvider) HealthCheck(context.Context) error { return ErrNotSupported }
func (MCPMarketProvider) HealthCheck(context.Context) error      { return ErrNotSupported }
