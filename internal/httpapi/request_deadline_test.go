package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"finance-sys/internal/config"
	"github.com/stretchr/testify/require"
)

func TestHTTPDeadlineDoesNotExtendParentBudget(t *testing.T) {
	cfg := &config.Config{}
	cfg.Service.HTTP.WriteTimeoutMS = 1000000
	s := &Server{runtime: config.NewRuntime(&config.Snapshot{Config: cfg})}
	deadline := time.Now().Add(time.Second)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	var observed time.Time
	handler := s.requestDeadlineMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observed, _ = r.Context().Deadline()
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx))
	require.Equal(t, deadline, observed)
}
