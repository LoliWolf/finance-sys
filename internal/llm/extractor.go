package llm

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"finance-sys/internal/domain"
)

var (
	symbolRe    = regexp.MustCompile(`\b\d{6}(?:\.(?:SH|SZ))?\b`)
	bullishRe   = regexp.MustCompile(`买入|推荐|增持|看多|超配|乐观`)
	bearishRe   = regexp.MustCompile(`卖出|减持|回避|看空|谨慎|下调`)
	riskLineRe  = regexp.MustCompile(`风险|不确定|波动`)
	indexSymbol = map[string]string{
		"沪深300": "000300.SH",
		"上证50":  "000016.SH",
		"创业板指":  "399006.SZ",
	}
)

type Extractor interface {
	Extract(context.Context, domain.Document, domain.ParseRun) ([]domain.ExtractedSignal, error)
}

type MockExtractor struct{}

func NewMockExtractor() *MockExtractor {
	return &MockExtractor{}
}

func (m *MockExtractor) Extract(_ context.Context, document domain.Document, parsed domain.ParseRun) ([]domain.ExtractedSignal, error) {
	if parsed.CleanedText == "" {
		return nil, fmt.Errorf("empty parsed text")
	}

	lines := strings.Split(parsed.CleanedText, "\n")
	unique := make(map[string]struct{})
	var signals []domain.ExtractedSignal

	emit := func(symbol string, thesis string) {
		if symbol == "" {
			return
		}
		symbol = normalizeSymbol(symbol)
		if _, ok := unique[symbol]; ok {
			return
		}
		unique[symbol] = struct{}{}
		sentiment := inferSentiment(parsed.CleanedText)
		if sentiment == "NEUTRAL" {
			return
		}
		signal := domain.ExtractedSignal{
			ExpertName: choose(document.Author, document.SourceName, "unknown"),
			ExpertOrg:  choose(document.Institution, document.SourceName, "unknown"),
			Symbol:     symbol,
			AssetType:  inferAssetType(symbol),
			Market:     inferMarket(symbol),
			Sentiment:  sentiment,
			Thesis:     thesis,
			Evidence:   collectEvidence(symbol, parsed.Chunks),
			Risks:      collectRisks(lines),
			Confidence: inferConfidence(parsed.CleanedText),
		}
		if err := ValidateSignal(signal); err == nil {
			signals = append(signals, signal)
		}
	}

	for _, line := range lines {
		matches := symbolRe.FindAllString(line, -1)
		for _, match := range matches {
			emit(match, summarizeLine(line))
		}
		for alias, symbol := range indexSymbol {
			if strings.Contains(line, alias) {
				emit(symbol, summarizeLine(line))
			}
		}
	}

	if len(signals) == 0 {
		return nil, fmt.Errorf("no structured signals extracted")
	}
	return signals, nil
}

func ValidateSignal(signal domain.ExtractedSignal) error {
	if signal.Symbol == "" {
		return fmt.Errorf("symbol is required")
	}
	switch signal.Sentiment {
	case "BULLISH", "BEARISH":
	default:
		return fmt.Errorf("sentiment must be BULLISH or BEARISH")
	}
	if signal.Thesis == "" {
		return fmt.Errorf("thesis is required")
	}
	if signal.Confidence <= 0 || signal.Confidence > 1 {
		return fmt.Errorf("confidence must be in (0,1]")
	}
	return nil
}

func normalizeSymbol(value string) string {
	if strings.Contains(value, ".") {
		return strings.ToUpper(value)
	}
	if strings.HasPrefix(value, "6") {
		return value + ".SH"
	}
	return value + ".SZ"
}

func inferSentiment(text string) string {
	switch {
	case bullishRe.MatchString(text):
		return "BULLISH"
	case bearishRe.MatchString(text):
		return "BEARISH"
	default:
		return "NEUTRAL"
	}
}

func inferAssetType(symbol string) string {
	switch {
	case strings.HasPrefix(symbol, "159"), strings.HasPrefix(symbol, "510"), strings.HasPrefix(symbol, "512"), strings.HasPrefix(symbol, "513"):
		return "ETF"
	case strings.HasPrefix(symbol, "000300"), strings.HasPrefix(symbol, "000016"), strings.HasPrefix(symbol, "399006"):
		return "INDEX"
	default:
		return "A_SHARE"
	}
}

func inferMarket(symbol string) string {
	if strings.HasSuffix(symbol, ".SH") {
		return "SH"
	}
	return "SZ"
}

func collectEvidence(symbol string, chunks []domain.Chunk) []domain.EvidenceSpan {
	evidence := make([]domain.EvidenceSpan, 0, 2)
	for _, chunk := range chunks {
		if strings.Contains(strings.ToUpper(chunk.Text), strings.ToUpper(symbol[:6])) || strings.Contains(chunk.Text, symbol) {
			evidence = append(evidence, domain.EvidenceSpan{
				ChunkIndex: chunk.Index,
				Text:       summarizeLine(chunk.Text),
			})
		}
		if len(evidence) == 2 {
			break
		}
	}
	return evidence
}

func collectRisks(lines []string) []string {
	risks := make([]string, 0, 3)
	for _, line := range lines {
		if riskLineRe.MatchString(line) {
			risks = append(risks, summarizeLine(line))
			if len(risks) == 3 {
				break
			}
		}
	}
	return risks
}

func summarizeLine(line string) string {
	line = strings.TrimSpace(line)
	if len([]rune(line)) <= 140 {
		return line
	}
	return string([]rune(line)[:140])
}

func inferConfidence(text string) float64 {
	length := len([]rune(text))
	switch {
	case length > 5000:
		return 0.88
	case length > 1000:
		return 0.8
	default:
		return 0.7
	}
}

func choose(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return time.Now().Format("2006-01-02")
}
