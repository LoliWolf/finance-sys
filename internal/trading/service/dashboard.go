package service

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"time"

	"finance-sys/internal/dal"
	"finance-sys/internal/domain/db_model"
	tradingdomain "finance-sys/internal/trading/domain"
	"finance-sys/internal/trading/instrument"
)

const dashboardItemLimit = 200

type TradingDashboardView struct {
	TradeDate      string                     `json:"trade_date"`
	Runtime        TradingDashboardRuntime    `json:"runtime"`
	Account        *TradingDashboardAccount   `json:"account"`
	Positions      []TradingDashboardPosition `json:"positions"`
	DailySummary   TradingDashboardDaily      `json:"daily_summary"`
	Fills          []TradingDashboardFill     `json:"fills"`
	Orders         []TradingDashboardOrder    `json:"orders"`
	PositionCycles []TradingDashboardCycle    `json:"position_cycles"`
}

type TradingDashboardRuntime struct {
	TradingEnabled        bool   `json:"trading_enabled"`
	NacosKillSwitch       bool   `json:"nacos_kill_switch"`
	RuntimeKillSwitch     bool   `json:"runtime_kill_switch"`
	SchedulerEnabled      bool   `json:"scheduler_enabled"`
	ExitEnabled           bool   `json:"exit_enabled"`
	ReconciliationEnabled bool   `json:"reconciliation_enabled"`
	Environment           string `json:"environment"`
	Provider              string `json:"provider"`
	ConfigVersion         int64  `json:"config_version"`
}

type TradingDashboardAccount struct {
	AccountID             string    `json:"account_id"`
	AccountName           string    `json:"account_name"`
	NAV                   string    `json:"nav"`
	Balance               string    `json:"balance"`
	AvailableCash         string    `json:"available_cash"`
	FrozenCash            string    `json:"frozen_cash"`
	MarketValue           string    `json:"market_value"`
	PositionRatio         string    `json:"position_ratio"`
	FloatingPnL           string    `json:"floating_pnl"`
	CumulativePnL         string    `json:"cumulative_pnl"`
	CumulativeCommission  string    `json:"cumulative_commission"`
	CommissionDataStatus  string    `json:"commission_data_status"`
	TerminalState         string    `json:"terminal_state"`
	AccountState          string    `json:"account_state"`
	SnapshotAt            time.Time `json:"snapshot_at"`
	SnapshotAgeSeconds    int64     `json:"snapshot_age_seconds"`
	SnapshotMaxAgeSeconds int       `json:"snapshot_max_age_seconds"`
	SnapshotStale         bool      `json:"snapshot_stale"`
}

type TradingDashboardPosition struct {
	ID               int64  `json:"id"`
	Symbol           string `json:"symbol"`
	TSCode           string `json:"ts_code"`
	SecurityName     string `json:"security_name"`
	Market           string `json:"market"`
	AssetType        string `json:"asset_type"`
	EastmoneySymbol  string `json:"eastmoney_symbol"`
	Volume           int64  `json:"volume"`
	AvailableVolume  int64  `json:"available_volume"`
	TodayVolume      int64  `json:"today_volume"`
	VWAP             string `json:"vwap"`
	LastPrice        string `json:"last_price"`
	MarketValue      string `json:"market_value"`
	FloatingPnL      string `json:"floating_pnl"`
	FloatingPnLRatio string `json:"floating_pnl_ratio"`
	CycleID          *int64 `json:"cycle_id"`
	CycleStatus      string `json:"cycle_status"`
	StopLossPrice    string `json:"stop_loss_price"`
	TakeProfitPrice  string `json:"take_profit_price"`
	HoldingTradeDays int32  `json:"holding_trade_days"`
	MaxHoldingDays   int32  `json:"max_holding_trade_days"`
	ExitReason       string `json:"exit_reason"`
}

type TradingDashboardDaily struct {
	FillCount   int    `json:"fill_count"`
	BuyCount    int    `json:"buy_count"`
	SellCount   int    `json:"sell_count"`
	BuyVolume   int64  `json:"buy_volume"`
	SellVolume  int64  `json:"sell_volume"`
	BuyAmount   string `json:"buy_amount"`
	SellAmount  string `json:"sell_amount"`
	Commission  string `json:"commission"`
	NetCashFlow string `json:"net_cash_flow"`
}

type TradingDashboardFill struct {
	ID               int64     `json:"id"`
	TradingOrderID   int64     `json:"trading_order_id"`
	ClientOrderID    string    `json:"client_order_id"`
	Symbol           string    `json:"symbol"`
	TSCode           string    `json:"ts_code"`
	SecurityName     string    `json:"security_name"`
	Side             string    `json:"side"`
	Price            string    `json:"price"`
	Volume           int64     `json:"volume"`
	Amount           string    `json:"amount"`
	Commission       string    `json:"commission"`
	CommissionStatus string    `json:"commission_status"`
	OrderStatus      string    `json:"order_status"`
	TradedAt         time.Time `json:"traded_at"`
}

type TradingDashboardOrder struct {
	ID               int64      `json:"id"`
	ClientOrderID    string     `json:"client_order_id"`
	Symbol           string     `json:"symbol"`
	TSCode           string     `json:"ts_code"`
	SecurityName     string     `json:"security_name"`
	Side             string     `json:"side"`
	OrderType        string     `json:"order_type"`
	LimitPrice       *string    `json:"limit_price"`
	Volume           int64      `json:"volume"`
	FilledVolume     int64      `json:"filled_volume"`
	FilledVWAP       *string    `json:"filled_vwap"`
	FilledAmount     string     `json:"filled_amount"`
	FilledCommission string     `json:"filled_commission"`
	Status           string     `json:"status"`
	ProviderStatus   string     `json:"provider_status"`
	ErrorCode        string     `json:"error_code"`
	ErrorMessage     string     `json:"error_message"`
	CreatedAt        time.Time  `json:"created_at"`
	SubmittedAt      *time.Time `json:"submitted_at"`
	FinishedAt       *time.Time `json:"finished_at"`
}

type TradingDashboardCycle struct {
	ID                          int64      `json:"id"`
	SourceRecommendationEventID *int64     `json:"source_recommendation_event_id"`
	Symbol                      string     `json:"symbol"`
	TSCode                      string     `json:"ts_code"`
	SecurityName                string     `json:"security_name"`
	Status                      string     `json:"status"`
	EntryTradeDate              time.Time  `json:"entry_trade_date"`
	SellableTradeDate           time.Time  `json:"sellable_trade_date"`
	EntryPrice                  string     `json:"entry_price"`
	InitialVolume               int64      `json:"initial_volume"`
	CurrentVolume               int64      `json:"current_volume"`
	StopLossPrice               string     `json:"stop_loss_price"`
	TakeProfitPrice             string     `json:"take_profit_price"`
	HoldingTradeDays            int32      `json:"holding_trade_days"`
	MaxHoldingTradeDays         int32      `json:"max_holding_trade_days"`
	ExitReason                  string     `json:"exit_reason"`
	ExitPrice                   *string    `json:"exit_price"`
	RealizedPnL                 *string    `json:"realized_pnl"`
	RealizedPnLRatio            *string    `json:"realized_pnl_ratio"`
	OpenedAt                    time.Time  `json:"opened_at"`
	ClosedAt                    *time.Time `json:"closed_at"`
}

func (s *Service) TradingDashboard(ctx context.Context, tradeDate string) (*TradingDashboardView, error) {
	cfg := s.runtime.Config()
	if cfg == nil {
		return nil, fmt.Errorf("runtime config unavailable")
	}
	date, from, to, err := dashboardDateRange(tradeDate, cfg.Meta.Timezone)
	if err != nil {
		return nil, err
	}
	view := &TradingDashboardView{
		TradeDate: date,
		Runtime: TradingDashboardRuntime{
			TradingEnabled: cfg.Trading.Enabled, NacosKillSwitch: cfg.Trading.KillSwitch,
			RuntimeKillSwitch: true, SchedulerEnabled: cfg.Trading.Scheduler.Enabled,
			ExitEnabled: cfg.Trading.Exit.Enabled, ReconciliationEnabled: cfg.Trading.Reconciliation.Enabled,
			Environment: cfg.Trading.Environment, Provider: cfg.Trading.Provider, ConfigVersion: cfg.Meta.ConfigVersion,
		},
		Positions: []TradingDashboardPosition{}, Fills: []TradingDashboardFill{},
		Orders: []TradingDashboardOrder{}, PositionCycles: []TradingDashboardCycle{},
	}
	if control, controlErr := dal.TradingRuntimeControls.KillSwitch(ctx, s.db); controlErr == nil {
		view.Runtime.RuntimeKillSwitch = control.Enabled
	}

	accountID := cfg.Trading.Bridge.ExpectedAccountID
	account, accountErr := dal.TradingAccountSnapshots.Latest(ctx, s.db, accountID)
	if accountErr != nil && !errors.Is(accountErr, dal.ErrNotFound) {
		return nil, accountErr
	}
	var positions []db_model.TradingPositionSnapshot
	if account != nil {
		positions, err = dal.TradingPositionSnapshots.ByAccountSnapshot(ctx, s.db, account.ID)
		if err != nil {
			return nil, err
		}
		age := time.Since(account.SnapshotAt)
		if age < 0 {
			age = 0
		}
		view.Account = &TradingDashboardAccount{
			AccountID: account.AccountID, AccountName: account.AccountName, NAV: account.Nav, Balance: account.Balance,
			AvailableCash: account.AvailableCash, FrozenCash: account.FrozenCash, MarketValue: account.MarketValue,
			PositionRatio: safeRatio(account.MarketValue, account.Nav), FloatingPnL: account.FloatingPnl,
			CumulativePnL: account.CumulativePnl, CumulativeCommission: account.CumulativeCommission,
			CommissionDataStatus: account.CommissionDataStatus, TerminalState: account.TerminalState,
			AccountState: account.AccountState, SnapshotAt: account.SnapshotAt, SnapshotAgeSeconds: int64(age.Seconds()),
			SnapshotMaxAgeSeconds: cfg.Trading.Risk.MaxSnapshotAgeSeconds,
			SnapshotStale:         age > time.Duration(cfg.Trading.Risk.MaxSnapshotAgeSeconds)*time.Second,
		}
	}

	fills, err := dal.TradingFills.ListByAccountAndTradedAt(ctx, s.db, accountID, from, to, dashboardItemLimit)
	if err != nil {
		return nil, err
	}
	orders, err := dal.TradingOrders.ListByAccountAndCreatedAt(ctx, s.db, accountID, from, to, dashboardItemLimit)
	if err != nil {
		return nil, err
	}
	cycles, err := dal.TradingPositionCycles.List(ctx, s.db, accountID, "", dashboardItemLimit)
	if err != nil {
		return nil, err
	}

	orderIDs := make([]int64, 0, len(fills)+len(cycles)*2)
	for _, fill := range fills {
		orderIDs = append(orderIDs, fill.TradingOrderID)
	}
	for _, cycle := range cycles {
		if cycle.EntryOrderID != nil {
			orderIDs = append(orderIDs, *cycle.EntryOrderID)
		}
		if cycle.ExitOrderID != nil {
			orderIDs = append(orderIDs, *cycle.ExitOrderID)
		}
	}
	linkedOrders, err := dal.TradingOrders.QueryByIDs(ctx, s.db, uniqueInt64s(orderIDs))
	if err != nil {
		return nil, err
	}
	orderByID := make(map[int64]db_model.TradingOrder, len(linkedOrders)+len(orders))
	for _, order := range append(linkedOrders, orders...) {
		orderByID[order.ID] = order
	}

	securityByTSCode, err := s.dashboardSecurities(ctx, positions, fills, orders, cycles)
	if err != nil {
		return nil, err
	}
	openCycleBySymbol := make(map[string]db_model.TradingPositionCycle)
	for _, cycle := range cycles {
		if cycle.Status == "OPEN" || cycle.Status == "EXIT_PENDING" {
			openCycleBySymbol[strings.ToUpper(cycle.EastmoneySymbol)] = cycle
		}
	}

	for _, position := range positions {
		identity := dashboardIdentity(position.Symbol, position.EastmoneySymbol, "", securityByTSCode)
		item := TradingDashboardPosition{
			ID: position.ID, Symbol: position.Symbol, TSCode: identity.TSCode, SecurityName: identity.Name,
			Market: identity.Market, AssetType: identity.AssetType, EastmoneySymbol: position.EastmoneySymbol,
			Volume: position.Volume, AvailableVolume: position.AvailableVolume, TodayVolume: position.TodayVolume,
			VWAP: position.Vwap, LastPrice: position.LastPrice, MarketValue: position.MarketValue,
			FloatingPnL: position.FloatingPnl, FloatingPnLRatio: safeRatio(position.FloatingPnl, multiplyDecimal(position.Vwap, position.Volume)),
		}
		if cycle, ok := openCycleBySymbol[strings.ToUpper(position.EastmoneySymbol)]; ok {
			item.CycleID = &cycle.ID
			item.CycleStatus = cycle.Status
			item.StopLossPrice = cycle.StopLossPrice
			item.TakeProfitPrice = cycle.TakeProfitPrice
			item.HoldingTradeDays = cycle.HoldingTradeDays
			item.MaxHoldingDays = cycle.MaxHoldingTradeDays
			item.ExitReason = cycle.ExitReason
		}
		view.Positions = append(view.Positions, item)
	}
	view.DailySummary = buildDashboardDaily(fills)
	view.Fills = buildDashboardFills(fills, orderByID, securityByTSCode)
	view.Orders = buildDashboardOrders(orders, securityByTSCode)
	view.PositionCycles = buildDashboardCycles(cycles, orderByID, securityByTSCode)
	return view, nil
}

func (s *Service) RefreshTradingDashboard(ctx context.Context, tradeDate string) (*TradingDashboardView, error) {
	cfg := s.runtime.Config()
	if cfg == nil {
		return nil, fmt.Errorf("runtime config unavailable")
	}
	if _, _, _, err := dashboardDateRange(tradeDate, cfg.Meta.Timezone); err != nil {
		return nil, err
	}
	if s.bridge == nil {
		return nil, fmt.Errorf("Windows Bridge client unavailable")
	}
	health, err := s.bridge.Health(ctx)
	if err != nil {
		return nil, fmt.Errorf("Windows Bridge unavailable: %w", err)
	}
	if health.Runner != "READY" {
		return nil, fmt.Errorf("Windows Runner is offline; start FinanceSys-Eastmoney-Runner before refreshing")
	}
	if health.Terminal != "CONNECTED" {
		return nil, fmt.Errorf("Eastmoney terminal is not connected: %s", health.Terminal)
	}
	if health.AuthState != "AUTH_OK" {
		return nil, fmt.Errorf("Eastmoney authentication is not ready: %s", health.AuthState)
	}
	if health.AccountID != cfg.Trading.Bridge.ExpectedAccountID {
		return nil, fmt.Errorf("Bridge account differs from the configured simulation account")
	}

	var baseline *tradingdomain.ReconciliationSnapshot
	if value, baselineErr := s.bridge.ReconciliationSnapshot(ctx, ""); baselineErr == nil {
		baseline = value
	}
	requestedAt := time.Now()
	if _, err := s.bridge.RefreshSnapshot(ctx, hashParts("dashboard-refresh", requestedAt.Format(time.RFC3339Nano))); err != nil {
		return nil, fmt.Errorf("request Bridge snapshot refresh: %w", err)
	}
	wait, maxAge := bridgeSnapshotWaitSettings(cfg)
	deadline := time.NewTimer(wait)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		snapshot, snapshotErr := s.bridge.ReconciliationSnapshot(ctx, "")
		if snapshotErr == nil && dashboardSnapshotAdvanced(baseline, snapshot, requestedAt, maxAge) {
			if snapshot.Account.AccountID != cfg.Trading.Bridge.ExpectedAccountID || !strings.EqualFold(snapshot.Account.Environment, "SIMULATION") {
				return nil, fmt.Errorf("refreshed snapshot is not from the configured simulation account")
			}
			if err := s.persistSnapshot(ctx, snapshot.Account, snapshot.Positions); err != nil {
				return nil, fmt.Errorf("persist refreshed account snapshot: %w", err)
			}
			if err := s.syncPositionCycles(ctx, snapshot.Account, snapshot.Positions); err != nil {
				return nil, fmt.Errorf("sync position cycles after refresh: %w", err)
			}
			return s.TradingDashboard(ctx, tradeDate)
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("wait for refreshed Bridge snapshot: %w", ctx.Err())
		case <-deadline.C:
			return nil, fmt.Errorf("timed out waiting for Windows Runner to produce a new account snapshot")
		case <-ticker.C:
		}
	}
}

func dashboardSnapshotAdvanced(baseline, candidate *tradingdomain.ReconciliationSnapshot, requestedAt time.Time, maxAge time.Duration) bool {
	if candidate == nil || candidate.Account.SnapshotAt.IsZero() {
		return false
	}
	if baseline == nil || baseline.Account.SnapshotAt.IsZero() {
		return !candidate.Account.SnapshotAt.Before(requestedAt.Add(-maxAge))
	}
	return candidate.SnapshotVersion != baseline.SnapshotVersion && candidate.Account.SnapshotAt.After(baseline.Account.SnapshotAt)
}

func dashboardDateRange(value, timezone string) (string, time.Time, time.Time, error) {
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return "", time.Time{}, time.Time{}, fmt.Errorf("invalid configured timezone %q: %w", timezone, err)
	}
	if strings.TrimSpace(value) == "" {
		value = time.Now().In(location).Format("2006-01-02")
	}
	from, err := time.ParseInLocation("2006-01-02", value, location)
	if err != nil {
		return "", time.Time{}, time.Time{}, fmt.Errorf("trade_date must use YYYY-MM-DD")
	}
	return value, from, from.AddDate(0, 0, 1), nil
}

type dashboardSecurityIdentity struct {
	TSCode    string
	Name      string
	Market    string
	AssetType string
}

func (s *Service) dashboardSecurities(ctx context.Context, positions []db_model.TradingPositionSnapshot, fills []db_model.TradingFill, orders []db_model.TradingOrder, cycles []db_model.TradingPositionCycle) (map[string]db_model.SecurityMaster, error) {
	codes := make([]string, 0, len(positions)+len(fills)+len(orders)+len(cycles))
	for _, item := range positions {
		codes = append(codes, dashboardTSCode(item.Symbol, item.EastmoneySymbol, ""))
	}
	for _, item := range fills {
		codes = append(codes, dashboardTSCode(item.Symbol, item.EastmoneySymbol, ""))
	}
	for _, item := range orders {
		codes = append(codes, dashboardTSCode(item.Symbol, item.EastmoneySymbol, ""))
	}
	for _, item := range cycles {
		codes = append(codes, item.TSCode)
	}
	masters, err := dal.SecurityMasters.QueryByTSCodes(ctx, s.db, uniqueStrings(codes))
	if err != nil {
		return nil, err
	}
	result := make(map[string]db_model.SecurityMaster, len(masters))
	for _, master := range masters {
		result[strings.ToUpper(master.TSCode)] = master
	}
	return result, nil
}

func dashboardIdentity(symbol, eastmoneySymbol, assetType string, masters map[string]db_model.SecurityMaster) dashboardSecurityIdentity {
	code := dashboardTSCode(symbol, eastmoneySymbol, assetType)
	if master, ok := masters[strings.ToUpper(code)]; ok {
		return dashboardSecurityIdentity{TSCode: master.TSCode, Name: master.Name, Market: master.Market, AssetType: master.AssetType}
	}
	identity := dashboardSecurityIdentity{TSCode: code, Name: symbol, AssetType: strings.ToUpper(assetType)}
	if parsed, err := instrument.Parse(eastmoneySymbol, "", assetType); err == nil {
		identity.Market = parsed.Market
		identity.AssetType = parsed.AssetType
	}
	return identity
}

func dashboardTSCode(symbol, eastmoneySymbol, assetType string) string {
	if parsed, err := instrument.Parse(eastmoneySymbol, "", assetType); err == nil {
		return parsed.TSCode
	}
	return strings.ToUpper(strings.TrimSpace(symbol))
}

func buildDashboardDaily(fills []db_model.TradingFill) TradingDashboardDaily {
	buyAmount := new(big.Rat)
	sellAmount := new(big.Rat)
	commission := new(big.Rat)
	result := TradingDashboardDaily{FillCount: len(fills)}
	for _, fill := range fills {
		commission.Add(commission, decimalRat(fill.Commission))
		if strings.EqualFold(fill.Side, "BUY") {
			result.BuyCount++
			result.BuyVolume += fill.Volume
			buyAmount.Add(buyAmount, decimalRat(fill.Amount))
		} else if strings.EqualFold(fill.Side, "SELL") {
			result.SellCount++
			result.SellVolume += fill.Volume
			sellAmount.Add(sellAmount, decimalRat(fill.Amount))
		}
	}
	netCashFlow := new(big.Rat).Sub(new(big.Rat).Set(sellAmount), buyAmount)
	netCashFlow.Sub(netCashFlow, commission)
	result.BuyAmount = buyAmount.FloatString(6)
	result.SellAmount = sellAmount.FloatString(6)
	result.Commission = commission.FloatString(6)
	result.NetCashFlow = netCashFlow.FloatString(6)
	return result
}

func buildDashboardFills(fills []db_model.TradingFill, orders map[int64]db_model.TradingOrder, masters map[string]db_model.SecurityMaster) []TradingDashboardFill {
	result := make([]TradingDashboardFill, 0, len(fills))
	for _, fill := range fills {
		identity := dashboardIdentity(fill.Symbol, fill.EastmoneySymbol, "", masters)
		order := orders[fill.TradingOrderID]
		result = append(result, TradingDashboardFill{
			ID: fill.ID, TradingOrderID: fill.TradingOrderID, ClientOrderID: order.ClientOrderID,
			Symbol: fill.Symbol, TSCode: identity.TSCode, SecurityName: identity.Name, Side: fill.Side,
			Price: fill.Price, Volume: fill.Volume, Amount: fill.Amount, Commission: fill.Commission,
			CommissionStatus: fill.CommissionStatus, OrderStatus: order.Status, TradedAt: fill.TradedAt,
		})
	}
	return result
}

func buildDashboardOrders(orders []db_model.TradingOrder, masters map[string]db_model.SecurityMaster) []TradingDashboardOrder {
	result := make([]TradingDashboardOrder, 0, len(orders))
	for _, order := range orders {
		identity := dashboardIdentity(order.Symbol, order.EastmoneySymbol, "", masters)
		result = append(result, TradingDashboardOrder{
			ID: order.ID, ClientOrderID: order.ClientOrderID, Symbol: order.Symbol, TSCode: identity.TSCode,
			SecurityName: identity.Name, Side: order.Side, OrderType: order.OrderType, LimitPrice: order.LimitPrice,
			Volume: order.Volume, FilledVolume: order.FilledVolume, FilledVWAP: order.FilledVwap,
			FilledAmount: order.FilledAmount, FilledCommission: order.FilledCommission, Status: order.Status,
			ProviderStatus: order.ProviderStatus, ErrorCode: order.ErrorCode, ErrorMessage: order.ErrorMessage,
			CreatedAt: order.CreatedAt, SubmittedAt: order.SubmittedAt, FinishedAt: order.FinishedAt,
		})
	}
	return result
}

func buildDashboardCycles(cycles []db_model.TradingPositionCycle, orders map[int64]db_model.TradingOrder, masters map[string]db_model.SecurityMaster) []TradingDashboardCycle {
	result := make([]TradingDashboardCycle, 0, len(cycles))
	for _, cycle := range cycles {
		identity := dashboardIdentity(cycle.Symbol, cycle.EastmoneySymbol, cycle.AssetType, masters)
		item := TradingDashboardCycle{
			ID: cycle.ID, SourceRecommendationEventID: cycle.SourceRecommendationEventID,
			Symbol: cycle.Symbol, TSCode: identity.TSCode, SecurityName: identity.Name, Status: cycle.Status,
			EntryTradeDate: cycle.EntryTradeDate, SellableTradeDate: cycle.SellableTradeDate,
			EntryPrice: cycle.EntryPrice, InitialVolume: cycle.InitialVolume, CurrentVolume: cycle.CurrentVolume,
			StopLossPrice: cycle.StopLossPrice, TakeProfitPrice: cycle.TakeProfitPrice,
			HoldingTradeDays: cycle.HoldingTradeDays, MaxHoldingTradeDays: cycle.MaxHoldingTradeDays,
			ExitReason: cycle.ExitReason, OpenedAt: cycle.OpenedAt, ClosedAt: cycle.ClosedAt,
		}
		if cycle.Status == "CLOSED" && cycle.EntryOrderID != nil && cycle.ExitOrderID != nil {
			entry, entryOK := orders[*cycle.EntryOrderID]
			exit, exitOK := orders[*cycle.ExitOrderID]
			if entryOK && exitOK && exit.FilledVwap != nil && entry.FilledAmount != "" && exit.FilledAmount != "" {
				item.ExitPrice = exit.FilledVwap
				cost := new(big.Rat).Add(decimalRat(entry.FilledAmount), decimalRat(entry.FilledCommission))
				proceeds := new(big.Rat).Sub(decimalRat(exit.FilledAmount), decimalRat(exit.FilledCommission))
				pnl := new(big.Rat).Sub(proceeds, cost)
				pnlText := pnl.FloatString(6)
				item.RealizedPnL = &pnlText
				if cost.Sign() > 0 {
					ratio := new(big.Rat).Quo(pnl, cost).FloatString(8)
					item.RealizedPnLRatio = &ratio
				}
			}
		}
		result = append(result, item)
	}
	return result
}

func uniqueInt64s(values []int64) []int64 {
	seen := make(map[int64]struct{}, len(values))
	result := make([]int64, 0, len(values))
	for _, value := range values {
		if value == 0 {
			continue
		}
		if _, ok := seen[value]; !ok {
			seen[value] = struct{}{}
			result = append(result, value)
		}
	}
	return result
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToUpper(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; !ok {
			seen[value] = struct{}{}
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}
