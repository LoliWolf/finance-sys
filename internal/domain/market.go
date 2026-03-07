package domain

import "time"

type Security struct {
	Symbol      string `json:"symbol"`
	Name        string `json:"name"`
	Market      string `json:"market"`
	Kind        string `json:"kind"`
	ListingDate string `json:"listing_date"`
}

type Quote struct {
	Symbol    string    `json:"symbol"`
	TradeTime time.Time `json:"trade_time"`
	Last      float64   `json:"last"`
	Open      float64   `json:"open"`
	High      float64   `json:"high"`
	Low       float64   `json:"low"`
	PrevClose float64   `json:"prev_close"`
	Volume    float64   `json:"volume"`
	Turnover  float64   `json:"turnover"`
}

type DailyBar struct {
	Symbol    string    `json:"symbol"`
	TradeDate time.Time `json:"trade_date"`
	Open      float64   `json:"open"`
	High      float64   `json:"high"`
	Low       float64   `json:"low"`
	Close     float64   `json:"close"`
	Volume    float64   `json:"volume"`
	Turnover  float64   `json:"turnover"`
}

type MinuteBar struct {
	Symbol    string    `json:"symbol"`
	TradeTime time.Time `json:"trade_time"`
	Open      float64   `json:"open"`
	High      float64   `json:"high"`
	Low       float64   `json:"low"`
	Close     float64   `json:"close"`
	Volume    float64   `json:"volume"`
}

type TradingDay struct {
	Date      time.Time `json:"date"`
	IsTrading bool      `json:"is_trading"`
}

type CorporateAction struct {
	Symbol string    `json:"symbol"`
	Date   time.Time `json:"date"`
	Type   string    `json:"type"`
	Detail string    `json:"detail"`
}

type SuspensionStatus struct {
	Symbol    string    `json:"symbol"`
	TradeDate time.Time `json:"trade_date"`
	Suspended bool      `json:"suspended"`
	Reason    string    `json:"reason"`
}

type MarketSnapshot struct {
	ID                 int64     `json:"id"`
	Symbol             string    `json:"symbol"`
	TradeDate          time.Time `json:"trade_date"`
	Provider           string    `json:"provider"`
	Open               float64   `json:"open"`
	High               float64   `json:"high"`
	Low                float64   `json:"low"`
	Close              float64   `json:"close"`
	Volume             float64   `json:"volume"`
	Turnover           float64   `json:"turnover"`
	ATR                float64   `json:"atr"`
	PrevClose          float64   `json:"prev_close"`
	BenchmarkReturnPct float64   `json:"benchmark_return_pct"`
	RawObjectKey       string    `json:"raw_object_key"`
	ConfigVersion      int64     `json:"config_version"`
	CreatedAt          time.Time `json:"created_at"`
}
