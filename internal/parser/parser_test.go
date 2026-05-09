package parser_test

import (
	"archive/zip"
	"bytes"
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

func TestParseDocxExtractsPlainText(t *testing.T) {
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	file, err := writer.Create("word/document.xml")
	require.NoError(t, err)
	_, err = file.Write([]byte(`<w:document><w:body><w:p><w:r><w:t>Recommend 600519.SH</w:t></w:r></w:p><w:p><w:r><w:t>Reference price 1688</w:t></w:r></w:p></w:body></w:document>`))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	service := parser.New(nil)
	result, err := service.Parse(context.Background(), "sample.docx", buf.Bytes(), config.DocumentConfig{
		Chunking: config.ChunkingConfig{
			Enabled:     true,
			TargetChars: 64,
		},
	})
	require.NoError(t, err)
	require.Equal(t, "PARSED", result.Status)
	require.Contains(t, result.CleanedText, "600519.SH")
	require.Contains(t, result.CleanedText, "Reference price 1688")
	require.NotEmpty(t, result.Chunks)
}
