package service

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"finance-sys/internal/config"
	"gorm.io/gorm"
)

const TradingReportItemLimit = 2000

type TradingReportLimitError struct{ Limit int }

func (e *TradingReportLimitError) Error() string {
	return fmt.Sprintf("当日成交、订单或当前持仓超过单次导出 %d 条限制，请缩小日期范围", e.Limit)
}

type TradingDashboardReport struct {
	Kind        string                `json:"kind"`
	GeneratedAt time.Time             `json:"generated_at"`
	MaxItems    int                   `json:"max_items"`
	CycleLimit  int                   `json:"cycle_limit"`
	Trading     *TradingDashboardView `json:"trading"`
}

// Database snapshot only: no Bridge call, reconciliation job or order is submitted.
func (s *Service) TradingDashboardReport(ctx context.Context, date string) (*TradingDashboardReport, error) {
	options := &sql.TxOptions{ReadOnly: true}
	if s.db.Dialector.Name() == "mysql" {
		options.Isolation = sql.LevelRepeatableRead
	}
	frozen := config.NewRuntime(s.runtime.Current())
	var result *TradingDashboardReport
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		reader := &Service{db: tx, runtime: frozen}
		view, err := reader.tradingDashboard(ctx, date, TradingReportItemLimit+1)
		if err != nil {
			return err
		}
		if len(view.Fills) > TradingReportItemLimit || len(view.Orders) > TradingReportItemLimit || len(view.Positions) > TradingReportItemLimit {
			return &TradingReportLimitError{Limit: TradingReportItemLimit}
		}
		result = &TradingDashboardReport{Kind: "trading", GeneratedAt: time.Now().UTC(), MaxItems: TradingReportItemLimit, CycleLimit: dashboardItemLimit, Trading: view}
		return nil
	}, options)
	return result, err
}
