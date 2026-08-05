package httpapi

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParsePerformanceFilterIncludesBloggerNameAndPagination(t *testing.T) {
	request := httptest.NewRequest("GET", "/api/v1/recommendation-performance?blogger_name=%E5%B0%8F%E4%BD%9C%E6%96%87&date_from=2026-07-01&date_to=2026-07-26&limit=100&offset=200", nil)

	filter, err := parsePerformanceFilter(request)

	require.NoError(t, err)
	require.Equal(t, "小作文", filter.BloggerName)
	require.Equal(t, "2026-07-01", filter.DateFrom.Format("2006-01-02"))
	require.Equal(t, "2026-07-26", filter.DateTo.Format("2006-01-02"))
	require.Equal(t, 100, filter.Limit)
	require.Equal(t, 200, filter.Offset)
}
