package parser

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"finance-sys/internal/config"
	"finance-sys/internal/domain"
	"finance-sys/internal/utils"
)

const version = "parser-v2"

type Service struct {
	logger *slog.Logger
}

func New(logger *slog.Logger) *Service {
	return &Service{logger: logger}
}

func (s *Service) Parse(ctx context.Context, fileName string, content []byte, cfg config.DocumentConfig) (domain.ParseRun, error) {
	ext := strings.ToLower(filepath.Ext(fileName))
	if s.logger != nil {
		s.logger.InfoContext(ctx, "parser parse start", "file_name", fileName, "extension", ext, "size_bytes", len(content))
	}
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
	case ".txt", ".md", ".csv":
		text = string(content)
	case ".doc":
		text, err = parseDOC(ctx, fileName, content)
	case ".docx":
		text, err = parseDOCX(content)
	case ".pdf":
		text, err = parsePDF(ctx, fileName, content)
	default:
		err = fmt.Errorf("unsupported extension: %s", ext)
	}
	if err != nil {
		result.Status = "FAILED"
		result.ErrorMessage = err.Error()
		if s.logger != nil {
			s.logger.ErrorContext(ctx, "parser parse failed", "file_name", fileName, "extension", ext, "error", err.Error())
		}
		return result, err
	}

	result.ContentText = text
	result.CleanedText = cleanText(text)
	result.Chunks = buildChunks(result.CleanedText, cfg.Chunking)
	if s.logger != nil {
		s.logger.InfoContext(ctx, "parser parse success", "file_name", fileName, "extension", ext, "cleaned_chars", len([]rune(result.CleanedText)), "chunk_count", len(result.Chunks))
	}
	return result, nil
}

func parserName(ext string) string {
	switch ext {
	case ".pdf":
		return "pdf-cli"
	case ".doc":
		return "doc-cli"
	case ".docx":
		return "docx-native"
	default:
		return "text-native"
	}
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
		text := strings.NewReplacer(
			"<w:t>", "",
			"</w:t>", " ",
			"<w:tab/>", " ",
			"<w:br/>", "\n",
			"</w:p>", "\n",
		).Replace(string(raw))
		return stripXMLTags(text), nil
	}
	return "", fmt.Errorf("word/document.xml not found")
}

func parseDOC(ctx context.Context, fileName string, content []byte) (string, error) {
	tmpFile, err := os.CreateTemp("", "finance-sys-*.doc")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.Write(content); err != nil {
		tmpFile.Close()
		return "", err
	}
	if err := tmpFile.Close(); err != nil {
		return "", err
	}

	cmd := exec.CommandContext(ctx, "antiword", tmpFile.Name())
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("antiword failed for %s: %w", fileName, err)
	}
	return string(output), nil
}

func parsePDF(ctx context.Context, fileName string, content []byte) (string, error) {
	tmpFile, err := os.CreateTemp("", "finance-sys-*.pdf")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.Write(content); err != nil {
		tmpFile.Close()
		return "", err
	}
	if err := tmpFile.Close(); err != nil {
		return "", err
	}

	cmd := exec.CommandContext(ctx, "pdftotext", "-layout", tmpFile.Name(), "-")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("pdftotext failed for %s: %w", fileName, err)
	}
	return string(output), nil
}

func cleanText(input string) string {
	lines := strings.Split(input, "\n")
	seen := make(map[string]struct{})
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if isNoiseLine(line) {
			continue
		}
		line = utils.NormalizeWhitespace(line)
		if _, ok := seen[line]; ok {
			continue
		}
		seen[line] = struct{}{}
		filtered = append(filtered, line)
	}
	return strings.Join(filtered, "\n")
}

func isNoiseLine(line string) bool {
	noiseTokens := []string{"免责声明", "仅供参考", "版权归", "风险提示"}
	for _, token := range noiseTokens {
		if strings.Contains(line, token) {
			return true
		}
	}
	return false
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

func stripXMLTags(input string) string {
	var builder strings.Builder
	inTag := false
	for _, r := range input {
		switch r {
		case '<':
			inTag = true
		case '>':
			inTag = false
		default:
			if !inTag {
				builder.WriteRune(r)
			}
		}
	}
	return builder.String()
}
