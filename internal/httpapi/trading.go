package httpapi

import (
	"crypto/hmac"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	tradingdomain "finance-sys/internal/trading/domain"
	tradingservice "finance-sys/internal/trading/service"
	"finance-sys/internal/tradingbridgeclient"

	"github.com/go-chi/chi/v5"
)

func (s *Server) handleCreateTradingAgentRun(w http.ResponseWriter, r *http.Request) {
	if !s.requireTradingService(w) {
		return
	}
	var request tradingservice.RunRequest
	if err := decodeJSONBody(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	view, err := s.trading.StartRun(r.Context(), request)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusAccepted, view)
}

func (s *Server) handleGetTradingAgentRun(w http.ResponseWriter, r *http.Request) {
	if !s.requireTradingService(w) {
		return
	}
	id, err := parseID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	view, err := s.trading.GetRun(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleListTradingIntents(w http.ResponseWriter, r *http.Request) {
	if !s.requireTradingService(w) {
		return
	}
	items, err := s.trading.ListIntents(r.Context(), r.URL.Query().Get("status"), r.URL.Query().Get("symbol"), parseQueryInt(r, "limit", 100))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

func (s *Server) handleListTradingOrders(w http.ResponseWriter, r *http.Request) {
	if !s.requireTradingService(w) {
		return
	}
	items, err := s.trading.ListOrders(r.Context(), r.URL.Query().Get("status"), r.URL.Query().Get("symbol"), parseQueryInt(r, "limit", 100))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

func (s *Server) handleListTradingPositionCycles(w http.ResponseWriter, r *http.Request) {
	if !s.requireTradingService(w) {
		return
	}
	items, err := s.trading.ListPositionCycles(r.Context(), r.URL.Query().Get("status"), parseQueryInt(r, "limit", 100))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

func (s *Server) handleListTradingDailySessions(w http.ResponseWriter, r *http.Request) {
	if !s.requireTradingService(w) {
		return
	}
	items, err := s.trading.ListDailySessions(r.Context(), parseQueryInt(r, "limit", 30))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

func (s *Server) handleListTradingSkillDecisions(w http.ResponseWriter, r *http.Request) {
	if !s.requireTradingService(w) {
		return
	}
	id, err := parseID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	items, err := s.trading.ListSkillDecisions(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

func (s *Server) handleTradingPreflight(w http.ResponseWriter, r *http.Request) {
	if !s.requireTradingService(w) {
		return
	}
	session, err := s.trading.Preflight(r.Context(), time.Now())
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]any{"session": session, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, session)
}

func (s *Server) handleCancelTradingOrder(w http.ResponseWriter, r *http.Request) {
	if !s.requireTradingService(w) {
		return
	}
	order, err := s.trading.CancelOrder(r.Context(), chi.URLParam(r, "clientOrderID"))
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusAccepted, order)
}

func (s *Server) handleGetTradingAccount(w http.ResponseWriter, r *http.Request) {
	if !s.requireTradingService(w) {
		return
	}
	account, err := s.trading.LatestAccount(r.Context())
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, account)
}

func (s *Server) handleGetTradingPositions(w http.ResponseWriter, r *http.Request) {
	if !s.requireTradingService(w) {
		return
	}
	items, err := s.trading.LatestPositions(r.Context())
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

func (s *Server) handleGetTradingDashboard(w http.ResponseWriter, r *http.Request) {
	if !s.requireTradingService(w) {
		return
	}
	view, err := s.trading.TradingDashboard(r.Context(), r.URL.Query().Get("trade_date"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleRefreshTradingDashboard(w http.ResponseWriter, r *http.Request) {
	if !s.requireTradingService(w) {
		return
	}
	view, err := s.trading.RefreshTradingDashboard(r.Context(), r.URL.Query().Get("trade_date"))
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleCreateTradingReconciliationRun(w http.ResponseWriter, r *http.Request) {
	if !s.requireTradingService(w) {
		return
	}
	var request struct {
		RunType string `json:"run_type"`
	}
	if err := decodeJSONBody(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	run, err := s.trading.Reconcile(r.Context(), request.RunType)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"run": run, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, run)
}

func (s *Server) handleSetTradingKillSwitch(w http.ResponseWriter, r *http.Request) {
	if !s.requireTradingService(w) {
		return
	}
	var request tradingservice.KillSwitchRequest
	if err := decodeJSONBody(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.trading.SetKillSwitch(r.Context(), request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"kill_switch": request.Enabled})
}

func (s *Server) handleTradingBridgeEvent(w http.ResponseWriter, r *http.Request) {
	if !s.requireTradingService(w) {
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.verifyBridgeHMAC(r, body); err != nil {
		writeError(w, http.StatusUnauthorized, err)
		return
	}
	var event tradingdomain.BridgeEvent
	if err := json.Unmarshal(body, &event); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.trading.HandleBridgeEvent(r.Context(), event); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"event_hash": event.EventHash})
}

func (s *Server) handleTradingToolRecommendationCandidates(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeTradingAgent(w, r) || !s.requireTradingService(w) {
		return
	}
	asOf, err := parseQueryTime(r.URL.Query().Get("as_of_time"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	afterID, _ := strconv.ParseInt(r.URL.Query().Get("after_id"), 10, 64)
	result, err := s.trading.RecommendationCandidates(r.Context(), asOf, afterID, parseQueryInt(r, "limit", 20))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleTradingToolBloggerPerformance(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeTradingAgent(w, r) || !s.requireTradingService(w) {
		return
	}
	id, err := parseID(chi.URLParam(r, "bloggerID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	result, err := s.trading.BloggerPerformance(r.Context(), id, parseQueryInt(r, "window_days", 30))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleTradingToolMarketSnapshot(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeTradingAgent(w, r) || !s.requireTradingService(w) {
		return
	}
	asOf, err := parseQueryTime(r.URL.Query().Get("as_of_time"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	symbols := splitNonEmpty(r.URL.Query().Get("symbols"))
	result, err := s.trading.MarketSnapshots(r.Context(), symbols, asOf)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": result})
}

func (s *Server) handleTradingToolDailyHistory(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeTradingAgent(w, r) || !s.requireTradingService(w) {
		return
	}
	asOf, err := parseQueryTime(r.URL.Query().Get("as_of_time"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	result, err := s.trading.DailyHistory(r.Context(), splitNonEmpty(r.URL.Query().Get("symbols")), asOf, parseQueryInt(r, "limit", 20))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": result})
}

func (s *Server) handleTradingToolPortfolio(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeTradingAgent(w, r) || !s.requireTradingService(w) {
		return
	}
	result, err := s.trading.Portfolio(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleTradingToolRiskBudget(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeTradingAgent(w, r) || !s.requireTradingService(w) {
		return
	}
	result, err := s.trading.RiskBudget(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) requireTradingService(w http.ResponseWriter) bool {
	if s.trading == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("trading service unavailable"))
		return false
	}
	return true
}

func (s *Server) authorizeTradingAgent(w http.ResponseWriter, r *http.Request) bool {
	cfg := s.runtime.Config()
	if cfg == nil || cfg.Trading.Agent.InternalToken == "" || !hmac.Equal([]byte(r.Header.Get("Authorization")), []byte("Bearer "+cfg.Trading.Agent.InternalToken)) {
		writeError(w, http.StatusUnauthorized, errors.New("unauthorized trading agent"))
		return false
	}
	return true
}

func (s *Server) verifyBridgeHMAC(r *http.Request, body []byte) error {
	cfg := s.runtime.Config()
	if cfg == nil || cfg.Trading.Bridge.HMAC.Secret == "" {
		return errors.New("Bridge HMAC is not configured")
	}
	if r.Header.Get("X-FS-Key-Id") != cfg.Trading.Bridge.HMAC.KeyID {
		return errors.New("unknown Bridge key id")
	}
	timestampRaw := r.Header.Get("X-FS-Timestamp")
	timestamp, err := strconv.ParseInt(timestampRaw, 10, 64)
	if err != nil {
		return errors.New("invalid Bridge timestamp")
	}
	skew := time.Since(time.UnixMilli(timestamp))
	if skew < 0 {
		skew = -skew
	}
	if skew > time.Duration(cfg.Trading.Bridge.HMAC.MaxClockSkewSeconds)*time.Second {
		return errors.New("Bridge timestamp outside allowed clock skew")
	}
	nonce := r.Header.Get("X-FS-Nonce")
	if nonce == "" {
		return errors.New("missing Bridge nonce")
	}
	signature, err := hex.DecodeString(r.Header.Get("X-FS-Signature"))
	if err != nil {
		return errors.New("invalid Bridge signature encoding")
	}
	canonical := tradingbridgeclient.CanonicalString(r.Method, r.URL.Path, cloneURLValues(r.URL.Query()), body, timestampRaw, nonce)
	expected, _ := hex.DecodeString(tradingbridgeclient.Sign(cfg.Trading.Bridge.HMAC.Secret, canonical))
	if !hmac.Equal(signature, expected) {
		return errors.New("invalid Bridge signature")
	}
	now := time.Now()
	s.bridgeNonceMu.Lock()
	defer s.bridgeNonceMu.Unlock()
	for value, expires := range s.bridgeNonces {
		if !expires.After(now) {
			delete(s.bridgeNonces, value)
		}
	}
	if expires, exists := s.bridgeNonces[nonce]; exists && expires.After(now) {
		return errors.New("replayed Bridge nonce")
	}
	s.bridgeNonces[nonce] = now.Add(time.Duration(cfg.Trading.Bridge.HMAC.NonceTTLSeconds) * time.Second)
	return nil
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, target any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return nil
}

func parseQueryInt(r *http.Request, name string, defaultValue int) int {
	value, err := strconv.Atoi(r.URL.Query().Get(name))
	if err != nil {
		return defaultValue
	}
	return value
}

func parseQueryTime(raw string) (time.Time, error) {
	if strings.TrimSpace(raw) == "" {
		return time.Now(), nil
	}
	value, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, errors.New("as_of_time must use RFC3339")
	}
	return value, nil
}

func splitNonEmpty(raw string) []string {
	parts := strings.Split(raw, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		if value := strings.TrimSpace(part); value != "" {
			values = append(values, value)
		}
	}
	return values
}

func cloneURLValues(values url.Values) url.Values {
	clone := make(url.Values, len(values))
	for key, items := range values {
		clone[key] = append([]string(nil), items...)
	}
	return clone
}
