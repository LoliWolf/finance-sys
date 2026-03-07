package parser_test

import (
	"context"
	"testing"

	"finance-sys/internal/config"
	"finance-sys/internal/parser"

	"github.com/stretchr/testify/require"
)

func TestParseHTML(t *testing.T) {
	service := parser.New()
	result, err := service.Parse(context.Background(), "sample.html", []byte(`<html><body><h1>报告</h1><p>看多 600519.SH，风险可控。</p></body></html>`), config.DocumentParsingConfig{
		HTML: config.HTMLParsingConfig{Enabled: true, RemoveScripts: true, RemoveStyles: true},
		Cleaning: config.CleaningConfig{
			NormalizeWhitespace: true,
		},
		Chunking: config.ChunkingConfig{
			Enabled:     true,
			TargetChars: 32,
		},
	})
	require.NoError(t, err)
	require.Equal(t, "PARSED", result.Status)
	require.Contains(t, result.CleanedText, "600519.SH")
	require.NotEmpty(t, result.Chunks)
}

func TestParseEmail(t *testing.T) {
	service := parser.New()
	email := "Subject: 盘后观点\r\nFrom: analyst@example.com\r\nTo: desk@example.com\r\n\r\n看多 000300.SH，建议关注低吸机会。"
	result, err := service.Parse(context.Background(), "daily.eml", []byte(email), config.DocumentParsingConfig{
		Email: config.EmailParsingConfig{Enabled: true, PreferPlainText: true},
		Cleaning: config.CleaningConfig{
			NormalizeWhitespace: true,
		},
		Chunking: config.ChunkingConfig{
			Enabled:     true,
			TargetChars: 64,
		},
	})
	require.NoError(t, err)
	require.Equal(t, "盘后观点", result.RawMetadata["subject"])
	require.Contains(t, result.CleanedText, "000300.SH")
}
