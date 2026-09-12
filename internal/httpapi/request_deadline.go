package httpapi

import (
	"context"
	"net/http"
	"time"
)

// All downstream stages inherit the HTTP deadline, including OCR and the Agent.
func (s *Server) requestDeadlineMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.runtime == nil || s.runtime.Config() == nil || s.runtime.Config().Service.HTTP.WriteTimeoutMS <= 0 {
			next.ServeHTTP(w, r)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), time.Duration(s.runtime.Config().Service.HTTP.WriteTimeoutMS)*time.Millisecond)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
