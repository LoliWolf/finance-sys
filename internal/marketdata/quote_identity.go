package marketdata

import (
	"errors"
	"fmt"
	"time"
)

var ErrInvalidDailyIdentity = errors.New("invalid daily quote identity")

// ValidateDailyIdentity binds provider prices to their original code and date.
func ValidateDailyIdentity(tsCode string, tradeDate time.Time, values map[string]any) error {
	rawCode, ok := values["ts_code"].(string)
	if !ok || rawCode == "" || rawCode != tsCode {
		return fmt.Errorf("%w: ts_code does not match %q", ErrInvalidDailyIdentity, tsCode)
	}
	rawDate, ok := values["trade_date"].(string)
	if !ok || tradeDate.IsZero() || rawDate != tradeDate.Format("20060102") {
		return fmt.Errorf("%w: trade_date does not match %s for %s", ErrInvalidDailyIdentity, tradeDate.Format(time.DateOnly), tsCode)
	}
	return nil
}
