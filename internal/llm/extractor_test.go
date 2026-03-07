package llm_test

import (
	"context"
	"testing"
	"time"

	"finance-sys/internal/domain"
	"finance-sys/internal/llm"

	"github.com/stretchr/testify/require"
)

func TestMockExtractorExtractsSignal(t *testing.T) {
	extractor := llm.NewMockExtractor()
	signals, err := extractor.Extract(context.Background(), domain.Document{
		ID:          1,
		Author:      "Alice",
		Institution: "Research",
		CreatedAt:   time.Now(),
	}, domain.ParseRun{
		CleanedText: "我们看多 600519.SH，预计需求回升。风险在于消费修复不及预期。",
		Chunks: []domain.Chunk{
			{Index: 0, Text: "我们看多 600519.SH，预计需求回升。"},
		},
	})
	require.NoError(t, err)
	require.Len(t, signals, 1)
	require.Equal(t, "600519.SH", signals[0].Symbol)
	require.Equal(t, "BULLISH", signals[0].Sentiment)
}
