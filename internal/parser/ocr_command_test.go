package parser

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveOCRCommandFindsProjectRelativeTool(t *testing.T) {
	command, err := resolveOCRCommand("tools/guziyuan_pdf_ocr_tool/ocr_pdf.bat")
	require.NoError(t, err)
	require.True(t, filepath.IsAbs(command))
	require.FileExists(t, command)
	require.True(t, strings.HasSuffix(filepath.ToSlash(command), "tools/guziyuan_pdf_ocr_tool/ocr_pdf.bat"))
}

func TestBuildOCRExecWrapsWindowsBatchWithCmd(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows batch wrapping is Windows-specific")
	}

	command, args, err := buildOCRExec("tools/guziyuan_pdf_ocr_tool/ocr_pdf.bat", []string{"input.pdf", "--stdout"})
	require.NoError(t, err)
	require.True(t, strings.EqualFold(filepath.Base(command), "cmd.exe") || strings.EqualFold(command, "cmd"))
	require.GreaterOrEqual(t, len(args), 4)
	require.Equal(t, "/c", args[0])
	require.True(t, filepath.IsAbs(args[1]))
	require.Equal(t, "input.pdf", args[2])
	require.Equal(t, "--stdout", args[3])
}
