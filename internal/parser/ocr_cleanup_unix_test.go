//go:build !windows

package parser

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"finance-sys/internal/config"
	"github.com/stretchr/testify/require"
)

func TestOCRWorkspaceRemovedOnSuccessFailureAndTimeout(t *testing.T) {
	for _, mode := range []string{"success", "failure", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			record := filepath.Join(dir, "workspace.txt")
			script := `printf '%s' "$FINANCE_SYS_OCR_WORK_DIR" > "$1"; touch "$FINANCE_SYS_OCR_WORK_DIR/rendered.png"; `
			timeout := 5000
			switch mode {
			case "success":
				script += `printf 'recognized text'`
			case "failure":
				script += `exit 2`
			case "timeout":
				timeout = 100
				script += `sleep 20 & wait`
			}
			scriptPath := filepath.Join(dir, "ocr.sh")
			require.NoError(t, os.WriteFile(scriptPath, []byte(script), 0600))
			_, err := parsePDFWithOCR(context.Background(), "unused.pdf", config.PDFOCRConfig{
				Command: "/bin/sh", Args: []string{scriptPath, record}, TimeoutMS: timeout,
			})
			if mode == "success" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			raw, readErr := os.ReadFile(record)
			require.NoError(t, readErr)
			require.NoDirExists(t, string(raw))
		})
	}
}

func TestOCRTimeoutKillsDescendantBeforeItCanWriteAgain(t *testing.T) {
	dir := t.TempDir()
	sentinel := filepath.Join(dir, "orphan.txt")
	scriptPath := filepath.Join(dir, "ocr.sh")
	require.NoError(t, os.WriteFile(scriptPath, []byte(`(sleep 1; printf leaked > "$1") & wait`), 0600))
	_, err := parsePDFWithOCR(context.Background(), "unused.pdf", config.PDFOCRConfig{
		Command:   "/bin/sh",
		Args:      []string{scriptPath, sentinel},
		TimeoutMS: 100,
	})
	require.Error(t, err)
	time.Sleep(1100 * time.Millisecond)
	require.NoFileExists(t, sentinel)
}
