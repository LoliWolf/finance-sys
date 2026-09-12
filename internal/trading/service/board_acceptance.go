package service

import (
	"context"
	"encoding/json"
	"fmt"

	"finance-sys/internal/config"
	"finance-sys/internal/dal"
	"finance-sys/internal/domain/db_model"
	tradingdomain "finance-sys/internal/trading/domain"
	"finance-sys/internal/trading/instrument"
)

// Verification is derived from settled minimum-unit UAT fills and fee evidence,
// never from allowed_boards or from successfully displaying a market quote.
func (s *Service) VerifyBoardAcceptance(ctx context.Context, board string) (*db_model.TradingBoardCapability, error) {
	cfg := s.runtime.Config()
	if cfg == nil {
		return nil, fmt.Errorf("config unavailable")
	}
	unit, err := instrument.UnitRule(board)
	if err != nil {
		return nil, err
	}
	orders, err := dal.TradingOrders.ByAccount(ctx, s.db, cfg.Trading.Bridge.ExpectedAccountID)
	if err != nil {
		return nil, err
	}
	marketProofs := map[string]any{}
	for _, buy := range orders {
		if buy.Side != "BUY" || buy.Status != "FILLED" || buy.FilledVolume != unit.MinimumBuyVolume {
			continue
		}
		security, e := instrument.Parse(buy.EastmoneySymbol, "", "")
		if e != nil || security.BoardType != board {
			continue
		}
		var buyRequest tradingdomain.BridgeOrderRequest
		_ = json.Unmarshal(buy.RequestJSON, &buyRequest)
		if buyRequest.UATKey == "" && board != instrument.BoardSHMain {
			continue
		}
		buyIntent, e := dal.TradingIntents.QueryByID(ctx, s.db, buy.TradingIntentID)
		if e != nil {
			return nil, e
		}
		buyRun, e := dal.TradingAgentRuns.QueryByID(ctx, s.db, buyIntent.TradingAgentRunID)
		if e != nil {
			return nil, e
		}
		if buyRun.TriggerType != "UAT_MANUAL" {
			continue
		}
		buyFills, e := dal.TradingFills.ByOrder(ctx, s.db, buy.ID)
		if e != nil {
			return nil, e
		}
		if !completeVerifiedFills(buy, buyFills) {
			continue
		}
		for _, sell := range orders {
			if sell.ID <= buy.ID || sell.Side != "SELL" || sell.EastmoneySymbol != buy.EastmoneySymbol || sell.Status != "FILLED" || sell.FilledVolume != buy.FilledVolume {
				continue
			}
			var sellRequest tradingdomain.BridgeOrderRequest
			_ = json.Unmarshal(sell.RequestJSON, &sellRequest)
			if buyRequest.UATKey != "" && sellRequest.UATKey != buyRequest.UATKey {
				continue
			}
			sellFills, e := dal.TradingFills.ByOrder(ctx, s.db, sell.ID)
			if e != nil {
				return nil, e
			}
			if !completeVerifiedFills(sell, sellFills) {
				continue
			}
			if !sellFills[0].TradedAt.After(buyFills[len(buyFills)-1].TradedAt) {
				continue
			}
			if (security.AssetType == "STOCK" || buyRequest.TradeN > 0) && config.ChinaDate(sellFills[0].TradedAt) <= config.ChinaDate(buyFills[len(buyFills)-1].TradedAt) {
				continue
			}
			key := hashParts(cfg.Trading.Provider, cfg.Trading.Bridge.ExpectedAccountID, board, security.AssetType)
			now := s.now()
			marketProofs[security.Market] = map[string]any{"buy_order_id": buy.ID, "sell_order_id": sell.ID, "buy_fills": buyFills, "sell_fills": sellFills, "uat_key": buyRequest.UATKey, "legacy_sh_main": buyRequest.UATKey == ""}
			if board == instrument.BoardETF && len(marketProofs) < 2 {
				break
			}
			capability := &db_model.TradingBoardCapability{CapabilityKey: key, Provider: cfg.Trading.Provider, Environment: "SIMULATION", AccountID: cfg.Trading.Bridge.ExpectedAccountID, BoardType: board, AssetType: security.AssetType, BuyEnabled: true, SellEnabled: true, MinimumBuyVolume: unit.MinimumBuyVolume, BuyStep: unit.BuyStep, MinimumSellVolume: unit.MinimumSellVolume, SellStep: unit.SellStep, ResidualSellSupported: true, VerificationStatus: "VERIFIED", VerifiedAt: &now, EvidenceJSON: marshalJSON(map[string]any{"markets": marketProofs}), TradingRuleVersion: cfg.Trading.Risk.TradingRuleVersion, ConfigVersion: cfg.Meta.ConfigVersion}
			if e := dal.TradingBoardCapabilities.Upsert(ctx, s.db, capability); e != nil {
				return nil, e
			}
			return capability, nil
		}
	}
	return nil, fmt.Errorf("%s has no complete, fee-verified minimum-unit UAT BUY/SELL pair", board)
}

func completeVerifiedFills(order db_model.TradingOrder, fills []db_model.TradingFill) bool {
	volume := int64(0)
	if len(fills) == 0 {
		return false
	}
	for _, fill := range fills {
		if fill.CommissionStatus != "VERIFIED" || !isPositiveDecimal(fill.Price) || fill.Volume <= 0 {
			return false
		}
		volume += fill.Volume
	}
	return volume == order.FilledVolume
}
