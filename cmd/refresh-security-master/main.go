package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"finance-sys/internal/bootstrap"
	"finance-sys/internal/config"
	"finance-sys/internal/service"
)

func main() {
	var asOfDate string
	var allowProduction bool
	var timeout time.Duration
	flag.StringVar(&asOfDate, "as-of-date", "", "snapshot date in YYYY-MM-DD; defaults to today")
	flag.BoolVar(&allowProduction, "allow-production", false, "allow production writes")
	flag.DurationVar(&timeout, "timeout", 30*time.Minute, "overall timeout")
	flag.Parse()

	if strings.EqualFold(strings.TrimSpace(os.Getenv(config.FinanceSysEnvironmentVariable)), "PROD") && !allowProduction {
		fatal(errors.New("-allow-production is required when FINANCE_SYS_ENV=PROD"))
	}
	if err := bootstrap.LoadNacosServerAddressFromFiles("bootstrap_go122.env", "bootstrap_go122.env.example"); err != nil {
		fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	app, err := bootstrap.Build(ctx)
	if err != nil {
		fatal(err)
	}
	defer app.Close()

	result, err := app.MarketDataService.RefreshSecurityMaster(ctx, service.SecurityMasterRefreshRequest{AsOfDate: asOfDate})
	if err != nil {
		fatal(err)
	}
	fmt.Printf("security_master_refresh_completed as_of_date=%s sector_data_date=%s upserted=%d aliases=%d fetched=%v token_alias=%s\n",
		result.AsOfDate, result.SectorDataDate, result.UpsertedCount, result.AliasCount, result.FetchedCounts, result.TokenAlias)
}

func fatal(err error) {
	_, _ = fmt.Fprintln(os.Stderr, "refresh security master:", err)
	os.Exit(1)
}
