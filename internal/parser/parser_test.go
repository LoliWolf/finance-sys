package parser_test

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"os"
	"runtime"
	"testing"

	"finance-sys/internal/config"
	"finance-sys/internal/domain"
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
	require.Equal(t, domain.ParseRunStatusParsed, result.Status)
	require.Contains(t, result.CleanedText, "600519.SH")
	require.NotEmpty(t, result.Chunks)
}

func TestParsePDFFallsBackToConfiguredOCR(t *testing.T) {
	t.Setenv("FINANCE_SYS_PARSER_OCR_HELPER", "1")

	service := parser.New(nil)
	result, err := service.Parse(context.Background(), "scan.pdf", []byte("not a real pdf"), config.DocumentConfig{
		Chunking: config.ChunkingConfig{
			Enabled:     true,
			TargetChars: 64,
		},
		PDFOCR: config.PDFOCRConfig{
			Enabled:      true,
			Command:      os.Args[0],
			Args:         []string{"-test.run=^TestParserOCRHelperProcess$"},
			MinTextChars: 80,
			TimeoutMS:    5000,
		},
	})
	require.NoError(t, err)
	require.Equal(t, domain.ParseRunStatusParsed, result.Status)
	require.Equal(t, domain.ParserNamePDFOCR, result.ParserName)
	require.Equal(t, true, result.RawMetadata["pdf_ocr_used"])
	require.Contains(t, result.CleanedText, "OCR 600519.SH")
	require.NotEmpty(t, result.Chunks)
}

func TestParserOCRHelperProcess(t *testing.T) {
	if os.Getenv("FINANCE_SYS_PARSER_OCR_HELPER") != "1" {
		return
	}
	fmt.Print("OCR 600519.SH reference 1688")
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
	require.Equal(t, domain.ParseRunStatusParsed, result.Status)
	require.Contains(t, result.CleanedText, "600519.SH")
	require.Contains(t, result.CleanedText, "Reference price 1688")
	require.NotEmpty(t, result.Chunks)
}

func TestParseDocUsesMacOSTextutil(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("textutil is provided by macOS")
	}

	service := parser.New(nil)
	content := []byte(`{\rtf1\ansi Mac DOC recommends 600519.SH at 1688.}`)
	result, err := service.Parse(context.Background(), "sample.doc", content, config.DocumentConfig{
		Chunking: config.ChunkingConfig{
			Enabled:     true,
			TargetChars: 64,
		},
	})
	require.NoError(t, err)
	require.Equal(t, domain.ParseRunStatusParsed, result.Status)
	require.Contains(t, result.CleanedText, "600519.SH")
	require.NotEmpty(t, result.Chunks)
}
