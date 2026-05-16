package domain

import "time"

type Document struct {
	ID            int64          `json:"id"`
	Author        string         `json:"author"`
	Institution   string         `json:"institution"`
	Title         string         `json:"title"`
	FileName      string         `json:"file_name"`
	SHA256        string         `json:"sha256"`
	PDFOCREnabled bool           `json:"pdf_ocr_enabled"`
	Status        DocumentStatus `json:"status"`
	ConfigVersion int64          `json:"config_version"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
}

type DocumentIngestRequest struct {
	Author      string
	Institution string
	Title       string
	FileName    string
	Content     []byte
	PDFUseOCR   bool
}

type ParseRun struct {
	ID            int64          `json:"id"`
	DocumentID    int64          `json:"document_id"`
	Status        ParseRunStatus `json:"status"`
	ParserName    ParserName     `json:"parser_name"`
	ParserVersion string         `json:"parser_version"`
	ErrorMessage  string         `json:"error_message"`
	CleanedText   string         `json:"cleaned_text"`
	Chunks        []Chunk        `json:"chunks"`
	RawMetadata   map[string]any `json:"raw_metadata"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
}

type Chunk struct {
	Index int    `json:"index"`
	Text  string `json:"text"`
}
