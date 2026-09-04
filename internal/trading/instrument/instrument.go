package instrument

import (
	"fmt"
	"strings"
)

const (
	BoardSHMain  = "SH_MAIN"
	BoardSZMain  = "SZ_MAIN"
	BoardChiNext = "CHINEXT"
	BoardSTAR    = "STAR"
	BoardETF     = "ETF"
	BoardUnknown = "UNKNOWN"
)

type Canonical struct {
	Symbol          string `json:"symbol"`
	TSCode          string `json:"ts_code"`
	Market          string `json:"market"`
	EastmoneySymbol string `json:"eastmoney_symbol"`
	AssetType       string `json:"asset_type"`
	BoardType       string `json:"board_type"`
}

type TradingUnitRule struct {
	MinimumBuyVolume  int64 `json:"minimum_buy_volume"`
	BuyStep           int64 `json:"buy_step"`
	MinimumSellVolume int64 `json:"minimum_sell_volume"`
	SellStep          int64 `json:"sell_step"`
	MaximumBuyVolume  int64 `json:"maximum_buy_volume"`
	MaximumSellVolume int64 `json:"maximum_sell_volume"`
}

func Parse(raw, market, assetType string) (Canonical, error) {
	raw = strings.ToUpper(strings.TrimSpace(raw))
	market = normalizeMarket(market)
	assetType = normalizeAssetType(assetType)
	if raw == "" {
		return Canonical{}, fmt.Errorf("security symbol is required")
	}

	code, inferredMarket, err := splitSymbol(raw)
	if err != nil {
		return Canonical{}, err
	}
	if market == "" {
		market = inferredMarket
	}
	if inferredMarket != "" && market != inferredMarket {
		return Canonical{}, fmt.Errorf("security symbol %q conflicts with market %q", raw, market)
	}
	if market == "" {
		market = inferMarket(code)
	}
	if market != "SH" && market != "SZ" {
		return Canonical{}, fmt.Errorf("security symbol %q has unsupported market %q", raw, market)
	}
	if !isSixDigitCode(code) {
		return Canonical{}, fmt.Errorf("security symbol %q must contain a six-digit code", raw)
	}
	if assetType == "" {
		assetType = inferAssetType(code)
	}
	if assetType != "STOCK" && assetType != "ETF" {
		return Canonical{}, fmt.Errorf("security symbol %q has unsupported asset type %q", raw, assetType)
	}

	exchange := "SZSE"
	suffix := "SZ"
	if market == "SH" {
		exchange = "SHSE"
		suffix = "SH"
	}
	return Canonical{
		Symbol:          code,
		TSCode:          code + "." + suffix,
		Market:          market,
		EastmoneySymbol: exchange + "." + code,
		AssetType:       assetType,
		BoardType:       boardType(market, code, assetType),
	}, nil
}

func UnitRule(boardType string) (TradingUnitRule, error) {
	switch strings.ToUpper(strings.TrimSpace(boardType)) {
	case BoardSTAR:
		return TradingUnitRule{
			MinimumBuyVolume: 200, BuyStep: 1,
			MinimumSellVolume: 200, SellStep: 1,
			MaximumBuyVolume: 100000, MaximumSellVolume: 100000,
		}, nil
	case BoardSHMain, BoardSZMain, BoardChiNext, BoardETF:
		return TradingUnitRule{
			MinimumBuyVolume: 100, BuyStep: 100,
			MinimumSellVolume: 100, SellStep: 100,
		}, nil
	default:
		return TradingUnitRule{}, fmt.Errorf("unsupported board type %q", boardType)
	}
}

func RoundBuyVolume(raw int64, rule TradingUnitRule) (int64, error) {
	if err := validateUnitRule(rule); err != nil {
		return 0, err
	}
	if raw < rule.MinimumBuyVolume {
		return 0, fmt.Errorf("volume is below the minimum buy volume")
	}
	volume := rule.MinimumBuyVolume + (raw-rule.MinimumBuyVolume)/rule.BuyStep*rule.BuyStep
	if rule.MaximumBuyVolume > 0 && volume > rule.MaximumBuyVolume {
		volume = rule.MaximumBuyVolume
		volume = rule.MinimumBuyVolume + (volume-rule.MinimumBuyVolume)/rule.BuyStep*rule.BuyStep
	}
	return volume, nil
}

func RoundSellVolume(target, available int64, rule TradingUnitRule) (int64, error) {
	if err := validateUnitRule(rule); err != nil {
		return 0, err
	}
	if available <= 0 {
		return 0, fmt.Errorf("no sellable position")
	}
	if target <= 0 || target > available {
		target = available
	}
	if available < rule.MinimumSellVolume {
		if target != available {
			return 0, fmt.Errorf("residual position must be sold in full")
		}
		return available, nil
	}
	volume := target
	if volume < rule.MinimumSellVolume {
		return 0, fmt.Errorf("volume is below the minimum sell volume")
	}
	volume = rule.MinimumSellVolume + (volume-rule.MinimumSellVolume)/rule.SellStep*rule.SellStep
	if rule.MaximumSellVolume > 0 && volume > rule.MaximumSellVolume {
		volume = rule.MaximumSellVolume
		volume = rule.MinimumSellVolume + (volume-rule.MinimumSellVolume)/rule.SellStep*rule.SellStep
	}
	return volume, nil
}

func ContainsBoard(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(target)) {
			return true
		}
	}
	return false
}

func splitSymbol(raw string) (string, string, error) {
	if strings.HasPrefix(raw, "SHSE.") || strings.HasPrefix(raw, "SZSE.") {
		parts := strings.Split(raw, ".")
		if len(parts) != 2 || !isSixDigitCode(parts[1]) {
			return "", "", fmt.Errorf("invalid Eastmoney security symbol %q", raw)
		}
		market := "SZ"
		if parts[0] == "SHSE" {
			market = "SH"
		}
		return parts[1], market, nil
	}
	parts := strings.Split(raw, ".")
	switch len(parts) {
	case 1:
		return parts[0], "", nil
	case 2:
		market := normalizeMarket(parts[1])
		if market == "" {
			return "", "", fmt.Errorf("invalid security market suffix in %q", raw)
		}
		return parts[0], market, nil
	default:
		return "", "", fmt.Errorf("invalid security symbol %q", raw)
	}
}

func boardType(market, code, assetType string) string {
	if assetType == "ETF" {
		return BoardETF
	}
	if market == "SZ" && (strings.HasPrefix(code, "300") || strings.HasPrefix(code, "301")) {
		return BoardChiNext
	}
	if market == "SH" && (strings.HasPrefix(code, "688") || strings.HasPrefix(code, "689")) {
		return BoardSTAR
	}
	if market == "SH" {
		return BoardSHMain
	}
	if market == "SZ" {
		return BoardSZMain
	}
	return BoardUnknown
}

func normalizeMarket(value string) string {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "SH", "SSE", "SHSE":
		return "SH"
	case "SZ", "SZSE":
		return "SZ"
	default:
		return ""
	}
}

func normalizeAssetType(value string) string {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "A_SHARE", "STOCK":
		return "STOCK"
	case "ETF":
		return "ETF"
	default:
		return strings.ToUpper(strings.TrimSpace(value))
	}
}

func inferMarket(code string) string {
	if code == "" {
		return ""
	}
	if strings.Contains("569", code[:1]) {
		return "SH"
	}
	if strings.Contains("0123", code[:1]) {
		return "SZ"
	}
	return ""
}

func inferAssetType(code string) string {
	if strings.HasPrefix(code, "5") || strings.HasPrefix(code, "15") || strings.HasPrefix(code, "16") {
		return "ETF"
	}
	return "STOCK"
}

func isSixDigitCode(value string) bool {
	if len(value) != 6 {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func validateUnitRule(rule TradingUnitRule) error {
	if rule.MinimumBuyVolume <= 0 || rule.BuyStep <= 0 || rule.MinimumSellVolume <= 0 || rule.SellStep <= 0 {
		return fmt.Errorf("trading unit rule is invalid")
	}
	return nil
}
