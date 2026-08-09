package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveProductionParseRunPrefersExactParser(t *testing.T) {
	runs := []parseRunRow{
		{ID: 3, DocumentID: 7, Status: "PARSED", ParserName: "pdf_kit", ParserVersion: "v2"},
		{ID: 1, DocumentID: 7, Status: "PARSED", ParserName: "pdf_ocr", ParserVersion: "v1"},
		{ID: 2, DocumentID: 7, Status: "PARSED", ParserName: "pdf_kit", ParserVersion: "v1"},
	}
	id, fallback := resolveProductionParseRun(runs, parseRunRow{ID: 99, ParserName: "pdf_kit", ParserVersion: "v2"})
	require.Equal(t, int64(3), id)
	require.False(t, fallback)
}

func TestResolveProductionParseRunFallsBackToLatest(t *testing.T) {
	runs := []parseRunRow{
		{ID: 5, DocumentID: 7, Status: "PARSED", ParserName: "pdf_ocr", ParserVersion: "v9"},
		{ID: 2, DocumentID: 7, Status: "PARSED", ParserName: "pdf_kit", ParserVersion: "v1"},
	}
	id, fallback := resolveProductionParseRun(runs, parseRunRow{ID: 1, ParserName: "legacy_parser", ParserVersion: "v0"})
	require.Equal(t, int64(5), id)
	require.True(t, fallback)
}

func TestResolveProductionParseRunEmpty(t *testing.T) {
	id, fallback := resolveProductionParseRun(nil, parseRunRow{ID: 1, ParserName: "pdf_kit", ParserVersion: "v1"})
	require.Equal(t, int64(0), id)
	require.True(t, fallback)
}

func TestBloggerBusinessKeyIsStable(t *testing.T) {
	require.Equal(t, bloggerBusinessKey("a", "b"), bloggerBusinessKey("a", "b"))
	require.NotEqual(t, bloggerBusinessKey("a", "b"), bloggerBusinessKey("a", "c"))
}
