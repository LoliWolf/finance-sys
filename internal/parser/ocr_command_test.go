package parser

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveOCRCommandFindsProjectRelativeTool(t *testing.T) {
	for _, relativePath := range []string{
		"tools/guziyuan_pdf_ocr_tool/ocr_pdf.bat",
		`tools\guziyuan_pdf_ocr_tool\ocr_pdf.bat`,
	} {
		command, err := resolveOCRCommand(relativePath)
		require.NoError(t, err)
		require.True(t, filepath.IsAbs(command))
		require.FileExists(t, command)
		require.True(t, strings.HasSuffix(filepath.ToSlash(command), "tools/guziyuan_pdf_ocr_tool/ocr_pdf.bat"))
	}
}

func TestDOCTextExtractorsPreferMacOSTextutil(t *testing.T) {
	extractors := docTextExtractors("darwin", "/tmp/sample.doc")
	require.Len(t, extractors, 2)
	require.Equal(t, "textutil", extractors[0].name)
	require.Equal(t, "/usr/bin/textutil", extractors[0].command)
	require.Equal(t, []string{"-convert", "txt", "-stdout", "--", "/tmp/sample.doc"}, extractors[0].args)
	require.Equal(t, "antiword", extractors[1].name)
}

func TestDOCTextExtractorsUseAntiwordOutsideMacOS(t *testing.T) {
	for _, goos := range []string{"windows", "linux"} {
		extractors := docTextExtractors(goos, "sample.doc")
		require.Len(t, extractors, 1)
		require.Equal(t, "antiword", extractors[0].name)
	}
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

func TestMapWindowsBatchForPlatformUsesSiblingShellScript(t *testing.T) {
	batchCommand, err := resolveOCRCommand("tools/guziyuan_pdf_ocr_tool/ocr_pdf.bat")
	require.NoError(t, err)

	command, mapped, err := mapWindowsBatchForPlatform("darwin", batchCommand)
	require.NoError(t, err)
	require.True(t, mapped)
	require.True(t, filepath.IsAbs(command))
	require.FileExists(t, command)
	require.True(t, strings.HasSuffix(filepath.ToSlash(command), "tools/guziyuan_pdf_ocr_tool/ocr_pdf.sh"))
}

func TestMapWindowsBatchForPlatformPreservesWindowsCommand(t *testing.T) {
	command, mapped, err := mapWindowsBatchForPlatform("windows", `C:\tools\ocr_pdf.bat`)
	require.NoError(t, err)
	require.False(t, mapped)
	require.Equal(t, `C:\tools\ocr_pdf.bat`, command)
}

func TestBuildOCRExecWrapsPythonScript(t *testing.T) {
	command, args, err := buildOCRExec("tools/guziyuan_pdf_ocr_tool/ocr_pdf_articles.py", []string{"input.pdf", "--stdout"})
	require.NoError(t, err)
	require.NotEmpty(t, command)
	require.GreaterOrEqual(t, len(args), 3)
	require.True(t, filepath.IsAbs(args[0]))
	require.True(t, strings.HasSuffix(filepath.ToSlash(args[0]), "tools/guziyuan_pdf_ocr_tool/ocr_pdf_articles.py"))
	require.Equal(t, "input.pdf", args[1])
	require.Equal(t, "--stdout", args[2])
}

func TestPythonInterpreterCandidatesArePlatformSpecific(t *testing.T) {
	require.Equal(t,
		[]string{filepath.Join("agent", ".venv", "Scripts", "python.exe")},
		pythonVirtualenvPaths("windows"),
	)
	require.Equal(t, []string{"python.exe", "python", "py"}, pythonExecutableNames("windows"))
	require.Equal(t,
		[]string{
			filepath.Join("agent", ".venv", "bin", "python3"),
			filepath.Join("agent", ".venv", "bin", "python"),
		},
		pythonVirtualenvPaths("darwin"),
	)
	require.Equal(t, []string{"python3", "python"}, pythonExecutableNames("darwin"))
}
