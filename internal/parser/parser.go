package parser

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"html"
	"io"
	"net/mail"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"finance-sys/internal/config"
	"finance-sys/internal/domain"
	"finance-sys/internal/utils"
)

const version = "parser-v1"

var (
	scriptTagRe = regexp.MustCompile(`(?is)<script.*?</script>`)
	styleTagRe  = regexp.MustCompile(`(?is)<style.*?</style>`)
	htmlTagRe   = regexp.MustCompile(`(?s)<[^>]+>`)
)

type Service struct{}

func New() *Service {
	return &Service{}
}

func (s *Service) Parse(ctx context.Context, fileName string, content []byte, cfg config.DocumentParsingConfig) (domain.ParseRun, error) {
	ext := strings.ToLower(filepath.Ext(fileName))
	result := domain.ParseRun{
		Status:        "PARSED",
		ParserName:    parserName(ext),
		ParserVersion: version,
		PageCount:     1,
		RawMetadata:   map[string]any{"extension": ext},
	}

	var text string
	var err error
	switch ext {
	case ".txt", ".md":
		text = string(content)
	case ".html":
		text = parseHTML(content, cfg.HTML)
	case ".eml":
		text, result.RawMetadata, err = parseEmail(content, cfg.Email)
	case ".docx":
		text, err = parseDOCX(content)
	case ".pdf":
		text, result.RequiresOCR, err = parsePDF(ctx, fileName, content, cfg)
	default:
		err = fmt.Errorf("unsupported extension: %s", ext)
	}
	if err != nil {
		result.Status = "FAILED"
		result.ErrorMessage = err.Error()
		return result, err
	}

	result.ContentText = text
	result.CleanedText = cleanText(text, cfg.Cleaning)
	result.Sections = buildSections(result.CleanedText)
	result.Chunks = buildChunks(result.CleanedText, cfg.Chunking)
	result.TextDensity = calculateDensity(content, result.CleanedText)
	if result.RequiresOCR {
		result.Status = "NEEDS_OCR"
	}
	return result, nil
}

func parserName(ext string) string {
	switch ext {
	case ".pdf":
		return "pdf-cli"
	case ".docx":
		return "docx-native"
	case ".html":
		return "html-native"
	case ".eml":
		return "email-native"
	default:
		return "text-native"
	}
}

func parseHTML(content []byte, cfg config.HTMLParsingConfig) string {
	text := string(content)
	if cfg.RemoveScripts {
		text = scriptTagRe.ReplaceAllString(text, " ")
	}
	if cfg.RemoveStyles {
		text = styleTagRe.ReplaceAllString(text, " ")
	}
	text = htmlTagRe.ReplaceAllString(text, " ")
	return html.UnescapeString(text)
}

func parseEmail(content []byte, cfg config.EmailParsingConfig) (string, map[string]any, error) {
	message, err := mail.ReadMessage(bytes.NewReader(content))
	if err != nil {
		return "", nil, err
	}
	body, err := io.ReadAll(message.Body)
	if err != nil {
		return "", nil, err
	}
	text := string(body)
	metadata := map[string]any{
		"subject": message.Header.Get("Subject"),
		"from":    message.Header.Get("From"),
		"to":      message.Header.Get("To"),
	}
	if cfg.PreferPlainText {
		return text, metadata, nil
	}
	return text, metadata, nil
}

func parseDOCX(content []byte) (string, error) {
	reader, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return "", err
	}
	for _, file := range reader.File {
		if file.Name != "word/document.xml" {
			continue
		}
		handle, err := file.Open()
		if err != nil {
			return "", err
		}
		defer handle.Close()
		raw, err := io.ReadAll(handle)
		if err != nil {
			return "", err
		}
		text := htmlTagRe.ReplaceAllString(string(raw), " ")
		return html.UnescapeString(text), nil
	}
	return "", fmt.Errorf("word/document.xml not found")
}

func parsePDF(ctx context.Context, fileName string, content []byte, cfg config.DocumentParsingConfig) (string, bool, error) {
	tmpFile, err := os.CreateTemp("", "expert-trade-*.pdf")
	if err != nil {
		return "", false, err
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.Write(content); err != nil {
		tmpFile.Close()
		return "", false, err
	}
	if err := tmpFile.Close(); err != nil {
		return "", false, err
	}

	// `pdftotext` stays outside the Go runtime but keeps PDF extraction in the Go-controlled pipeline.
	cmd := exec.CommandContext(ctx, "pdftotext", "-layout", tmpFile.Name(), "-")
	output, err := cmd.Output()
	if err != nil {
		if cfg.OCR.Enabled {
			return "", true, fmt.Errorf("pdftotext failed for %s: %w", fileName, err)
		}
		return "", false, fmt.Errorf("pdftotext failed for %s: %w", fileName, err)
	}
	text := string(output)
	if calculateDensity(content, text) < cfg.PDF.OCRFallbackWhenTextDensityBelow {
		if cfg.OCR.Enabled {
			return text, true, nil
		}
	}
	return text, false, nil
}

func cleanText(input string, cfg config.CleaningConfig) string {
	lines := strings.Split(input, "\n")
	seen := make(map[string]struct{})
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if cfg.RemoveDisclaimerBlocks && isNoiseLine(line) {
			continue
		}
		if cfg.NormalizeWhitespace {
			line = utils.NormalizeWhitespace(line)
		}
		if cfg.RemoveDuplicateLines {
			if _, ok := seen[line]; ok {
				continue
			}
			seen[line] = struct{}{}
		}
		filtered = append(filtered, line)
	}
	return strings.Join(filtered, "\n")
}

func isNoiseLine(line string) bool {
	noiseTokens := []string{"免责声明", "仅供参考", "版权所有", "风险提示"}
	for _, token := range noiseTokens {
		if strings.Contains(line, token) {
			return true
		}
	}
	return false
}

func buildSections(input string) []domain.Section {
	if input == "" {
		return nil
	}
	parts := strings.Split(input, "\n")
	sections := make([]domain.Section, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		sections = append(sections, domain.Section{
			Heading: inferHeading(part),
			Text:    part,
		})
	}
	return sections
}

func inferHeading(text string) string {
	if len(text) <= 24 {
		return text
	}
	return "body"
}

func buildChunks(input string, cfg config.ChunkingConfig) []domain.Chunk {
	if input == "" {
		return nil
	}
	if !cfg.Enabled || cfg.TargetChars <= 0 {
		return []domain.Chunk{{Index: 0, Text: input}}
	}
	runes := []rune(input)
	chunks := make([]domain.Chunk, 0)
	start := 0
	index := 0
	for start < len(runes) {
		end := start + cfg.TargetChars
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, domain.Chunk{
			Index: index,
			Text:  string(runes[start:end]),
		})
		if end == len(runes) {
			break
		}
		start = end - cfg.OverlapChars
		if start < 0 {
			start = 0
		}
		index++
	}
	return chunks
}

func calculateDensity(raw []byte, text string) float64 {
	if len(raw) == 0 {
		return 0
	}
	return float64(len(strings.TrimSpace(text))) / float64(len(raw))
}
