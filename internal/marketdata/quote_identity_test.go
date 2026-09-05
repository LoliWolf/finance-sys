package marketdata

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestValidateDailyIdentity(t *testing.T) {
	date := time.Date(2026, 7, 6, 0, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		name  string
		code  any
		date  any
		valid bool
	}{
		{name: "valid", code: "300343.SZ", date: "20260706", valid: true},
		{name: "wrong code", code: "300006.SZ", date: "20260706"},
		{name: "missing code", date: "20260706"},
		{name: "wrong date", code: "300343.SZ", date: "20260707"},
		{name: "missing date", code: "300343.SZ"},
		{name: "non string date", code: "300343.SZ", date: 20260706},
		{name: "invalid date format", code: "300343.SZ", date: "2026-07-06"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDailyIdentity("300343.SZ", date, map[string]any{"ts_code": tt.code, "trade_date": tt.date})
			if tt.valid {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, ErrInvalidDailyIdentity)
			}
		})
	}
	require.ErrorIs(t, ValidateDailyIdentity("300343.SZ", date, nil), ErrInvalidDailyIdentity)
	require.ErrorIs(t, ValidateDailyIdentity("300343.SZ", time.Time{}, map[string]any{"ts_code": "300343.SZ", "trade_date": "00010101"}), ErrInvalidDailyIdentity)
}
