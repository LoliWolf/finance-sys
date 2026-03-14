package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"finance-sys/internal/config"
	"finance-sys/internal/domain"
)

const systemPrompt = `You extract structured T+1 trade intents from Chinese research text.
Return JSON only.
Do not generate entry price, stop loss, take profit, or position.
Only extract facts explicitly supported by the source text.
Output shape:
{"plans":[{"analyst":"","institution":"","symbol":"","asset_type":"","market":"","direction":"LONG or SHORT","reference_price":0,"reference_price_note":"","thesis":"","evidence":[{"chunk_index":0,"text":""}],"risks":[""],"confidence":0.0}]}`

var (
	jsonFenceRe = regexp.MustCompile("(?s)^```(?:json)?\\s*(.*?)\\s*```$")
)

type Analyzer interface {
	Analyze(context.Context, domain.Document, domain.ParseRun) ([]domain.PlanIntent, error)
}

type ModelAnalyzer struct {
	runtime *config.Runtime
	client  *http.Client
	logger  *slog.Logger
}

type chatCompletionRequest struct {
	Model          string            `json:"model"`
	Messages       []chatMessage     `json:"messages"`
	Temperature    float64           `json:"temperature"`
	ResponseFormat map[string]string `json:"response_format,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatCompletionResponse struct {
	Choices []struct {
		Message struct {
			Content any `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type plansEnvelope struct {
	Plans []domain.PlanIntent `json:"plans"`
}

func NewModelAnalyzer(runtime *config.Runtime, logger *slog.Logger) *ModelAnalyzer {
	return &ModelAnalyzer{
		runtime: runtime,
		client:  &http.Client{},
		logger:  logger,
	}
}

func (a *ModelAnalyzer) Analyze(ctx context.Context, document domain.Document, parsed domain.ParseRun) ([]domain.PlanIntent, error) {
	cfg := a.runtime.Config()
	if cfg == nil {
		return nil, fmt.Errorf("config runtime unavailable")
	}
	if !cfg.LLM.Enabled {
		return nil, fmt.Errorf("llm analyzer disabled")
	}
	if strings.TrimSpace(parsed.CleanedText) == "" {
		return nil, fmt.Errorf("empty parsed text")
	}
	if a.logger != nil {
		a.logger.InfoContext(ctx, "llm analyze start", "document_id", document.ID, "parse_run_id", parsed.ID, "chunk_count", len(parsed.Chunks), "model", cfg.LLM.Model, "provider", cfg.LLM.Provider)
	}

	chunks := parsed.Chunks
	if len(chunks) == 0 {
		chunks = []domain.Chunk{{Index: 0, Text: parsed.CleanedText}}
	}

	rawIntents := make([]domain.PlanIntent, 0)
	for _, chunk := range chunks {
		if a.logger != nil {
			a.logger.DebugContext(ctx, "llm analyze chunk dispatch", "document_id", document.ID, "chunk_index", chunk.Index, "chunk_chars", len([]rune(chunk.Text)))
		}
		intents, err := a.analyzeChunk(ctx, cfg.LLM, document, chunk)
		if err != nil {
			if a.logger != nil {
				a.logger.ErrorContext(ctx, "llm analyze chunk failed", "document_id", document.ID, "chunk_index", chunk.Index, "error", err.Error())
			}
			return nil, err
		}
		rawIntents = append(rawIntents, intents...)
	}

	intents, err := normalizeAndMergeIntents(rawIntents)
	if err != nil {
		if a.logger != nil {
			a.logger.ErrorContext(ctx, "llm analyze merge failed", "document_id", document.ID, "error", err.Error())
		}
		return nil, err
	}
	if len(intents) == 0 {
		return nil, fmt.Errorf("no structured trade intent extracted")
	}
	if a.logger != nil {
		a.logger.InfoContext(ctx, "llm analyze completed", "document_id", document.ID, "raw_intent_count", len(rawIntents), "merged_intent_count", len(intents))
	}
	return intents, nil
}

func (a *ModelAnalyzer) analyzeChunk(ctx context.Context, cfg config.LLMConfig, document domain.Document, chunk domain.Chunk) ([]domain.PlanIntent, error) {
	attempts := cfg.MaxRetries + 1
	if attempts <= 0 {
		attempts = 1
	}

	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		if a.logger != nil {
			a.logger.DebugContext(ctx, "llm analyze chunk attempt", "chunk_index", chunk.Index, "attempt", attempt, "max_attempts", attempts)
		}
		intents, err := a.requestPlans(ctx, cfg, document, chunk)
		if err == nil {
			if a.logger != nil {
				a.logger.InfoContext(ctx, "llm analyze chunk success", "chunk_index", chunk.Index, "attempt", attempt, "intent_count", len(intents))
			}
			return intents, nil
		}
		lastErr = err
		if a.logger != nil && attempt < attempts {
			a.logger.Warn("llm analyze chunk failed; retrying", "chunk_index", chunk.Index, "attempt", attempt, "error", err.Error())
		}
	}
	return nil, fmt.Errorf("llm analyze chunk %d failed after %d attempts: %w", chunk.Index, attempts, lastErr)
}

func (a *ModelAnalyzer) requestPlans(ctx context.Context, cfg config.LLMConfig, document domain.Document, chunk domain.Chunk) ([]domain.PlanIntent, error) {
	requestBody := chatCompletionRequest{
		Model:       cfg.Model,
		Temperature: 0,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: buildUserPrompt(document, chunk)},
		},
		ResponseFormat: map[string]string{"type": "json_object"},
	}

	raw, err := json.Marshal(requestBody)
	if err != nil {
		return nil, err
	}
	if a.logger != nil {
		a.logger.DebugContext(ctx, "llm request prepared", "chunk_index", chunk.Index, "payload_bytes", len(raw), "endpoint", cfg.Endpoint)
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(cfg.TimeoutMS)*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(timeoutCtx, http.MethodPost, cfg.Endpoint, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if a.logger != nil {
		a.logger.DebugContext(ctx, "llm response received", "chunk_index", chunk.Index, "status_code", resp.StatusCode, "body_bytes", len(body))
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("llm http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var completion chatCompletionResponse
	if err := json.Unmarshal(body, &completion); err != nil {
		return nil, fmt.Errorf("decode llm response: %w", err)
	}
	if completion.Error != nil && completion.Error.Message != "" {
		return nil, fmt.Errorf("llm error: %s", completion.Error.Message)
	}
	if len(completion.Choices) == 0 {
		return nil, fmt.Errorf("llm response missing choices")
	}

	content, err := extractMessageContent(completion.Choices[0].Message.Content)
	if err != nil {
		return nil, err
	}
	return parseAndValidatePlans(content)
}

func buildUserPrompt(document domain.Document, chunk domain.Chunk) string {
	return fmt.Sprintf(
		"Document title: %s\nAuthor: %s\nInstitution: %s\nChunk index: %d\n\nSource text:\n%s\n\nReturn only JSON.",
		document.Title,
		document.Author,
		document.Institution,
		chunk.Index,
		chunk.Text,
	)
}

func extractMessageContent(content any) (string, error) {
	switch value := content.(type) {
	case string:
		return strings.TrimSpace(value), nil
	case []any:
		var builder strings.Builder
		for _, item := range value {
			obj, ok := item.(map[string]any)
			if !ok {
				continue
			}
			text, _ := obj["text"].(string)
			builder.WriteString(text)
		}
		if builder.Len() == 0 {
			return "", fmt.Errorf("llm response content array missing text")
		}
		return strings.TrimSpace(builder.String()), nil
	default:
		return "", fmt.Errorf("unsupported llm message content type %T", content)
	}
}

func parseAndValidatePlans(raw string) ([]domain.PlanIntent, error) {
	raw = strings.TrimSpace(raw)
	if matches := jsonFenceRe.FindStringSubmatch(raw); len(matches) == 2 {
		raw = strings.TrimSpace(matches[1])
	}

	var envelope plansEnvelope
	if err := json.Unmarshal([]byte(raw), &envelope); err == nil && len(envelope.Plans) > 0 {
		return validatePlans(envelope.Plans)
	}

	var direct []domain.PlanIntent
	if err := json.Unmarshal([]byte(raw), &direct); err == nil && len(direct) > 0 {
		return validatePlans(direct)
	}

	return nil, fmt.Errorf("llm structured output validation failed")
}

func validatePlans(plans []domain.PlanIntent) ([]domain.PlanIntent, error) {
	validated := make([]domain.PlanIntent, 0, len(plans))
	for _, plan := range plans {
		normalized := normalizeIntent(plan)
		if err := ValidateIntent(normalized); err != nil {
			return nil, err
		}
		validated = append(validated, normalized)
	}
	if len(validated) == 0 {
		return nil, fmt.Errorf("llm structured output empty")
	}
	return validated, nil
}

func normalizeAndMergeIntents(intents []domain.PlanIntent) ([]domain.PlanIntent, error) {
	merged := make(map[string]domain.PlanIntent)
	order := make([]string, 0, len(intents))

	for _, item := range intents {
		intent := normalizeIntent(item)
		if err := ValidateIntent(intent); err != nil {
			return nil, err
		}

		key := intent.Symbol + ":" + intent.Direction
		current, exists := merged[key]
		if !exists {
			merged[key] = intent
			order = append(order, key)
			continue
		}

		if current.Analyst == "" {
			current.Analyst = intent.Analyst
		}
		if current.Institution == "" {
			current.Institution = intent.Institution
		}
		if current.ReferencePrice <= 0 && intent.ReferencePrice > 0 {
			current.ReferencePrice = intent.ReferencePrice
			current.ReferencePriceNote = intent.ReferencePriceNote
		}
		if len(intent.Thesis) > 0 && (current.Thesis == "" || len(intent.Thesis) < len(current.Thesis)) {
			current.Thesis = intent.Thesis
		}
		if intent.Confidence > current.Confidence {
			current.Confidence = intent.Confidence
		}
		current.Risks = appendUniqueStrings(current.Risks, intent.Risks, 5)
		current.Evidence = appendUniqueEvidence(current.Evidence, intent.Evidence, 4)
		merged[key] = current
	}

	result := make([]domain.PlanIntent, 0, len(order))
	for _, key := range order {
		result = append(result, merged[key])
	}
	return result, nil
}

func ValidateIntent(intent domain.PlanIntent) error {
	if intent.Symbol == "" {
		return fmt.Errorf("symbol is required")
	}
	if intent.Direction != "LONG" && intent.Direction != "SHORT" {
		return fmt.Errorf("direction must be LONG or SHORT")
	}
	if intent.ReferencePrice < 0 {
		return fmt.Errorf("reference_price must be zero or positive")
	}
	if intent.Thesis == "" {
		return fmt.Errorf("thesis is required")
	}
	if intent.Confidence <= 0 || intent.Confidence > 1 {
		return fmt.Errorf("confidence must be in (0,1]")
	}
	return nil
}

func normalizeIntent(intent domain.PlanIntent) domain.PlanIntent {
	intent.Analyst = strings.TrimSpace(intent.Analyst)
	intent.Institution = strings.TrimSpace(intent.Institution)
	intent.Symbol = normalizeSymbol(strings.TrimSpace(intent.Symbol))
	intent.Direction = strings.ToUpper(strings.TrimSpace(intent.Direction))
	intent.AssetType = inferAssetType(intent.Symbol)
	intent.Market = inferMarket(intent.Symbol)
	intent.Thesis = summarizeLine(intent.Thesis)
	intent.ReferencePriceNote = strings.TrimSpace(intent.ReferencePriceNote)
	if intent.ReferencePrice > 0 && intent.ReferencePriceNote == "" {
		intent.ReferencePriceNote = "explicit_price_mention"
	}
	if intent.ReferencePrice <= 0 && intent.ReferencePriceNote == "" {
		intent.ReferencePriceNote = "price_missing_in_text"
	}
	intent.Risks = appendUniqueStrings(nil, intent.Risks, 5)
	intent.Evidence = appendUniqueEvidence(nil, intent.Evidence, 4)
	return intent
}

func normalizeSymbol(value string) string {
	if value == "" {
		return ""
	}
	if strings.Contains(value, ".") {
		return strings.ToUpper(value)
	}
	if strings.HasPrefix(value, "6") {
		return value + ".SH"
	}
	return value + ".SZ"
}

func inferAssetType(symbol string) string {
	if strings.HasPrefix(symbol, "159") || strings.HasPrefix(symbol, "510") || strings.HasPrefix(symbol, "512") || strings.HasPrefix(symbol, "513") {
		return "ETF"
	}
	return "A_SHARE"
}

func inferMarket(symbol string) string {
	if strings.HasSuffix(symbol, ".SH") {
		return "SH"
	}
	return "SZ"
}

func summarizeLine(line string) string {
	line = strings.TrimSpace(line)
	if len([]rune(line)) <= 180 {
		return line
	}
	return string([]rune(line)[:180])
}

func appendUniqueStrings(base []string, additions []string, limit int) []string {
	seen := make(map[string]struct{}, len(base))
	items := make([]string, 0, len(base)+len(additions))
	for _, item := range base {
		item = summarizeLine(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		items = append(items, item)
	}
	for _, item := range additions {
		item = summarizeLine(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		items = append(items, item)
		if limit > 0 && len(items) >= limit {
			break
		}
	}
	return items
}

func appendUniqueEvidence(base []domain.EvidenceSpan, additions []domain.EvidenceSpan, limit int) []domain.EvidenceSpan {
	seen := make(map[string]struct{}, len(base))
	items := make([]domain.EvidenceSpan, 0, len(base)+len(additions))
	appendOne := func(item domain.EvidenceSpan) bool {
		item.Text = summarizeLine(item.Text)
		if item.Text == "" {
			return false
		}
		key := strconv.Itoa(item.ChunkIndex) + ":" + item.Text
		if _, ok := seen[key]; ok {
			return false
		}
		seen[key] = struct{}{}
		items = append(items, item)
		return true
	}
	for _, item := range base {
		appendOne(item)
	}
	for _, item := range additions {
		if appendOne(item) && limit > 0 && len(items) >= limit {
			break
		}
	}
	return items
}
