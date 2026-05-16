package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"finance-sys/internal/bootstrap"
	"finance-sys/internal/config"

	"github.com/stretchr/testify/require"
)

func TestHTTPUploadAnalyzeUsesPDFOCRProcessor(t *testing.T) {
	if os.Getenv("FINANCE_SYS_HTTP_OCR_INTEGRATION") != "1" {
		t.Skip("set FINANCE_SYS_HTTP_OCR_INTEGRATION=1 to run Nacos/MySQL HTTP OCR integration test")
	}
	if runtime.GOOS != "windows" {
		t.Skip("fake OCR command uses cmd.exe")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	app := buildIntegrationApp(t, ctx)
	defer app.Close()

	cfg := cloneConfig(t, app.Runtime.Config())
	cfg.Document.AutoAnalyzeUpload = false
	cfg.Document.PDFOCR = config.PDFOCRConfig{
		Enabled:      true,
		Command:      "cmd",
		Args:         []string{"/c", "echo OCR_SENTINEL 推荐 600519.SH 参考价 1688"},
		MinTextChars: 80,
		TimeoutMS:    5000,
	}
	app.Runtime.Update(&config.Snapshot{
		Config:   cfg,
		Source:   app.Runtime.Current().Source,
		SHA256:   app.Runtime.Current().SHA256,
		LoadedAt: app.Runtime.Current().LoadedAt,
		Raw:      app.Runtime.Current().Raw,
	})

	baseURL, shutdown := startMainHTTPServerForTest(t, app)
	defer shutdown()

	documentID := uploadFile(t, baseURL, "ocr_sentinel.pdf", []byte("not a real pdf"), map[string]string{
		"author":      "OCR Tester",
		"title":       "HTTP OCR sentinel",
		"pdf_use_ocr": "true",
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/api/v1/documents/%d/analyze", baseURL, documentID), nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Contains(t, []int{http.StatusOK, http.StatusInternalServerError}, resp.StatusCode, "parser must run before any LLM result")

	parseRun, err := app.DocumentService.GetLatestParseRunByDocumentID(ctx, documentID)
	require.NoError(t, err)
	require.Equal(t, "PARSED", parseRun.Status)
	require.Equal(t, "pdf-ocr", parseRun.ParserName)
	require.Equal(t, true, parseRun.RawMetadata["pdf_ocr_used"])
	require.Contains(t, parseRun.CleanedText, "OCR_SENTINEL")
	require.Contains(t, parseRun.CleanedText, "600519.SH")
}

func TestHTTPUploadAllFuturePDFs(t *testing.T) {
	if os.Getenv("FINANCE_SYS_HTTP_INGEST_ALL_TESTDATA") != "1" {
		t.Skip("set FINANCE_SYS_HTTP_INGEST_ALL_TESTDATA=1 to upload every PDF under testdata/游资大V复盘文章汇总2026")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Hour)
	defer cancel()

	app := buildIntegrationApp(t, ctx)
	defer app.Close()

	cfg := cloneConfig(t, app.Runtime.Config())
	cfg.Document.AutoAnalyzeUpload = false
	cfg.Document.PDFOCR = config.PDFOCRConfig{
		Enabled:              true,
		Command:              filepath.Join("..", "..", "tools", "guziyuan_pdf_ocr_tool", "ocr_pdf.bat"),
		Args:                 []string{"{input}", "--stdout"},
		MinTextChars:         80,
		TimeoutMS:            120000,
		TreatExitCodeOneAsOK: true,
	}
	app.Runtime.Update(&config.Snapshot{
		Config:   cfg,
		Source:   app.Runtime.Current().Source,
		SHA256:   app.Runtime.Current().SHA256,
		LoadedAt: app.Runtime.Current().LoadedAt,
		Raw:      app.Runtime.Current().Raw,
	})

	baseURL, shutdown := startMainHTTPServerForTest(t, app)
	defer shutdown()

	root := filepath.Join("..", "..", "testdata", "游资大V复盘文章汇总2026")
	shouldAnalyze := os.Getenv("FINANCE_SYS_HTTP_ANALYZE_ALL_TESTDATA") == "1"
	var total, failed int
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		require.NoError(t, walkErr)
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(path), ".pdf") {
			return nil
		}
		total++
		content, err := os.ReadFile(path)
		if err != nil {
			failed++
			t.Logf("read failed: %s: %v", path, err)
			return nil
		}
		documentID, err := uploadFileE(baseURL, filepath.Base(path), content, map[string]string{
			"author":      inferAuthorFromFileName(filepath.Base(path)),
			"title":       strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
			"pdf_use_ocr": "true",
		})
		if err != nil {
			failed++
			t.Logf("upload failed: %s: %v", path, err)
			return nil
		}
		if shouldAnalyze {
			status, err := analyzeDocument(baseURL, documentID)
			if err != nil {
				failed++
				t.Logf("analyze request failed: %s: %v", path, err)
				return nil
			}
			if status != http.StatusOK && status != http.StatusInternalServerError {
				failed++
				t.Logf("analyze unexpected status: %s: %d", path, status)
			}
		}
		return nil
	})
	require.NoError(t, err)
	require.Greater(t, total, 0)
	require.Zero(t, failed)
}

func analyzeDocument(baseURL string, documentID int64) (int, error) {
	resp, err := http.Post(fmt.Sprintf("%s/api/v1/documents/%d/analyze", baseURL, documentID), "application/json", nil)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}

func buildIntegrationApp(t *testing.T, ctx context.Context) *bootstrap.App {
	t.Helper()
	app, err := bootstrap.Build(ctx)
	require.NoError(t, err)
	return app
}

func startMainHTTPServerForTest(t *testing.T, app *bootstrap.App) (string, func()) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	server := newHTTPServer(app, listener.Addr().String())
	done := make(chan struct{})
	go func() {
		defer close(done)
		err := server.Serve(listener)
		if err != nil && err != http.ErrServerClosed {
			t.Errorf("api server failed: %v", err)
		}
	}()

	shutdown := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
		<-done
	}
	return "http://" + listener.Addr().String(), shutdown
}

func uploadFile(t *testing.T, baseURL, name string, content []byte, fields map[string]string) int64 {
	t.Helper()
	id, err := uploadFileE(baseURL, name, content, fields)
	require.NoError(t, err)
	return id
}

func uploadFileE(baseURL, name string, content []byte, fields map[string]string) (int64, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			return 0, err
		}
	}
	part, err := writer.CreateFormFile("file", name)
	if err != nil {
		return 0, err
	}
	if _, err := part.Write(content); err != nil {
		return 0, err
	}
	if err := writer.Close(); err != nil {
		return 0, err
	}

	resp, err := http.Post(baseURL+"/api/v1/documents/upload", writer.FormDataContentType(), &body)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("upload http %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var payload struct {
		Document struct {
			ID int64 `json:"id"`
		} `json:"document"`
	}
	if err := json.Unmarshal(respBody, &payload); err != nil {
		return 0, err
	}
	if payload.Document.ID == 0 {
		return 0, fmt.Errorf("upload response missing document.id")
	}
	return payload.Document.ID, nil
}

func cloneConfig(t *testing.T, cfg *config.Config) *config.Config {
	t.Helper()
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	var clone config.Config
	require.NoError(t, json.Unmarshal(raw, &clone))
	return &clone
}

func inferAuthorFromFileName(name string) string {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	for i, r := range base {
		if r >= '0' && r <= '9' {
			return strings.TrimSpace(base[:i])
		}
	}
	return ""
}
