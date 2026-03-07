package market

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"finance-sys/internal/domain"
)

type SinaHTTPProvider struct {
	*httpProvider
}

func NewSinaHTTPProvider(timeout time.Duration, limiter *rateLimiter, archiver RawArchiver) *SinaHTTPProvider {
	return &SinaHTTPProvider{
		httpProvider: newHTTPProvider("sina_http", timeout, limiter, archiver),
	}
}

func (p *SinaHTTPProvider) GetSecurityMaster(context.Context, string, string) ([]domain.Security, error) {
	return nil, ErrNotSupported
}

func (p *SinaHTTPProvider) GetRealtimeQuotes(ctx context.Context, symbols []string) ([]domain.Quote, error) {
	quotes := make([]domain.Quote, 0, len(symbols))
	for _, symbol := range symbols {
		end := time.Now()
		start := end.AddDate(0, 0, -5)
		bars, err := p.GetDailyBars(ctx, symbol, start, end, "")
		if err != nil || len(bars) == 0 {
			return nil, err
		}
		last := bars[len(bars)-1]
		prevClose := last.Open
		if len(bars) > 1 {
			prevClose = bars[len(bars)-2].Close
		}
		quotes = append(quotes, domain.Quote{
			Symbol:    symbol,
			TradeTime: last.TradeDate,
			Last:      last.Close,
			Open:      last.Open,
			High:      last.High,
			Low:       last.Low,
			PrevClose: prevClose,
			Volume:    last.Volume,
			Turnover:  last.Turnover,
		})
	}
	return quotes, nil
}

func (p *SinaHTTPProvider) GetDailyBars(ctx context.Context, symbol string, _ time.Time, _ time.Time, _ string) ([]domain.DailyBar, error) {
	body, _, err := p.get(ctx, "https://money.finance.sina.com.cn/quotes_service/api/json_v2.php/CN_MarketData.getKLineData", map[string]string{
		"symbol":  sinaSymbol(symbol),
		"scale":   "240",
		"ma":      "no",
		"datalen": "20",
	}, "daily_bars")
	if err != nil {
		return nil, err
	}

	var response []struct {
		Day    string `json:"day"`
		Open   string `json:"open"`
		High   string `json:"high"`
		Low    string `json:"low"`
		Close  string `json:"close"`
		Volume string `json:"volume"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("decode sina daily bars: %w", err)
	}
	if len(response) == 0 {
		return nil, fmt.Errorf("sina returned no bars for %s", symbol)
	}
	bars := make([]domain.DailyBar, 0, len(response))
	for _, item := range response {
		tradeDate, err := time.Parse("2006-01-02 15:04:05", item.Day)
		if err != nil {
			continue
		}
		openPrice, _ := strconv.ParseFloat(item.Open, 64)
		highPrice, _ := strconv.ParseFloat(item.High, 64)
		lowPrice, _ := strconv.ParseFloat(item.Low, 64)
		closePrice, _ := strconv.ParseFloat(item.Close, 64)
		volume, _ := strconv.ParseFloat(item.Volume, 64)
		bars = append(bars, domain.DailyBar{
			Symbol:    symbol,
			TradeDate: tradeDate,
			Open:      openPrice,
			High:      highPrice,
			Low:       lowPrice,
			Close:     closePrice,
			Volume:    volume,
			Turnover:  0,
		})
	}
	return bars, nil
}

func (p *SinaHTTPProvider) GetMinuteBars(context.Context, string, time.Time, time.Time, string, string) ([]domain.MinuteBar, error) {
	return nil, ErrNotSupported
}

func (p *SinaHTTPProvider) GetTradingCalendar(_ context.Context, start time.Time, end time.Time) ([]domain.TradingDay, error) {
	return weekdayCalendar(start, end), nil
}

func (p *SinaHTTPProvider) GetCorporateActions(context.Context, string, time.Time, time.Time) ([]domain.CorporateAction, error) {
	return nil, nil
}

func (p *SinaHTTPProvider) GetSuspensionStatus(_ context.Context, symbols []string, tradeDate time.Time) ([]domain.SuspensionStatus, error) {
	items := make([]domain.SuspensionStatus, 0, len(symbols))
	for _, symbol := range symbols {
		items = append(items, domain.SuspensionStatus{Symbol: symbol, TradeDate: tradeDate, Suspended: false})
	}
	return items, nil
}

func (p *SinaHTTPProvider) HealthCheck(ctx context.Context) error {
	_, err := p.GetTradingCalendar(ctx, time.Now(), time.Now().AddDate(0, 0, 1))
	return err
}

func sinaSymbol(symbol string) string {
	upper := strings.ToLower(symbol)
	code := strings.SplitN(upper, ".", 2)[0]
	if strings.HasSuffix(upper, ".sh") {
		return "sh" + code
	}
	return "sz" + code
}
