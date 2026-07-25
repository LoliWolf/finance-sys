package parser_test

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
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

func TestParsePDFUsesConfiguredOCRDirectly(t *testing.T) {
	t.Setenv("FINANCE_SYS_PARSER_OCR_HELPER", "1")

	service := parser.New(nil)
	result, err := service.Parse(context.Background(), "native.pdf", buildMinimalPDF("NATIVE TEXT MUST NOT WIN"), config.DocumentConfig{
		PDFUseOCR: true,
		Chunking: config.ChunkingConfig{
			Enabled:     true,
			TargetChars: 64,
		},
		PDFOCR: config.PDFOCRConfig{
			Command:      os.Args[0],
			Args:         []string{"-test.run=^TestParserOCRHelperProcess$"},
			MinTextChars: 10,
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

func TestParsePDFUsesNativeExtractorWithoutSystemBinary(t *testing.T) {
	service := parser.New(nil)
	result, err := service.Parse(context.Background(), "native.pdf", buildMinimalPDF("Recommend 600519.SH"), config.DocumentConfig{
		Chunking: config.ChunkingConfig{
			Enabled:     true,
			TargetChars: 64,
		},
	})

	require.NoError(t, err)
	require.Equal(t, domain.ParseRunStatusParsed, result.Status)
	require.Equal(t, domain.ParserNamePDFNative, result.ParserName)
	require.Contains(t, result.CleanedText, "600519.SH")
	require.NotEmpty(t, result.Chunks)
}

func TestParsePDFUsesPDFKitForChineseResearchReportOnMac(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("PDFKit is provided by macOS")
	}
	content, err := os.ReadFile("../../tmp/pdfs/company-research-test.pdf")
	if os.IsNotExist(err) {
		t.Skip("local acceptance report is not present")
	}
	require.NoError(t, err)

	service := parser.New(nil)
	result, err := service.Parse(context.Background(), "company-research-test.pdf", content, config.DocumentConfig{
		Chunking: config.ChunkingConfig{Enabled: true, TargetChars: 2000},
	})
	require.NoError(t, err)
	require.Equal(t, domain.ParserNamePDFKit, result.ParserName)
	require.Contains(t, result.CleanedText, "餐饮供应链温和复苏")
	require.Contains(t, result.CleanedText, "华龙证券研究所")
}

func TestParsePDFUsesVendoredOCRForGuziyuanPDFOnMac(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("PDFKit and Vision OCR are provided by macOS")
	}
	content, err := os.ReadFile("../../testdata/guziyuanpdf/大成路旁7.21强修复.pdf")
	if os.IsNotExist(err) {
		t.Skip("local guziyuan acceptance PDF is not present")
	}
	require.NoError(t, err)

	service := parser.New(nil)
	result, err := service.Parse(context.Background(), "大成路旁7.21强修复.pdf", content, config.DocumentConfig{
		PDFUseOCR: true,
		Chunking:  config.ChunkingConfig{Enabled: true, TargetChars: 2000},
		PDFOCR: config.PDFOCRConfig{
			Command:              "../../tools/guziyuan_pdf_ocr_tool/ocr_pdf.bat",
			Args:                 []string{"{input}", "--stdout"},
			MinTextChars:         80,
			TimeoutMS:            120000,
			TreatExitCodeOneAsOK: true,
		},
	})
	require.NoError(t, err)
	require.Equal(t, domain.ParserNamePDFOCR, result.ParserName)
	require.Equal(t, true, result.RawMetadata["pdf_ocr_used"])
	require.Contains(t, result.CleanedText, "7月22日强修复")
	require.Contains(t, result.CleanedText, "共进股份")
	require.NotContains(t, strings.ToLower(result.CleanedText), "guziyuan")
}

func buildMinimalPDF(text string) []byte {
	var buffer bytes.Buffer
	buffer.WriteString("%PDF-1.4\n")
	offsets := make([]int, 6)
	writeObject := func(number int, body string) {
		offsets[number] = buffer.Len()
		fmt.Fprintf(&buffer, "%d 0 obj\n%s\nendobj\n", number, body)
	}
	writeObject(1, "<< /Type /Catalog /Pages 2 0 R >>")
	writeObject(2, "<< /Type /Pages /Kids [3 0 R] /Count 1 >>")
	writeObject(3, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 144] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>")
	writeObject(4, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")
	stream := fmt.Sprintf("BT /F1 12 Tf 36 72 Td (%s) Tj ET", text)
	writeObject(5, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream))
	xrefOffset := buffer.Len()
	buffer.WriteString("xref\n0 6\n0000000000 65535 f \n")
	for number := 1; number <= 5; number++ {
		fmt.Fprintf(&buffer, "%010d 00000 n \n", offsets[number])
	}
	fmt.Fprintf(&buffer, "trailer\n<< /Size 6 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xrefOffset)
	return buffer.Bytes()
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
