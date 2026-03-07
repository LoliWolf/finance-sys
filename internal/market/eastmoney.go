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

type EastmoneyHTTPProvider struct {
	*httpProvider
}

func NewEastmoneyHTTPProvider(timeout time.Duration, limiter *rateLimiter, archiver RawArchiver) *EastmoneyHTTPProvider {
	return &EastmoneyHTTPProvider{
		httpProvider: newHTTPProvider("eastmoney_http", timeout, limiter, archiver),
	}
}

func (p *EastmoneyHTTPProvider) GetSecurityMaster(context.Context, string, string) ([]domain.Security, error) {
	return nil, ErrNotSupported
}

func (p *EastmoneyHTTPProvider) GetRealtimeQuotes(ctx context.Context, symbols []string) ([]domain.Quote, error) {
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

func (p *EastmoneyHTTPProvider) GetDailyBars(ctx context.Context, symbol string, start time.Time, end time.Time, adjust string) ([]domain.DailyBar, error) {
	body, _, err := p.get(ctx, "https://push2his.eastmoney.com/api/qt/stock/kline/get", map[string]string{
		"secid":   secID(symbol),
		"fields1": "f1,f2,f3,f4,f5,f6",
		"fields2": "f51,f52,f53,f54,f55,f56,f57,f58",
		"klt":     "101",
		"fqt":     eastmoneyAdjust(adjust),
		"beg":     start.Format("20060102"),
		"end":     end.Format("20060102"),
	}, "daily_bars")
	if err != nil {
		return nil, err
	}

	var response struct {
		Data struct {
			Klines []string `json:"klines"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}
	bars := make([]domain.DailyBar, 0, len(response.Data.Klines))
	for _, line := range response.Data.Klines {
		bar, err := parseEastmoneyBar(symbol, line)
		if err != nil {
			continue
		}
		bars = append(bars, bar)
	}
	if len(bars) == 0 {
		return nil, fmt.Errorf("eastmoney returned no bars for %s", symbol)
	}
	return bars, nil
}

func (p *EastmoneyHTTPProvider) GetMinuteBars(context.Context, string, time.Time, time.Time, string, string) ([]domain.MinuteBar, error) {
	return nil, ErrNotSupported
}

func (p *EastmoneyHTTPProvider) GetTradingCalendar(_ context.Context, start time.Time, end time.Time) ([]domain.TradingDay, error) {
	return weekdayCalendar(start, end), nil
}

func (p *EastmoneyHTTPProvider) GetCorporateActions(context.Context, string, time.Time, time.Time) ([]domain.CorporateAction, error) {
	return nil, nil
}

func (p *EastmoneyHTTPProvider) GetSuspensionStatus(_ context.Context, symbols []string, tradeDate time.Time) ([]domain.SuspensionStatus, error) {
	items := make([]domain.SuspensionStatus, 0, len(symbols))
	for _, symbol := range symbols {
		items = append(items, domain.SuspensionStatus{Symbol: symbol, TradeDate: tradeDate, Suspended: false})
	}
	return items, nil
}

func (p *EastmoneyHTTPProvider) HealthCheck(ctx context.Context) error {
	_, err := p.GetTradingCalendar(ctx, time.Now(), time.Now().AddDate(0, 0, 1))
	return err
}

func parseEastmoneyBar(symbol string, line string) (domain.DailyBar, error) {
	fields := strings.Split(line, ",")
	if len(fields) < 7 {
		return domain.DailyBar{}, fmt.Errorf("unexpected kline payload: %s", line)
	}
	tradeDate, err := time.Parse("2006-01-02", fields[0])
	if err != nil {
		return domain.DailyBar{}, err
	}
	openPrice, err := strconv.ParseFloat(fields[1], 64)
	if err != nil {
		return domain.DailyBar{}, err
	}
	closePrice, err := strconv.ParseFloat(fields[2], 64)
	if err != nil {
		return domain.DailyBar{}, err
	}
	highPrice, err := strconv.ParseFloat(fields[3], 64)
	if err != nil {
		return domain.DailyBar{}, err
	}
	lowPrice, err := strconv.ParseFloat(fields[4], 64)
	if err != nil {
		return domain.DailyBar{}, err
	}
	volume, _ := strconv.ParseFloat(fields[5], 64)
	turnover, _ := strconv.ParseFloat(fields[6], 64)
	return domain.DailyBar{
		Symbol:    symbol,
		TradeDate: tradeDate,
		Open:      openPrice,
		High:      highPrice,
		Low:       lowPrice,
		Close:     closePrice,
		Volume:    volume,
		Turnover:  turnover,
	}, nil
}

func secID(symbol string) string {
	upper := strings.ToUpper(symbol)
	code := strings.SplitN(upper, ".", 2)[0]
	if strings.HasSuffix(upper, ".SH") {
		return "1." + code
	}
	return "0." + code
}

func eastmoneyAdjust(adjust string) string {
	switch strings.ToLower(adjust) {
	case "qfq":
		return "1"
	case "hfq":
		return "2"
	default:
		return "0"
	}
}
