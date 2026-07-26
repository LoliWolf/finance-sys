package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"finance-sys/internal/bootstrap"
	"finance-sys/internal/config"
	"finance-sys/internal/dal"
	"finance-sys/internal/domain/db_model"
	"finance-sys/internal/nacoscfg"
	"finance-sys/internal/service"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

const (
	sourceTushare           = "TUSHARE"
	aliasTypeFullName       = "FULL_NAME"
	aliasTypeHistoricalName = "HISTORICAL_NAME"
	stockBasicFields        = "ts_code,symbol,name,area,industry,fullname,enname,cnspell,market,exchange,curr_type,list_status,list_date,delist_date,is_hs,act_name,act_ent_type"
)

type options struct {
	bootstrapEnv   string
	pythonPath     string
	skillScript    string
	outDir         string
	stockStatuses  string
	dryRun         bool
	skipETF        bool
	skipNamechange bool
}

type runner struct {
	options options
	token   string
	db      *gorm.DB
	cache   map[string]*db_model.SecurityMaster
	missing map[string]struct{}
}

type summary struct {
	StockRowsByStatus map[string]int
	ETFRows           int
	NamechangeRows    int
	MasterUpserts     int
	AliasUpserts      int
	SkippedRows       int
	OptionalFailures  []string
	OutputDir         string
}

type aliasCandidate struct {
	TSCode     string
	Alias      string
	AliasType  string
	Confidence float64
}

func main() {
	ctx := context.Background()
	opts := parseFlags()

	if opts.bootstrapEnv != "" {
		if err := bootstrap.LoadNacosServerAddressFromFiles(opts.bootstrapEnv); err != nil {
			fatal(err)
		}
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	snapshot, err := loadNacosSnapshot(ctx, logger)
	if err != nil {
		fatal(err)
	}

	token := strings.TrimSpace(snapshot.Config.Agent.Tushare.Token)
	if token == "" {
		fatal(errors.New("agent.tushare.token is empty in Nacos config"))
	}
	if !snapshot.Config.Agent.Tushare.Enabled {
		fmt.Fprintln(os.Stderr, "warning: agent.tushare.enabled is false; using agent.tushare.token for one-time initialization")
	}

	if opts.pythonPath == "" {
		opts.pythonPath = detectPython()
	}
	if opts.skillScript == "" {
		opts.skillScript = filepath.Join("agent", "skills", "tushare", "scripts", "tushare_call.py")
	}
	if opts.outDir == "" {
		opts.outDir = filepath.Join("tmp", "tushare-init", time.Now().Format("20060102-150405"))
	}

	if opts.dryRun {
		r := &runner{options: opts, token: token, cache: map[string]*db_model.SecurityMaster{}, missing: map[string]struct{}{}}
		if err := r.runSkillPreflight(ctx); err != nil {
			fatal(err)
		}
		fmt.Printf("dry_run_ok config_source=%s config_version=%d\n", snapshot.Source, snapshot.Config.Meta.ConfigVersion)
		return
	}

	db, err := openDB(ctx, snapshot.Config)
	if err != nil {
		fatal(fmt.Errorf("open db: %w", err))
	}
	sqlDB, err := db.DB()
	if err == nil {
		defer sqlDB.Close()
	}

	r := &runner{
		options: opts,
		token:   token,
		db:      db,
		cache:   map[string]*db_model.SecurityMaster{},
		missing: map[string]struct{}{},
	}
	result, err := r.run(ctx)
	if err != nil {
		fatal(err)
	}

	fmt.Printf("tushare_security_init_completed config_source=%s config_version=%d output_dir=%s\n", snapshot.Source, snapshot.Config.Meta.ConfigVersion, result.OutputDir)
	fmt.Printf("processed stock=%v etf=%d namechange=%d master_upserts=%d alias_upserts=%d skipped_rows=%d\n",
		result.StockRowsByStatus,
		result.ETFRows,
		result.NamechangeRows,
		result.MasterUpserts,
		result.AliasUpserts,
		result.SkippedRows,
	)
	if len(result.OptionalFailures) > 0 {
		fmt.Println("optional_failures:")
		for _, item := range result.OptionalFailures {
			fmt.Printf("- %s\n", item)
		}
	}
	if err := printDBCounts(ctx, db); err != nil {
		fatal(err)
	}
}

func parseFlags() options {
	defaultEnv := defaultBootstrapEnvPath()
	opts := options{}
	flag.StringVar(&opts.bootstrapEnv, "bootstrap-env", defaultEnv, "Nacos bootstrap env file. Empty disables file loading.")
	flag.StringVar(&opts.pythonPath, "python", "", "Python executable. Defaults to the platform-specific agent/.venv interpreter, then python3/python on PATH.")
	flag.StringVar(&opts.skillScript, "skill-script", filepath.Join("agent", "skills", "tushare", "scripts", "tushare_call.py"), "Tushare skill CLI script path.")
	flag.StringVar(&opts.outDir, "out-dir", "", "Directory for Tushare JSON exports. Defaults to tmp/tushare-init/<timestamp>.")
	flag.StringVar(&opts.stockStatuses, "stock-statuses", "L,D,P", "Comma-separated stock_basic list_status values.")
	flag.BoolVar(&opts.dryRun, "dry-run", false, "Run skill catalog and parameter dry-runs only; do not call Tushare data APIs or write DB.")
	flag.BoolVar(&opts.skipETF, "skip-etf", false, "Skip etf_basic initialization.")
	flag.BoolVar(&opts.skipNamechange, "skip-namechange", false, "Skip namechange alias initialization.")
	flag.Parse()
	return opts
}

func defaultBootstrapEnvPath() string {
	if fileExists("bootstrap_go122.env") {
		return "bootstrap_go122.env"
	}
	if fileExists("bootstrap_go122.env.example") {
		return "bootstrap_go122.env.example"
	}
	return ""
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func loadNacosSnapshot(ctx context.Context, logger *slog.Logger) (*config.Snapshot, error) {
	bootstrapCfg, err := bootstrap.LoadNacosBootstrapFromEnv()
	if err != nil {
		return nil, err
	}
	loader := nacoscfg.NewLoader(bootstrapCfg, logger)
	return loader.Load(ctx, false, true)
}

func openDB(ctx context.Context, cfg *config.Config) (*gorm.DB, error) {
	db, err := gorm.Open(mysql.Open(cfg.Database.DSN), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		return nil, err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(cfg.Database.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.Database.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(time.Duration(cfg.Database.ConnMaxLifetimeMinutes) * time.Minute)
	sqlDB.SetConnMaxIdleTime(time.Duration(cfg.Database.ConnMaxIdleTimeMinutes) * time.Minute)
	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	return db, nil
}

func detectPython() string {
	for _, candidate := range venvPythonCandidates(runtime.GOOS) {
		if fileExists(candidate) {
			return candidate
		}
	}
	for _, name := range []string{"python3", "python"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	if runtime.GOOS == "windows" {
		return "python"
	}
	return "python3"
}

func venvPythonCandidates(goos string) []string {
	if goos == "windows" {
		return []string{filepath.Join("agent", ".venv", "Scripts", "python.exe")}
	}
	return []string{
		filepath.Join("agent", ".venv", "bin", "python3"),
		filepath.Join("agent", ".venv", "bin", "python"),
	}
}

func (r *runner) run(ctx context.Context) (summary, error) {
	if err := r.runSkillPreflight(ctx); err != nil {
		return summary{}, err
	}
	if err := os.MkdirAll(r.options.outDir, 0o755); err != nil {
		return summary{}, err
	}

	result := summary{
		StockRowsByStatus: map[string]int{},
		OutputDir:         r.options.outDir,
	}

	for _, status := range splitCSV(r.options.stockStatuses) {
		records, err := r.fetchAPIWithFields(ctx, "stock_basic", map[string]string{"list_status": status}, stockBasicFields)
		if err != nil {
			if status == "L" {
				return result, fmt.Errorf("stock_basic list_status=%s: %w", status, err)
			}
			result.OptionalFailures = append(result.OptionalFailures, fmt.Sprintf("stock_basic list_status=%s: %v", status, err))
			continue
		}
		result.StockRowsByStatus[status] = len(records)
		stats, err := r.upsertSecurityMasters(ctx, records, "STOCK", status)
		if err != nil {
			return result, fmt.Errorf("upsert stock_basic list_status=%s: %w", status, err)
		}
		result.MasterUpserts += stats.masterUpserts
		result.AliasUpserts += stats.aliasUpserts
		result.SkippedRows += stats.skippedRows
	}

	if !r.options.skipETF {
		records, err := r.fetchAPI(ctx, "etf_basic", nil)
		if err != nil {
			result.OptionalFailures = append(result.OptionalFailures, fmt.Sprintf("etf_basic: %v", err))
			records, err = r.fetchAPI(ctx, "fund_basic", map[string]string{"market": "E"})
			if err != nil {
				result.OptionalFailures = append(result.OptionalFailures, fmt.Sprintf("fund_basic market=E ETF fallback: %v", err))
			} else {
				records = filterRecords(records, isFundETFRecord)
			}
		}
		if err == nil {
			if len(records) == 0 {
				result.OptionalFailures = append(result.OptionalFailures, "ETF initialization returned no rows")
			}
			result.ETFRows = len(records)
			stats, err := r.upsertSecurityMasters(ctx, records, "ETF", "L")
			if err != nil {
				return result, fmt.Errorf("upsert ETF rows: %w", err)
			}
			result.MasterUpserts += stats.masterUpserts
			result.AliasUpserts += stats.aliasUpserts
			result.SkippedRows += stats.skippedRows
		}
	}

	if !r.options.skipNamechange {
		records, err := r.fetchAPI(ctx, "namechange", nil)
		if err != nil {
			result.OptionalFailures = append(result.OptionalFailures, fmt.Sprintf("namechange: %v", err))
		} else {
			result.NamechangeRows = len(records)
			aliasUpserts, skipped, err := r.upsertNamechangeAliases(ctx, records)
			if err != nil {
				return result, fmt.Errorf("upsert namechange aliases: %w", err)
			}
			result.AliasUpserts += aliasUpserts
			result.SkippedRows += skipped
		}
	}

	return result, nil
}

func (r *runner) runSkillPreflight(ctx context.Context) error {
	apis := []string{"stock_basic"}
	if !r.options.skipETF {
		apis = append(apis, "etf_basic")
		apis = append(apis, "fund_basic")
	}
	if !r.options.skipNamechange {
		apis = append(apis, "namechange")
	}
	for _, api := range apis {
		if _, _, err := r.runSkill(ctx, []string{"--show", api}); err != nil {
			return fmt.Errorf("tushare skill --show %s: %w", api, err)
		}
	}
	for _, status := range splitCSV(r.options.stockStatuses) {
		args := append([]string{"stock_basic", "--dry-run", "--fields", stockBasicFields}, paramsArgs(map[string]string{"list_status": status})...)
		if _, _, err := r.runSkill(ctx, args); err != nil {
			return fmt.Errorf("tushare skill dry-run stock_basic list_status=%s: %w", status, err)
		}
	}
	if !r.options.skipETF {
		if _, _, err := r.runSkill(ctx, []string{"etf_basic", "--dry-run"}); err != nil {
			return fmt.Errorf("tushare skill dry-run etf_basic: %w", err)
		}
		args := append([]string{"fund_basic", "--dry-run"}, paramsArgs(map[string]string{"market": "E"})...)
		if _, _, err := r.runSkill(ctx, args); err != nil {
			return fmt.Errorf("tushare skill dry-run fund_basic market=E: %w", err)
		}
	}
	if !r.options.skipNamechange {
		if _, _, err := r.runSkill(ctx, []string{"namechange", "--dry-run"}); err != nil {
			return fmt.Errorf("tushare skill dry-run namechange: %w", err)
		}
	}
	return nil
}

func (r *runner) fetchAPI(ctx context.Context, api string, params map[string]string) ([]map[string]any, error) {
	return r.fetchAPIWithFields(ctx, api, params, "")
}

func (r *runner) fetchAPIWithFields(ctx context.Context, api string, params map[string]string, fields string) ([]map[string]any, error) {
	output := filepath.Join(r.options.outDir, api+"-"+paramsSuffix(params)+".json")
	args := []string{api, "--token", r.token}
	args = append(args, paramsArgs(params)...)
	if fields != "" {
		args = append(args, "--fields", fields)
	}
	args = append(args, "--output", output, "--format", "json")
	if _, _, err := r.runSkill(ctx, args); err != nil {
		return nil, err
	}
	return readJSONRecords(output)
}

func (r *runner) runSkill(ctx context.Context, args []string) (string, string, error) {
	commandArgs := append([]string{r.options.skillScript}, args...)
	cmd := exec.CommandContext(ctx, r.options.pythonPath, commandArgs...)
	cmd.Env = append(os.Environ(), "PYTHONIOENCODING=utf-8")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return stdout.String(), stderr.String(), fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), stderr.String(), nil
}

func paramsArgs(params map[string]string) []string {
	if len(params) == 0 {
		return nil
	}
	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	args := make([]string, 0, len(keys)*2)
	for _, key := range keys {
		args = append(args, "--param", key+"="+params[key])
	}
	return args
}

func paramsSuffix(params map[string]string) string {
	if len(params) == 0 {
		return "all"
	}
	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		value := strings.NewReplacer("/", "_", "\\", "_", ":", "_", ".", "_").Replace(params[key])
		parts = append(parts, key+"-"+value)
	}
	return strings.Join(parts, "_")
}

func readJSONRecords(path string) ([]map[string]any, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var records []map[string]any
	if err := json.Unmarshal(raw, &records); err != nil {
		return nil, err
	}
	return records, nil
}

type upsertStats struct {
	masterUpserts int
	aliasUpserts  int
	skippedRows   int
}

func (r *runner) upsertSecurityMasters(ctx context.Context, records []map[string]any, assetType string, fallbackStatus string) (upsertStats, error) {
	var stats upsertStats
	for _, record := range records {
		model, aliases, ok := securityMasterFromRecord(record, assetType, fallbackStatus)
		if !ok {
			stats.skippedRows++
			continue
		}
		if err := dal.SecurityMasters.UpsertByTSCode(ctx, r.db, model); err != nil {
			return stats, err
		}
		stats.masterUpserts++
		stored, err := dal.SecurityMasters.QueryByTSCode(ctx, r.db, model.TSCode)
		if err != nil {
			return stats, err
		}
		r.cache[stored.TSCode] = stored
		for _, alias := range aliases {
			ok, err := r.upsertAlias(ctx, stored, alias)
			if err != nil {
				return stats, err
			}
			if ok {
				stats.aliasUpserts++
			} else {
				stats.skippedRows++
			}
		}
	}
	return stats, nil
}

func securityMasterFromRecord(record map[string]any, assetType string, fallbackStatus string) (*db_model.SecurityMaster, []aliasCandidate, bool) {
	tsCode := strings.ToUpper(stringValue(record, "ts_code"))
	name := stringValue(record, "name")
	if tsCode == "" || name == "" {
		return nil, nil, false
	}
	symbol := stringValue(record, "symbol")
	if symbol == "" {
		symbol = symbolFromTSCode(tsCode)
	}
	if symbol == "" {
		return nil, nil, false
	}

	fullName := firstNonEmpty(
		stringValue(record, "fullname"),
		stringValue(record, "full_name"),
		stringValue(record, "fund_full_name"),
		name,
	)
	listStatus := firstNonEmpty(
		stringValue(record, "list_status"),
		stringValue(record, "status"),
		fallbackStatus,
		"L",
	)
	exchange := firstNonEmpty(stringValue(record, "exchange"), exchangeFromTSCode(tsCode))
	market := firstNonEmpty(marketFromTSCode(tsCode), marketFromExchange(exchange), stringValue(record, "market"))
	listDate := parseDatePtr(firstNonEmpty(stringValue(record, "list_date"), stringValue(record, "found_date")))
	delistDate := parseDatePtr(firstNonEmpty(stringValue(record, "delist_date"), stringValue(record, "due_date")))
	rawJSON, _ := json.Marshal(record)
	if len(rawJSON) == 0 {
		rawJSON = []byte("{}")
	}

	model := &db_model.SecurityMaster{
		TSCode:     limitRunes(tsCode, 16),
		Symbol:     limitRunes(symbol, 16),
		Name:       limitRunes(name, 128),
		FullName:   limitRunes(fullName, 255),
		Exchange:   limitRunes(exchange, 16),
		Market:     limitRunes(strings.ToUpper(market), 16),
		AssetType:  limitRunes(strings.ToUpper(assetType), 32),
		ListStatus: limitRunes(strings.ToUpper(listStatus), 8),
		ListDate:   listDate,
		DelistDate: delistDate,
		Industry:   limitRunes(stringValue(record, "industry"), 128),
		IsActive:   strings.EqualFold(listStatus, "L") && delistDate == nil,
		Source:     sourceTushare,
		RawJSON:    rawJSON,
	}

	aliases := make([]aliasCandidate, 0, 1)
	if fullName != "" && service.NormalizeSecurityAlias(fullName) != service.NormalizeSecurityAlias(name) {
		aliases = append(aliases, aliasCandidate{
			TSCode:     model.TSCode,
			Alias:      fullName,
			AliasType:  aliasTypeFullName,
			Confidence: 1,
		})
	}
	return model, aliases, true
}

func (r *runner) upsertNamechangeAliases(ctx context.Context, records []map[string]any) (int, int, error) {
	upserts := 0
	skipped := 0
	for _, record := range records {
		tsCode := strings.ToUpper(stringValue(record, "ts_code"))
		alias := stringValue(record, "name")
		if tsCode == "" || alias == "" {
			skipped++
			continue
		}
		security, err := r.securityForTSCode(ctx, tsCode)
		if err != nil {
			if errors.Is(err, dal.ErrNotFound) {
				skipped++
				continue
			}
			return upserts, skipped, err
		}
		ok, err := r.upsertAlias(ctx, security, aliasCandidate{
			TSCode:     tsCode,
			Alias:      alias,
			AliasType:  aliasTypeHistoricalName,
			Confidence: 0.95,
		})
		if err != nil {
			return upserts, skipped, err
		}
		if ok {
			upserts++
		} else {
			skipped++
		}
	}
	return upserts, skipped, nil
}

func (r *runner) securityForTSCode(ctx context.Context, tsCode string) (*db_model.SecurityMaster, error) {
	if row, ok := r.cache[tsCode]; ok {
		return row, nil
	}
	if _, ok := r.missing[tsCode]; ok {
		return nil, dal.ErrNotFound
	}
	row, err := dal.SecurityMasters.QueryByTSCode(ctx, r.db, tsCode)
	if err != nil {
		if errors.Is(err, dal.ErrNotFound) {
			r.missing[tsCode] = struct{}{}
		}
		return nil, err
	}
	r.cache[tsCode] = row
	return row, nil
}

func filterRecords(records []map[string]any, keep func(map[string]any) bool) []map[string]any {
	filtered := records[:0]
	for _, record := range records {
		if keep(record) {
			filtered = append(filtered, record)
		}
	}
	return filtered
}

func isFundETFRecord(record map[string]any) bool {
	text := strings.ToUpper(strings.Join([]string{
		stringValue(record, "name"),
		stringValue(record, "fullname"),
		stringValue(record, "full_name"),
		stringValue(record, "fund_type"),
		stringValue(record, "type"),
		stringValue(record, "invest_type"),
	}, " "))
	tsCode := strings.ToUpper(stringValue(record, "ts_code"))
	return strings.Contains(text, "ETF") && (strings.HasSuffix(tsCode, ".SH") || strings.HasSuffix(tsCode, ".SZ"))
}

func (r *runner) upsertAlias(ctx context.Context, security *db_model.SecurityMaster, alias aliasCandidate) (bool, error) {
	value := strings.TrimSpace(alias.Alias)
	if value == "" || len([]rune(value)) > 128 {
		return false, nil
	}
	normalized := service.NormalizeSecurityAlias(value)
	if normalized == "" || normalized == service.NormalizeSecurityAlias(security.Name) {
		return false, nil
	}
	model := &db_model.SecurityAlias{
		SecurityMasterID: security.ID,
		AliasName:        value,
		NormalizedAlias:  limitRunes(normalized, 128),
		AliasType:        limitRunes(alias.AliasType, 32),
		Source:           sourceTushare,
		Confidence:       alias.Confidence,
		IsActive:         security.IsActive && strings.EqualFold(security.ListStatus, "L"),
	}
	return true, dal.SecurityAliases.UpsertByAliasAndSecurityID(ctx, r.db, model)
}

func stringValue(record map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := record[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case string:
			if trimmed := strings.TrimSpace(typed); trimmed != "" {
				return trimmed
			}
		case float64:
			if math.Trunc(typed) == typed {
				return strconv.FormatInt(int64(typed), 10)
			}
			return strconv.FormatFloat(typed, 'f', -1, 64)
		default:
			if trimmed := strings.TrimSpace(fmt.Sprint(typed)); trimmed != "" && trimmed != "<nil>" {
				return trimmed
			}
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func symbolFromTSCode(tsCode string) string {
	before, _, found := strings.Cut(tsCode, ".")
	if found {
		return before
	}
	return ""
}

func exchangeFromTSCode(tsCode string) string {
	switch {
	case strings.HasSuffix(tsCode, ".SH"):
		return "SSE"
	case strings.HasSuffix(tsCode, ".SZ"):
		return "SZSE"
	case strings.HasSuffix(tsCode, ".BJ"):
		return "BSE"
	default:
		return ""
	}
}

func marketFromTSCode(tsCode string) string {
	switch {
	case strings.HasSuffix(tsCode, ".SH"):
		return "SH"
	case strings.HasSuffix(tsCode, ".SZ"):
		return "SZ"
	case strings.HasSuffix(tsCode, ".BJ"):
		return "BJ"
	default:
		return ""
	}
}

func marketFromExchange(exchange string) string {
	switch strings.ToUpper(strings.TrimSpace(exchange)) {
	case "SSE", "SH", "XSHG":
		return "SH"
	case "SZSE", "SZ", "XSHE":
		return "SZ"
	case "BSE", "BJ", "XBJE":
		return "BJ"
	default:
		return ""
	}
}

func parseDatePtr(value string) *time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if len(value) >= 10 && value[4] == '-' && value[7] == '-' {
		if parsed, err := time.Parse("2006-01-02", value[:10]); err == nil {
			return &parsed
		}
	}
	digits := make([]rune, 0, len(value))
	for _, item := range value {
		if item >= '0' && item <= '9' {
			digits = append(digits, item)
		}
	}
	if len(digits) >= 8 {
		if parsed, err := time.Parse("20060102", string(digits[:8])); err == nil {
			return &parsed
		}
	}
	return nil
}

func limitRunes(value string, max int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max])
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.ToUpper(strings.TrimSpace(part))
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

func printDBCounts(ctx context.Context, db *gorm.DB) error {
	type masterCount struct {
		AssetType  string
		ListStatus string
		Count      int64
	}
	type aliasCount struct {
		AliasType string
		Count     int64
	}

	var masterCounts []masterCount
	if err := db.WithContext(ctx).
		Model(&db_model.SecurityMaster{}).
		Select("asset_type, list_status, COUNT(*) AS count").
		Where("source = ?", sourceTushare).
		Group("asset_type, list_status").
		Order("asset_type ASC, list_status ASC").
		Scan(&masterCounts).Error; err != nil {
		return err
	}
	var aliasCounts []aliasCount
	if err := db.WithContext(ctx).
		Model(&db_model.SecurityAlias{}).
		Select("alias_type, COUNT(*) AS count").
		Where("source = ?", sourceTushare).
		Group("alias_type").
		Order("alias_type ASC").
		Scan(&aliasCounts).Error; err != nil {
		return err
	}

	fmt.Println("db_counts security_master source=TUSHARE")
	for _, item := range masterCounts {
		fmt.Printf("- asset_type=%s list_status=%s count=%d\n", item.AssetType, item.ListStatus, item.Count)
	}
	fmt.Println("db_counts security_aliases source=TUSHARE")
	for _, item := range aliasCounts {
		fmt.Printf("- alias_type=%s count=%d\n", item.AliasType, item.Count)
	}
	return nil
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "error: %v\n", err)
	os.Exit(1)
}
