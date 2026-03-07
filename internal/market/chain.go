package market

import (
	"context"
	"fmt"
	"strings"
	"time"

	"finance-sys/internal/config"
	"finance-sys/internal/domain"
	"finance-sys/internal/storage"
)

type Chain struct {
	providers []Provider
	breakers  map[string]*circuitBreaker
	cache     *memoryCache
}

func NewChain(cfg config.MarketConfig, storageCfg config.ObjectStorageConfig, objectStore storage.ObjectStorage) *Chain {
	timeout := time.Duration(cfg.ProviderTimeoutMS) * time.Millisecond
	limiter := newRateLimiter(cfg.RateLimit.RequestsPerSecond, cfg.RateLimit.Burst)
	archiver := NewStorageArchiver(objectStore, storageCfg.BucketRawMarket)

	providers := make([]Provider, 0, 5)
	breakers := make(map[string]*circuitBreaker)
	addProvider := func(provider Provider) {
		providers = append(providers, provider)
		breakers[provider.Name()] = newCircuitBreaker(cfg.CircuitBreaker.FailureThreshold, time.Duration(cfg.CircuitBreaker.HalfOpenAfterSeconds)*time.Second)
	}

	switch strings.ToLower(cfg.PrimaryProvider) {
	case "eastmoney_http":
		addProvider(NewEastmoneyHTTPProvider(timeout, limiter, archiver))
	case "sina_http":
		addProvider(NewSinaHTTPProvider(timeout, limiter, archiver))
	}
	for _, name := range cfg.FallbackProviders {
		switch strings.ToLower(name) {
		case "eastmoney_http":
			addProvider(NewEastmoneyHTTPProvider(timeout, limiter, archiver))
		case "sina_http":
			addProvider(NewSinaHTTPProvider(timeout, limiter, archiver))
		case "akshare_bridge":
			addProvider(AkshareBridgeProvider{})
		case "baostock_bridge":
			addProvider(BaostockBridgeProvider{})
		case "mcp_market":
			addProvider(MCPMarketProvider{})
		}
	}
	return &Chain{
		providers: dedupeProviders(providers),
		breakers:  breakers,
		cache:     newMemoryCache(),
	}
}

func (c *Chain) Name() string {
	return "provider_chain"
}

func (c *Chain) GetSecurityMaster(ctx context.Context, marketName string, kind string) ([]domain.Security, error) {
	return doWithFallback(c, ctx, "security_master:"+marketName+":"+kind, func(ctx context.Context, provider Provider) ([]domain.Security, error) {
		return provider.GetSecurityMaster(ctx, marketName, kind)
	})
}

func (c *Chain) GetRealtimeQuotes(ctx context.Context, symbols []string) ([]domain.Quote, error) {
	key := "quotes:" + strings.Join(symbols, ",")
	return doWithFallback(c, ctx, key, func(ctx context.Context, provider Provider) ([]domain.Quote, error) {
		return provider.GetRealtimeQuotes(ctx, symbols)
	})
}

func (c *Chain) GetDailyBars(ctx context.Context, symbol string, start time.Time, end time.Time, adjust string) ([]domain.DailyBar, error) {
	key := fmt.Sprintf("daily:%s:%s:%s:%s", symbol, start.Format(time.DateOnly), end.Format(time.DateOnly), adjust)
	return doWithFallback(c, ctx, key, func(ctx context.Context, provider Provider) ([]domain.DailyBar, error) {
		return provider.GetDailyBars(ctx, symbol, start, end, adjust)
	})
}

func (c *Chain) GetMinuteBars(ctx context.Context, symbol string, start time.Time, end time.Time, interval string, adjust string) ([]domain.MinuteBar, error) {
	key := fmt.Sprintf("minute:%s:%s:%s:%s:%s", symbol, start.Format(time.DateOnly), end.Format(time.DateOnly), interval, adjust)
	return doWithFallback(c, ctx, key, func(ctx context.Context, provider Provider) ([]domain.MinuteBar, error) {
		return provider.GetMinuteBars(ctx, symbol, start, end, interval, adjust)
	})
}

func (c *Chain) GetTradingCalendar(ctx context.Context, start time.Time, end time.Time) ([]domain.TradingDay, error) {
	key := fmt.Sprintf("calendar:%s:%s", start.Format(time.DateOnly), end.Format(time.DateOnly))
	return doWithFallback(c, ctx, key, func(ctx context.Context, provider Provider) ([]domain.TradingDay, error) {
		return provider.GetTradingCalendar(ctx, start, end)
	})
}

func (c *Chain) GetCorporateActions(ctx context.Context, symbol string, start time.Time, end time.Time) ([]domain.CorporateAction, error) {
	key := fmt.Sprintf("actions:%s:%s:%s", symbol, start.Format(time.DateOnly), end.Format(time.DateOnly))
	return doWithFallback(c, ctx, key, func(ctx context.Context, provider Provider) ([]domain.CorporateAction, error) {
		return provider.GetCorporateActions(ctx, symbol, start, end)
	})
}

func (c *Chain) GetSuspensionStatus(ctx context.Context, symbols []string, tradeDate time.Time) ([]domain.SuspensionStatus, error) {
	key := "suspension:" + tradeDate.Format(time.DateOnly) + ":" + strings.Join(symbols, ",")
	return doWithFallback(c, ctx, key, func(ctx context.Context, provider Provider) ([]domain.SuspensionStatus, error) {
		return provider.GetSuspensionStatus(ctx, symbols, tradeDate)
	})
}

func (c *Chain) HealthCheck(ctx context.Context) error {
	for _, provider := range c.providers {
		err := provider.HealthCheck(ctx)
		if err == nil || errorsIsNotSupported(err) {
			continue
		}
		return err
	}
	return nil
}

func doWithFallback[T any](chain *Chain, ctx context.Context, cacheKey string, fn func(context.Context, Provider) (T, error)) (T, error) {
	var zero T
	if cached, ok := chain.cache.Get(cacheKey); ok {
		if typed, ok := cached.(T); ok {
			return typed, nil
		}
	}

	var lastErr error
	for _, provider := range chain.providers {
		breaker := chain.breakers[provider.Name()]
		if breaker != nil && !breaker.Allow() {
			continue
		}
		value, err := fn(ctx, provider)
		if err == nil {
			if breaker != nil {
				breaker.Success()
			}
			chain.cache.Set(cacheKey, value, 5*time.Minute)
			return value, nil
		}
		lastErr = err
		if breaker != nil {
			breaker.Failure()
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no market provider available")
	}
	return zero, lastErr
}

func dedupeProviders(providers []Provider) []Provider {
	seen := make(map[string]struct{})
	items := make([]Provider, 0, len(providers))
	for _, provider := range providers {
		if _, ok := seen[provider.Name()]; ok {
			continue
		}
		seen[provider.Name()] = struct{}{}
		items = append(items, provider)
	}
	return items
}

func errorsIsNotSupported(err error) bool {
	return err == ErrNotSupported
}

func weekdayCalendar(start time.Time, end time.Time) []domain.TradingDay {
	var days []domain.TradingDay
	for current := start; !current.After(end); current = current.Add(24 * time.Hour) {
		isTrading := current.Weekday() != time.Saturday && current.Weekday() != time.Sunday
		days = append(days, domain.TradingDay{
			Date:      current,
			IsTrading: isTrading,
		})
	}
	return days
}
