package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"finance-sys/internal/bootstrap"
	"finance-sys/internal/domain"
)

func main() {
	root := flag.String("root", filepath.Join("testdata", "sample_docs"), "directory containing documents to ingest")
	limit := flag.Int("limit", 0, "maximum number of files to ingest; 0 means all")
	flag.Parse()

	ctx := context.Background()
	app, err := bootstrap.Build(ctx)
	if err != nil {
		panic(err)
	}
	defer app.Close()

	allowed := allowedExtensions(app.Runtime.Config().Document.AllowedExtensions)
	var total, created, duplicated, failed int
	err = filepath.WalkDir(*root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			failed++
			slog.Error("walk testdata failed", "path", path, "error", walkErr.Error())
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		if *limit > 0 && total >= *limit {
			return filepath.SkipAll
		}
		ext := strings.ToLower(filepath.Ext(path))
		if _, ok := allowed[ext]; !ok {
			return nil
		}

		total++
		content, err := os.ReadFile(path)
		if err != nil {
			failed++
			slog.Error("read testdata file failed", "path", path, "error", err.Error())
			return nil
		}

		document, duplicate, err := app.DocumentService.IngestDocument(ctx, domain.DocumentIngestRequest{
			Author:   inferAuthor(path),
			Title:    strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
			FileName: filepath.Base(path),
			Content:  content,
		})
		if err != nil {
			failed++
			slog.Error("ingest testdata file failed", "path", path, "error", err.Error())
			return nil
		}
		if duplicate {
			duplicated++
		} else {
			created++
		}
		slog.Info("ingested testdata file", "document_id", document.ID, "duplicate", duplicate, "path", path)
		return nil
	})
	if err != nil {
		panic(err)
	}

	fmt.Printf("ingest completed: total=%d created=%d duplicated=%d failed=%d\n", total, created, duplicated, failed)
	if failed > 0 {
		os.Exit(1)
	}
}

func allowedExtensions(items []string) map[string]struct{} {
	result := make(map[string]struct{}, len(items))
	for _, item := range items {
		result[strings.ToLower(item)] = struct{}{}
	}
	return result
}

func inferAuthor(path string) string {
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	for i, r := range base {
		if r >= '0' && r <= '9' {
			return strings.TrimSpace(base[:i])
		}
	}
	return ""
}
