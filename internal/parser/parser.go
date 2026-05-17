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
	"runtime"
	"strings"
	"time"

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
		Status:        domain.ParseRunStatusParsed,
		ParserName:    parserName(ext),
		ParserVersion: version,
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
		var usedOCR bool
		text, usedOCR, err = parsePDF(ctx, fileName, content, cfg.PDFOCR)
		if usedOCR {
			result.ParserName = domain.ParserNamePDFOCR
			result.RawMetadata["pdf_ocr_used"] = true
		}
	default:
		err = fmt.Errorf("unsupported extension: %s", ext)
	}
	if err != nil {
		result.Status = domain.ParseRunStatusFailed
		result.ErrorMessage = err.Error()
		if s.logger != nil {
			s.logger.ErrorContext(ctx, "parser parse failed", "file_name", fileName, "extension", ext, "error", err.Error())
		}
		return result, err
	}

	result.CleanedText = cleanText(text)
	result.Chunks = buildChunks(result.CleanedText, cfg.Chunking)
	if s.logger != nil {
		s.logger.InfoContext(ctx, "parser parse success", "file_name", fileName, "extension", ext, "cleaned_chars", len([]rune(result.CleanedText)), "chunk_count", len(result.Chunks))
	}
	return result, nil
}

func parserName(ext string) domain.ParserName {
	switch ext {
	case ".pdf":
		return domain.ParserNamePDFCLI
	case ".doc":
		return domain.ParserNameDOCCLI
	case ".docx":
		return domain.ParserNameDOCXNative
	default:
		return domain.ParserNameTextNative
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

func parsePDF(ctx context.Context, fileName string, content []byte, ocrCfg config.PDFOCRConfig) (string, bool, error) {
	tmpFile, err := os.CreateTemp("", "finance-sys-*.pdf")
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

	cmd := exec.CommandContext(ctx, "pdftotext", "-layout", tmpFile.Name(), "-")
	output, err := cmd.Output()
	if err == nil && !shouldFallbackToOCR(string(output), ocrCfg) {
		return string(output), false, nil
	}
	if !ocrCfg.Enabled {
		if err != nil {
			return "", false, fmt.Errorf("pdftotext failed for %s: %w", fileName, err)
		}
		return string(output), false, nil
	}

	ocrText, ocrErr := parsePDFWithOCR(ctx, tmpFile.Name(), ocrCfg)
	if ocrErr != nil {
		if err != nil {
			return "", false, fmt.Errorf("pdftotext failed for %s: %w; ocr failed: %v", fileName, err, ocrErr)
		}
		return "", false, fmt.Errorf("ocr failed for %s: %w", fileName, ocrErr)
	}
	return ocrText, true, nil
}

func shouldFallbackToOCR(text string, cfg config.PDFOCRConfig) bool {
	if !cfg.Enabled {
		return false
	}
	if cfg.MinTextChars <= 0 {
		return strings.TrimSpace(text) == ""
	}
	return len([]rune(cleanText(text))) < cfg.MinTextChars
}

func parsePDFWithOCR(ctx context.Context, inputPath string, cfg config.PDFOCRConfig) (string, error) {
	timeout := time.Duration(cfg.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	ocrCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	command := os.ExpandEnv(cfg.Command)
	args := make([]string, 0, len(cfg.Args))
	for _, arg := range cfg.Args {
		arg = os.ExpandEnv(arg)
		arg = strings.ReplaceAll(arg, "{input}", inputPath)
		args = append(args, arg)
	}

	command, args, err := buildOCRExec(command, args)
	if err != nil {
		return "", err
	}

	cmd := exec.CommandContext(ocrCtx, command, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		if cfg.TreatExitCodeOneAsOK {
			if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 && len(output) > 0 {
				return string(output), nil
			}
		}
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return "", fmt.Errorf("%s", message)
	}
	if strings.TrimSpace(string(output)) == "" {
		return "", fmt.Errorf("ocr produced empty text")
	}
	return string(output), nil
}

func buildOCRExec(command string, args []string) (string, []string, error) {
	resolved, err := resolveOCRCommand(command)
	if err != nil {
		return "", nil, err
	}
	if runtime.GOOS == "windows" {
		switch strings.ToLower(filepath.Ext(resolved)) {
		case ".bat", ".cmd":
			shell := os.Getenv("ComSpec")
			if strings.TrimSpace(shell) == "" {
				shell = "cmd"
			}
			shellArgs := make([]string, 0, len(args)+2)
			shellArgs = append(shellArgs, "/c", resolved)
			shellArgs = append(shellArgs, args...)
			return shell, shellArgs, nil
		}
	}
	return resolved, args, nil
}

func resolveOCRCommand(command string) (string, error) {
	command = strings.TrimSpace(os.ExpandEnv(command))
	if command == "" {
		return "", fmt.Errorf("ocr command is empty")
	}
	if !isPathCommand(command) {
		return command, nil
	}

	normalized := filepath.Clean(filepath.FromSlash(command))
	candidates := make([]string, 0, 2)
	if filepath.IsAbs(normalized) {
		candidates = append(candidates, normalized)
	} else {
		if abs, err := filepath.Abs(normalized); err == nil {
			candidates = append(candidates, abs)
		}
		if root, ok := findProjectRoot(); ok {
			candidates = append(candidates, filepath.Join(root, normalized))
		}
	}

	for _, candidate := range dedupeStrings(candidates) {
		if fileExists(candidate) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("ocr command not found: %s (looked in %s)", command, strings.Join(dedupeStrings(candidates), ", "))
}

func isPathCommand(command string) bool {
	return filepath.IsAbs(command) || strings.ContainsAny(command, `/\`)
}

func findProjectRoot() (string, bool) {
	dir, err := os.Getwd()
	if err != nil {
		return "", false
	}
	for {
		if fileExists(filepath.Join(dir, "go.mod")) {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dedupeStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		key := value
		if runtime.GOOS == "windows" {
			key = strings.ToLower(value)
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	return result
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
