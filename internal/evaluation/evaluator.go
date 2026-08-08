package evaluation

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

type Status string

const (
	StatusReady       Status = "READY"
	StatusPending     Status = "PENDING"
	StatusIncomplete  Status = "INCOMPLETE"
	StatusNoSecurity  Status = "NO_SECURITY"
	StatusUnsupported Status = "UNSUPPORTED"
	StatusFailed      Status = "FAILED"
)

const (
	ReasonWindowNotMatured  = "WINDOW_NOT_MATURED"
	ReasonQuoteGap          = "QUOTE_GAP"
	ReasonEntryQuoteMissing = "ENTRY_QUOTE_MISSING"
	ReasonExitQuoteMissing  = "EXIT_QUOTE_MISSING"
	ReasonNoQuote           = "NO_QUOTE"
	ReasonInvalidPrice      = "INVALID_PRICE"
	ReasonInvalidWindow     = "INVALID_WINDOW"
	ReasonUnsupported       = "UNSUPPORTED_DIRECTION"
)

type Quote struct {
	TradeDate time.Time
	Open      float64
	High      float64
	Low       float64
	Close     float64
}

type Input struct {
	RecommendDate         time.Time
	Direction             string
	WindowDays            int
	Quotes                []Quote
	MarketTradingDates    []time.Time
	LatestMarketDate      time.Time
	WinThresholdRatio     float64
	MinQuoteCoverageRatio float64
}

type Result struct {
	Status                  Status
	ReasonCode              string
	ReasonMessage           string
	BaseDate                *time.Time
	BaseClosePrice          *float64
	EntryDate               *time.Time
	EntryPrice              *float64
	ExitDate                *time.Time
	ExitClosePrice          *float64
	ExpectedQuoteCount      int
	ActualQuoteCount        int
	MissingQuoteCount       int
	RawReturnRatio          *float64
	DirectionReturnRatio    *float64
	MaxFavorableReturnRatio *float64
	MaxAdverseReturnRatio   *float64
	MaxDrawdownRatio        *float64
	WinFlag                 *bool
	BestTradeDate           *time.Time
	WorstTradeDate          *time.Time
}

// EvaluateWindow calculates a single recommendation window without external IO.
// MarketTradingDates define the fixed trading-day boundary. Quotes missing because
// of suspension or incomplete data never extend the window to a later date.
func EvaluateWindow(input Input) Result {
	result := Result{ExpectedQuoteCount: input.WindowDays}
	if input.WindowDays <= 0 {
		return failed(result, ReasonInvalidWindow, "window_days must be positive")
	}

	direction := strings.ToUpper(strings.TrimSpace(input.Direction))
	if direction != "LONG" && direction != "SHORT" {
		result.Status = StatusUnsupported
		result.ReasonCode = ReasonUnsupported
		result.ReasonMessage = fmt.Sprintf("direction %q is not supported", input.Direction)
		return result
	}

	recommendDate := dateOnly(input.RecommendDate)
	quotes := normalizeQuotes(input.Quotes)
	quotesByDate := indexQuotesByDate(quotes)
	tradingDates := tradingDatesAfter(input.MarketTradingDates, recommendDate)
	baseIndex := firstQuoteOnOrAfter(quotes, recommendDate)
	if baseIndex >= 0 {
		base := quotes[baseIndex]
		result.BaseDate = timePtr(base.TradeDate)
		result.BaseClosePrice = floatPtr(base.Close)
	}

	elapsedTradingDayCount := min(len(tradingDates), input.WindowDays)
	elapsedTradingDates := tradingDates[:elapsedTradingDayCount]
	result.ActualQuoteCount = countQuotesOnDates(quotesByDate, elapsedTradingDates)
	result.MissingQuoteCount = max(input.WindowDays-result.ActualQuoteCount, 0)
	if len(tradingDates) > 0 {
		entryDate := tradingDates[0]
		result.EntryDate = timePtr(entryDate)
		if entry, exists := quotesByDate[entryDate]; exists {
			result.EntryPrice = floatPtr(entry.Open)
		}
	}
	if len(tradingDates) < input.WindowDays {
		result.Status = StatusPending
		result.ReasonCode = ReasonWindowNotMatured
		result.ReasonMessage = fmt.Sprintf("only %d of %d market trading days have elapsed", len(tradingDates), input.WindowDays)
		return result
	}

	windowTradingDates := tradingDates[:input.WindowDays]
	entryDate := windowTradingDates[0]
	exitDate := windowTradingDates[len(windowTradingDates)-1]
	result.EntryDate = timePtr(entryDate)
	result.ExitDate = timePtr(exitDate)

	entry, entryExists := quotesByDate[entryDate]
	if !entryExists {
		result.Status = StatusIncomplete
		result.ReasonCode = ReasonEntryQuoteMissing
		result.ReasonMessage = fmt.Sprintf("entry quote is missing on fixed market trading date %s", entryDate.Format(time.DateOnly))
		return result
	}
	result.EntryPrice = floatPtr(entry.Open)

	exit, exitExists := quotesByDate[exitDate]
	if !exitExists {
		result.Status = StatusIncomplete
		result.ReasonCode = ReasonExitQuoteMissing
		result.ReasonMessage = fmt.Sprintf("exit quote is missing on fixed market trading date %s", exitDate.Format(time.DateOnly))
		return result
	}
	result.ExitClosePrice = floatPtr(exit.Close)

	windowQuotes := quotesOnDates(quotesByDate, windowTradingDates)
	coverage := float64(result.ActualQuoteCount) / float64(input.WindowDays)
	if coverage < normalizedCoverage(input.MinQuoteCoverageRatio) {
		result.Status = StatusIncomplete
		result.ReasonCode = ReasonQuoteGap
		result.ReasonMessage = fmt.Sprintf("quote coverage %.4f is below required %.4f within fixed market window", coverage, normalizedCoverage(input.MinQuoteCoverageRatio))
		return result
	}

	if baseIndex < 0 || !validPrice(quotes[baseIndex].Close) || !validPrice(entry.Open) {
		return failed(result, ReasonInvalidPrice, "base close price and entry open price must be positive finite values")
	}
	for _, quote := range windowQuotes {
		if !validQuote(quote) {
			return failed(result, ReasonInvalidPrice, fmt.Sprintf("invalid OHLC price on %s", quote.TradeDate.Format(time.DateOnly)))
		}
	}

	rawReturn := exit.Close/entry.Open - 1
	directionReturn := rawReturn
	if direction == "SHORT" {
		directionReturn = entry.Open/exit.Close - 1
	}

	bestRatio, adverseRatio, bestDate, worstDate := extremes(direction, entry.Open, windowQuotes)
	maxDrawdown := calculateMaxDrawdown(direction, entry.Open, windowQuotes)
	win := directionReturn > input.WinThresholdRatio

	result.Status = StatusReady
	result.RawReturnRatio = floatPtr(rawReturn)
	result.DirectionReturnRatio = floatPtr(directionReturn)
	result.MaxFavorableReturnRatio = floatPtr(bestRatio)
	result.MaxAdverseReturnRatio = floatPtr(adverseRatio)
	result.MaxDrawdownRatio = floatPtr(maxDrawdown)
	result.WinFlag = boolPtr(win)
	result.BestTradeDate = timePtr(bestDate)
	result.WorstTradeDate = timePtr(worstDate)
	return result
}

func indexQuotesByDate(quotes []Quote) map[time.Time]Quote {
	result := make(map[time.Time]Quote, len(quotes))
	for _, quote := range quotes {
		result[quote.TradeDate] = quote
	}
	return result
}

func tradingDatesAfter(values []time.Time, date time.Time) []time.Time {
	seen := make(map[time.Time]struct{}, len(values))
	result := make([]time.Time, 0, len(values))
	for _, value := range values {
		tradingDate := dateOnly(value)
		if tradingDate.IsZero() || !tradingDate.After(date) {
			continue
		}
		if _, exists := seen[tradingDate]; exists {
			continue
		}
		seen[tradingDate] = struct{}{}
		result = append(result, tradingDate)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Before(result[j])
	})
	return result
}

func countQuotesOnDates(quotesByDate map[time.Time]Quote, dates []time.Time) int {
	count := 0
	for _, date := range dates {
		if _, exists := quotesByDate[date]; exists {
			count++
		}
	}
	return count
}

func quotesOnDates(quotesByDate map[time.Time]Quote, dates []time.Time) []Quote {
	result := make([]Quote, 0, len(dates))
	for _, date := range dates {
		if quote, exists := quotesByDate[date]; exists {
			result = append(result, quote)
		}
	}
	return result
}

func normalizeQuotes(quotes []Quote) []Quote {
	if len(quotes) == 0 {
		return nil
	}
	byDate := make(map[time.Time]Quote, len(quotes))
	for _, quote := range quotes {
		quote.TradeDate = dateOnly(quote.TradeDate)
		byDate[quote.TradeDate] = quote
	}
	result := make([]Quote, 0, len(byDate))
	for _, quote := range byDate {
		result = append(result, quote)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].TradeDate.Before(result[j].TradeDate)
	})
	return result
}

func firstQuoteOnOrAfter(quotes []Quote, date time.Time) int {
	for index, quote := range quotes {
		if !quote.TradeDate.Before(date) {
			return index
		}
	}
	return -1
}

func extremes(direction string, entryPrice float64, quotes []Quote) (float64, float64, time.Time, time.Time) {
	bestDate := quotes[0].TradeDate
	worstDate := quotes[0].TradeDate
	if direction == "SHORT" {
		minLow := quotes[0].Low
		maxHigh := quotes[0].High
		for _, quote := range quotes[1:] {
			if quote.Low < minLow {
				minLow = quote.Low
				bestDate = quote.TradeDate
			}
			if quote.High > maxHigh {
				maxHigh = quote.High
				worstDate = quote.TradeDate
			}
		}
		return entryPrice/minLow - 1, entryPrice/maxHigh - 1, bestDate, worstDate
	}

	maxHigh := quotes[0].High
	minLow := quotes[0].Low
	for _, quote := range quotes[1:] {
		if quote.High > maxHigh {
			maxHigh = quote.High
			bestDate = quote.TradeDate
		}
		if quote.Low < minLow {
			minLow = quote.Low
			worstDate = quote.TradeDate
		}
	}
	return maxHigh/entryPrice - 1, minLow/entryPrice - 1, bestDate, worstDate
}

func calculateMaxDrawdown(direction string, entryPrice float64, quotes []Quote) float64 {
	peak := 1.0
	maxDrawdown := 0.0
	for _, quote := range quotes {
		equity := quote.Close / entryPrice
		if direction == "SHORT" {
			equity = entryPrice / quote.Close
		}
		if equity > peak {
			peak = equity
		}
		drawdown := equity/peak - 1
		if drawdown < maxDrawdown {
			maxDrawdown = drawdown
		}
	}
	return maxDrawdown
}

func normalizedCoverage(value float64) float64 {
	if value <= 0 || value > 1 {
		return 1
	}
	return value
}

func validQuote(quote Quote) bool {
	return validPrice(quote.Open) && validPrice(quote.High) && validPrice(quote.Low) && validPrice(quote.Close) && quote.High >= quote.Low
}

func validPrice(value float64) bool {
	return value > 0 && !math.IsInf(value, 0) && !math.IsNaN(value)
}

func failed(result Result, code string, message string) Result {
	result.Status = StatusFailed
	result.ReasonCode = code
	result.ReasonMessage = message
	return result
}

func dateOnly(value time.Time) time.Time {
	if value.IsZero() {
		return time.Time{}
	}
	year, month, day := value.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func timePtr(value time.Time) *time.Time {
	result := value
	return &result
}

func floatPtr(value float64) *float64 {
	result := value
	return &result
}

func boolPtr(value bool) *bool {
	result := value
	return &result
}

func min(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func max(left, right int) int {
	if left > right {
		return left
	}
	return right
}
