package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
)

func TestTargetRecoveryHandlerIsLocalOnlyAndRejectsUnknownFields(t *testing.T) {
	for _, tt := range []struct {
		name, remote, body string
		status             int
	}{
		{"remote caller", "192.0.2.1:1000", "{}", http.StatusForbidden},
		{"forwarded remote caller", "192.0.2.1:1000", "{}", http.StatusForbidden},
		{"unknown field", "127.0.0.1:1000", `{"entry_price":100}`, http.StatusBadRequest},
		{"multiple values", "127.0.0.1:1000", `{} {}`, http.StatusBadRequest},
		{"oversize", "127.0.0.1:1000", `{"note":"` + strings.Repeat("x", 64*1024) + `"}`, http.StatusBadRequest},
		{"local no service", "127.0.0.1:1000", `{}`, http.StatusServiceUnavailable},
		{"ipv6 local no service", "[::1]:1000", `{}`, http.StatusServiceUnavailable},
	} {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tt.body))
			request.RemoteAddr = tt.remote
			request.Header.Set("X-Forwarded-For", "127.0.0.1")
			route := chi.NewRouteContext()
			route.URLParams.Add("id", "7")
			route.URLParams.Add("targetID", "4")
			request = request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, route))
			recorder := httptest.NewRecorder()
			(&Server{}).handleRecoverUntrackableTarget(recorder, request)
			require.Equal(t, tt.status, recorder.Code)
		})
	}
}
