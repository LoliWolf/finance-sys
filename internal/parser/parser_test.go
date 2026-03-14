package parser_test

import (
	"context"
	"testing"

	"finance-sys/internal/config"
	"finance-sys/internal/parser"

	"github.com/stretchr/testify/require"
)

func TestParseTextBuildsChunks(t *testing.T) {
	service := parser.New(nil)
	result, err := service.Parse(context.Background(), "sample.txt", []byte("推荐 600519.SH，参考价 1688.00 元。\n风险提示：需求不及预期。"), config.DocumentConfig{
		Chunking: config.ChunkingConfig{
			Enabled:     true,
			TargetChars: 24,
		},
	})
	require.NoError(t, err)
	require.Equal(t, "PARSED", result.Status)
	require.Contains(t, result.CleanedText, "600519.SH")
	require.NotEmpty(t, result.Chunks)
}
