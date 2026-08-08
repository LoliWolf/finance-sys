package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"finance-sys/internal/config"
	"finance-sys/internal/dal"
	"finance-sys/internal/domain/db_model"
	"finance-sys/internal/marketdata"

	"gorm.io/gorm"
)

const (
	SecurityMasterSourceTushare   = "TUSHARE"
	SecurityMasterSourceTushareDC = "TUSHARE_DC"
)

type SecurityMasterRefreshRequest struct {
	AsOfDate string `json:"as_of_date,omitempty"`
}

type SecurityMasterRefreshResponse struct {
	AsOfDate       string         `json:"as_of_date"`
	SectorDataDate string         `json:"sector_data_date"`
	FetchedCounts  map[string]int `json:"fetched_counts"`
	UpsertedCount  int            `json:"upserted_count"`
	AliasCount     int            `json:"alias_count"`
	TokenAlias     string         `json:"token_alias"`
}

type securityMasterAliasCandidate struct {
	TSCode     string
	Alias      string
	AliasType  string
	Confidence float64
}

func (s *MarketDataService) RefreshSecurityMaster(ctx context.Context, request SecurityMasterRefreshRequest) (*SecurityMasterRefreshResponse, error) {
	cfg, err := s.currentMarketDataConfig()
	if err != nil {
		return nil, err
	}
	if !cfg.Enabled || !cfg.SecurityMaster.Enabled {
		return nil, fmt.Errorf("market_data.security_master is disabled")
	}
	if s.masterProvider == nil {
		return nil, fmt.Errorf("security master provider is not configured")
	}
	tokens := enabledTushareTokens(cfg.Tushare.Tokens)
	if len(tokens) == 0 {
		return nil, fmt.Errorf("no enabled market_data.tushare token")
	}
	asOfDate, err := normalizeSecurityMasterAsOfDate(request.AsOfDate, s.runtime.Config())
	if err != nil {
		return nil, err
	}

	allModels := make([]db_model.SecurityMaster, 0, 9000)
	allAliases := make([]securityMasterAliasCandidate, 0, 3000)
	fetchedCounts := make(map[string]int)
	tokenAlias := ""
	fetchedCodes := map[string][]string{
		"STOCK":  nil,
		"ETF":    nil,
		"SECTOR": nil,
	}

	for _, status := range []string{"L", "D", "P"} {
		rows, alias, fetchErr := fetchSecurityMasterRowsWithTokens(ctx, tokens, func(ctx context.Context, token string) ([]marketdata.ProviderRow, error) {
			return s.masterProvider.FetchStockBasic(ctx, token, status, cfg.SecurityMaster.StockFields)
		})
		if fetchErr != nil {
			return nil, fmt.Errorf("fetch stock_basic list_status=%s: %w", status, fetchErr)
		}
		if alias != "" {
			tokenAlias = alias
		}
		fetchedCounts["stock_"+strings.ToLower(status)] = len(rows)
		models, aliases := securityMastersFromProviderRows(rows, "STOCK", status, SecurityMasterSourceTushare, asOfDate)
		allModels = append(allModels, models...)
		allAliases = append(allAliases, aliases...)
		fetchedCodes["STOCK"] = append(fetchedCodes["STOCK"], securityMasterTSCodes(models)...)
	}

	for _, status := range []string{"L", "D", "P"} {
		rows, alias, fetchErr := fetchSecurityMasterRowsWithTokens(ctx, tokens, func(ctx context.Context, token string) ([]marketdata.ProviderRow, error) {
			return s.masterProvider.FetchETFBasic(ctx, token, status, cfg.SecurityMaster.ETFFields)
		})
		if fetchErr != nil {
			return nil, fmt.Errorf("fetch etf_basic list_status=%s: %w", status, fetchErr)
		}
		if alias != "" {
			tokenAlias = alias
		}
		fetchedCounts["etf_"+strings.ToLower(status)] = len(rows)
		models, aliases := securityMastersFromProviderRows(rows, "ETF", status, SecurityMasterSourceTushare, asOfDate)
		allModels = append(allModels, models...)
		allAliases = append(allAliases, aliases...)
		fetchedCodes["ETF"] = append(fetchedCodes["ETF"], securityMasterTSCodes(models)...)
	}

	sectorRows, sectorDataDate, alias, err := s.fetchLatestDCIndex(ctx, tokens, asOfDate, cfg.SecurityMaster.SectorFields)
	if err != nil {
		return nil, err
	}
	if alias != "" {
		tokenAlias = alias
	}
	fetchedCounts["sector"] = len(sectorRows)
	sectorModels, sectorAliases := securityMastersFromProviderRows(sectorRows, "SECTOR", "L", SecurityMasterSourceTushareDC, asOfDate)
	allModels = append(allModels, sectorModels...)
	allAliases = append(allAliases, sectorAliases...)
	fetchedCodes["SECTOR"] = append(fetchedCodes["SECTOR"], securityMasterTSCodes(sectorModels)...)

	allModels = dedupeSecurityMasterModels(allModels)
	if len(allModels) == 0 || len(sectorModels) == 0 {
		return nil, fmt.Errorf("security master refresh returned an incomplete snapshot")
	}

	aliasCount := 0
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := dal.SecurityMasters.UpsertBatch(ctx, tx, allModels, 500); err != nil {
			return err
		}
		stored, err := dal.SecurityMasters.QueryByTSCodes(ctx, tx, securityMasterTSCodes(allModels))
		if err != nil {
			return err
		}
		aliases := securityAliasModels(stored, allAliases)
		if err := dal.SecurityAliases.UpsertBatch(ctx, tx, aliases, 500); err != nil {
			return err
		}
		aliasCount = len(aliases)
		if err := dal.SecurityMasters.DeactivateMissingBySourceAndAssetType(ctx, tx, SecurityMasterSourceTushare, "STOCK", uniqueStrings(fetchedCodes["STOCK"])); err != nil {
			return err
		}
		if err := dal.SecurityMasters.DeactivateMissingBySourceAndAssetType(ctx, tx, SecurityMasterSourceTushare, "ETF", uniqueStrings(fetchedCodes["ETF"])); err != nil {
			return err
		}
		return dal.SecurityMasters.DeactivateMissingBySourceAndAssetType(ctx, tx, SecurityMasterSourceTushareDC, "SECTOR", uniqueStrings(fetchedCodes["SECTOR"]))
	}); err != nil {
		return nil, fmt.Errorf("persist security master snapshot: %w", err)
	}

	return &SecurityMasterRefreshResponse{
		AsOfDate:       asOfDate.Format(time.DateOnly),
		SectorDataDate: sectorDataDate.Format(time.DateOnly),
		FetchedCounts:  fetchedCounts,
		UpsertedCount:  len(allModels),
		AliasCount:     aliasCount,
		TokenAlias:     tokenAlias,
	}, nil
}

func (s *MarketDataService) fetchLatestDCIndex(ctx context.Context, tokens []config.TushareTokenConfig, asOfDate time.Time, fields []string) ([]marketdata.ProviderRow, time.Time, string, error) {
	var lastErr error
	for offset := 0; offset < 14; offset++ {
		tradeDate := asOfDate.AddDate(0, 0, -offset)
		rows, alias, err := fetchSecurityMasterRowsWithTokens(ctx, tokens, func(ctx context.Context, token string) ([]marketdata.ProviderRow, error) {
			return s.masterProvider.FetchDCIndex(ctx, token, tradeDate, fields)
		})
		if err != nil {
			lastErr = err
			continue
		}
		if len(rows) > 0 {
			return rows, tradeDate, alias, nil
		}
	}
	if lastErr != nil {
		return nil, time.Time{}, "", fmt.Errorf("fetch dc_index: %w", lastErr)
	}
	return nil, time.Time{}, "", fmt.Errorf("dc_index returned no rows in the 14 days through %s", asOfDate.Format(time.DateOnly))
}

func fetchSecurityMasterRowsWithTokens(ctx context.Context, tokens []config.TushareTokenConfig, fetch func(context.Context, string) ([]marketdata.ProviderRow, error)) ([]marketdata.ProviderRow, string, error) {
	var lastErr error
	for _, token := range tokens {
		rows, err := fetch(ctx, strings.TrimSpace(token.Token))
		if err == nil {
			return rows, strings.TrimSpace(token.Alias), nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no enabled token")
	}
	return nil, "", lastErr
}

func normalizeSecurityMasterAsOfDate(value string, cfg *config.Config) (time.Time, error) {
	if strings.TrimSpace(value) != "" {
		parsed, err := time.Parse(time.DateOnly, strings.TrimSpace(value))
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid as_of_date %q, expected YYYY-MM-DD", value)
		}
		return dateOnlyUTC(parsed), nil
	}
	location := time.Local
	if cfg != nil && strings.TrimSpace(cfg.Meta.Timezone) != "" {
		if loaded, err := time.LoadLocation(cfg.Meta.Timezone); err == nil {
			location = loaded
		}
	}
	return dateOnlyUTC(time.Now().In(location)), nil
}

func securityMastersFromProviderRows(rows []marketdata.ProviderRow, assetType string, fallbackStatus string, source string, asOfDate time.Time) ([]db_model.SecurityMaster, []securityMasterAliasCandidate) {
	models := make([]db_model.SecurityMaster, 0, len(rows))
	aliases := make([]securityMasterAliasCandidate, 0, len(rows))
	for _, row := range rows {
		model, rowAliases, ok := securityMasterFromProviderRow(row, assetType, fallbackStatus, source, asOfDate)
		if !ok {
			continue
		}
		models = append(models, *model)
		aliases = append(aliases, rowAliases...)
	}
	return models, aliases
}

func securityMasterFromProviderRow(row marketdata.ProviderRow, assetType string, fallbackStatus string, source string, asOfDate time.Time) (*db_model.SecurityMaster, []securityMasterAliasCandidate, bool) {
	tsCode := strings.ToUpper(providerString(row, "ts_code"))
	if tsCode == "" {
		return nil, nil, false
	}
	name := providerString(row, "name")
	if assetType == "ETF" {
		name = firstProviderString(row, "csname", "name", "cname", "extname")
	}
	if name == "" {
		return nil, nil, false
	}
	symbol := firstProviderString(row, "symbol", "index_code")
	if symbol == "" {
		if dot := strings.Index(tsCode, "."); dot > 0 {
			symbol = tsCode[:dot]
		}
	}
	fullName := firstProviderString(row, "fullname", "full_name")
	if assetType == "ETF" {
		fullName = firstProviderString(row, "extname", "cname", "fullname", "full_name", "csname")
	}
	if fullName == "" {
		fullName = name
	}
	listStatus := strings.ToUpper(firstNonBlank(providerString(row, "list_status"), fallbackStatus, "L"))
	listDate := parseProviderDate(firstProviderString(row, "list_date", "setup_date", "found_date"))
	delistDate := parseProviderDate(firstProviderString(row, "delist_date", "exp_date", "due_date"))
	exchange := strings.ToUpper(firstNonBlank(providerString(row, "exchange"), exchangeFromProviderTSCode(tsCode)))
	market := marketFromProviderTSCode(tsCode)
	sectorType := ""
	if assetType == "SECTOR" {
		exchange = "DC"
		market = "DC"
		sectorType = normalizeSectorType(firstProviderString(row, "idx_type", "category", "index_type"))
	}
	isActive := listStatus == "L" && !dateAfter(listDate, asOfDate) && !dateOnOrBefore(delistDate, asOfDate)
	rawJSON, _ := json.Marshal(row.Values)
	if len(rawJSON) == 0 {
		rawJSON = []byte("{}")
	}
	model := &db_model.SecurityMaster{
		TSCode:     tsCode,
		Symbol:     symbol,
		Name:       name,
		FullName:   fullName,
		Exchange:   exchange,
		Market:     market,
		AssetType:  assetType,
		ListStatus: listStatus,
		ListDate:   listDate,
		DelistDate: delistDate,
		Industry:   providerString(row, "industry"),
		SectorType: sectorType,
		IsActive:   isActive,
		Source:     source,
		RawJSON:    rawJSON,
	}
	aliases := masterAliases(*model)
	return model, aliases, true
}

func masterAliases(model db_model.SecurityMaster) []securityMasterAliasCandidate {
	values := []securityMasterAliasCandidate{{TSCode: model.TSCode, Alias: model.FullName, AliasType: "FULL_NAME", Confidence: 1}}
	if model.AssetType == "SECTOR" {
		base := trimSectorSuffix(model.Name)
		variants := []string{model.Name, model.Name + "板块", base, base + "板块"}
		switch model.SectorType {
		case "CONCEPT":
			variants = append(variants, base+"概念", base+"概念板块", base+"主题")
		case "INDUSTRY":
			variants = append(variants, base+"行业", base+"行业板块")
		case "REGION":
			variants = append(variants, base+"地域", base+"区域板块")
		}
		for _, variant := range variants {
			values = append(values, securityMasterAliasCandidate{TSCode: model.TSCode, Alias: variant, AliasType: "SECTOR_VARIANT", Confidence: 0.95})
		}
	}
	return values
}

func securityAliasModels(stored []db_model.SecurityMaster, candidates []securityMasterAliasCandidate) []db_model.SecurityAlias {
	masterByCode := make(map[string]db_model.SecurityMaster, len(stored))
	for _, model := range stored {
		masterByCode[model.TSCode] = model
	}
	seen := make(map[string]struct{}, len(candidates))
	result := make([]db_model.SecurityAlias, 0, len(candidates))
	for _, candidate := range candidates {
		master, ok := masterByCode[candidate.TSCode]
		if !ok {
			continue
		}
		alias := strings.TrimSpace(candidate.Alias)
		normalized := NormalizeSecurityAlias(alias)
		if alias == "" || normalized == "" || normalized == NormalizeSecurityAlias(master.Name) {
			continue
		}
		key := fmt.Sprintf("%d|%s|%s", master.ID, normalized, candidate.AliasType)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, db_model.SecurityAlias{
			SecurityMasterID: master.ID,
			AliasName:        alias,
			NormalizedAlias:  normalized,
			AliasType:        candidate.AliasType,
			Source:           master.Source,
			Confidence:       candidate.Confidence,
			IsActive:         master.IsActive,
		})
	}
	return result
}

func dedupeSecurityMasterModels(models []db_model.SecurityMaster) []db_model.SecurityMaster {
	byCode := make(map[string]db_model.SecurityMaster, len(models))
	for _, model := range models {
		byCode[model.TSCode] = model
	}
	codes := make([]string, 0, len(byCode))
	for code := range byCode {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	result := make([]db_model.SecurityMaster, 0, len(codes))
	for _, code := range codes {
		result = append(result, byCode[code])
	}
	return result
}

func securityMasterTSCodes(models []db_model.SecurityMaster) []string {
	result := make([]string, 0, len(models))
	for _, model := range models {
		result = append(result, model.TSCode)
	}
	return result
}

func providerString(row marketdata.ProviderRow, key string) string {
	return stringValue(row.Values[key])
}

func firstProviderString(row marketdata.ProviderRow, keys ...string) string {
	for _, key := range keys {
		if value := providerString(row, key); value != "" && value != "<nil>" {
			return value
		}
	}
	return ""
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func parseProviderDate(value string) *time.Time {
	value = strings.TrimSpace(value)
	for _, layout := range []string{"20060102", time.DateOnly} {
		if parsed, err := time.Parse(layout, value); err == nil {
			date := dateOnlyUTC(parsed)
			return &date
		}
	}
	return nil
}

func exchangeFromProviderTSCode(tsCode string) string {
	if dot := strings.LastIndex(tsCode, "."); dot >= 0 {
		switch tsCode[dot+1:] {
		case "SH":
			return "SSE"
		case "SZ":
			return "SZSE"
		case "BJ":
			return "BSE"
		case "DC":
			return "DC"
		}
	}
	return ""
}

func marketFromProviderTSCode(tsCode string) string {
	if dot := strings.LastIndex(tsCode, "."); dot >= 0 {
		return strings.ToUpper(tsCode[dot+1:])
	}
	return ""
}

func normalizeSectorType(value string) string {
	upper := strings.ToUpper(strings.TrimSpace(value))
	switch {
	case strings.Contains(upper, "概念"), strings.Contains(upper, "CONCEPT"):
		return "CONCEPT"
	case strings.Contains(upper, "行业"), strings.Contains(upper, "INDUSTRY"):
		return "INDUSTRY"
	case strings.Contains(upper, "地域"), strings.Contains(upper, "地区"), strings.Contains(upper, "REGION"):
		return "REGION"
	default:
		return upper
	}
}

func trimSectorSuffix(value string) string {
	result := strings.TrimSpace(value)
	for _, suffix := range []string{"概念板块", "行业板块", "区域板块", "概念", "行业", "板块", "指数", "主题"} {
		result = strings.TrimSuffix(result, suffix)
	}
	return strings.TrimSpace(result)
}

func dateAfter(value *time.Time, target time.Time) bool {
	return value != nil && value.After(target)
}

func dateOnOrBefore(value *time.Time, target time.Time) bool {
	return value != nil && !value.After(target)
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok || value == "" {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
