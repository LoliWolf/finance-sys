package marketdata

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	tushare "github.com/yushikuann/go-tushare-sdk/client"
)

type ProviderRow struct {
	Fields []string
	Values map[string]any
}

type StockDailyProvider interface {
	FetchStockDaily(ctx context.Context, token string, tradeDate time.Time, fields []string) ([]ProviderRow, error)
	FetchETFDaily(ctx context.Context, token string, tradeDate time.Time, fields []string) ([]ProviderRow, error)
	FetchSectorDaily(ctx context.Context, token string, tradeDate time.Time, fields []string) ([]ProviderRow, error)
}

type SecurityMasterProvider interface {
	FetchStockBasic(ctx context.Context, token string, listStatus string, fields []string) ([]ProviderRow, error)
	FetchETFBasic(ctx context.Context, token string, listStatus string, fields []string) ([]ProviderRow, error)
	FetchDCIndex(ctx context.Context, token string, tradeDate time.Time, fields []string) ([]ProviderRow, error)
}

type TushareProvider struct {
	httpClient *http.Client
}

func NewTushareProvider(httpClient *http.Client) *TushareProvider {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &TushareProvider{httpClient: httpClient}
}

func (p *TushareProvider) FetchStockDaily(ctx context.Context, token string, tradeDate time.Time, fields []string) ([]ProviderRow, error) {
	client := tushare.NewWithClient(strings.TrimSpace(token), p.httpClient)
	response, err := client.Daily(map[string]string{"trade_date": tradeDate.Format("20060102")}, fields)
	if err != nil {
		return nil, err
	}
	return providerRowsFromSDKResponse(response)
}

func (p *TushareProvider) FetchETFDaily(ctx context.Context, token string, tradeDate time.Time, fields []string) ([]ProviderRow, error) {
	return p.queryGeneric(ctx, "fund_daily", strings.TrimSpace(token), map[string]string{
		"trade_date": tradeDate.Format("20060102"),
	}, fields)
}

func (p *TushareProvider) FetchSectorDaily(ctx context.Context, token string, tradeDate time.Time, fields []string) ([]ProviderRow, error) {
	return p.queryGeneric(ctx, "dc_daily", strings.TrimSpace(token), map[string]string{
		"trade_date": tradeDate.Format("20060102"),
	}, fields)
}

func (p *TushareProvider) FetchStockBasic(ctx context.Context, token string, listStatus string, fields []string) ([]ProviderRow, error) {
	return p.queryGeneric(ctx, "stock_basic", strings.TrimSpace(token), map[string]string{
		"list_status": strings.ToUpper(strings.TrimSpace(listStatus)),
	}, fields)
}

func (p *TushareProvider) FetchETFBasic(ctx context.Context, token string, listStatus string, fields []string) ([]ProviderRow, error) {
	return p.queryGeneric(ctx, "etf_basic", strings.TrimSpace(token), map[string]string{
		"list_status": strings.ToUpper(strings.TrimSpace(listStatus)),
	}, fields)
}

func (p *TushareProvider) FetchDCIndex(ctx context.Context, token string, tradeDate time.Time, fields []string) ([]ProviderRow, error) {
	return p.queryGeneric(ctx, "dc_index", strings.TrimSpace(token), map[string]string{
		"trade_date": tradeDate.Format("20060102"),
	}, fields)
}

func (p *TushareProvider) queryGeneric(ctx context.Context, apiName string, token string, params map[string]string, fields []string) ([]ProviderRow, error) {
	raw, err := json.Marshal(map[string]any{
		"api_name": apiName,
		"token":    token,
		"params":   params,
		"fields":   fields,
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tushare.Domain, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tushare http %d for %s", resp.StatusCode, apiName)
	}
	var response tushare.APIResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}
	if response.Code != 0 {
		return nil, fmt.Errorf("tushare api %s returned code %d: %v", apiName, response.Code, response.Msg)
	}
	return providerRowsFromSDKResponse(&response)
}

func providerRowsFromSDKResponse(response *tushare.APIResponse) ([]ProviderRow, error) {
	if response == nil {
		return nil, fmt.Errorf("tushare response is nil")
	}
	rows := make([]ProviderRow, 0, len(response.Data.Items))
	for i, item := range response.Data.Items {
		if len(item) != len(response.Data.Fields) {
			return nil, fmt.Errorf("tushare row %d has %d values, expected %d", i, len(item), len(response.Data.Fields))
		}
		values := make(map[string]any, len(response.Data.Fields))
		for j, field := range response.Data.Fields {
			values[field] = item[j]
		}
		rows = append(rows, ProviderRow{
			Fields: response.Data.Fields,
			Values: values,
		})
	}
	return rows, nil
}
