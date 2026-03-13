package domain

import "time"

type Document struct {
	ID            int64     `json:"id"`
	SourceType    string    `json:"source_type"`
	SourceName    string    `json:"source_name"`
	Author        string    `json:"author"`
	Institution   string    `json:"institution"`
	Title         string    `json:"title"`
	FileName      string    `json:"file_name"`
	Extension     string    `json:"extension"`
	ContentType   string    `json:"content_type"`
	SHA256        string    `json:"sha256"`
	Status        string    `json:"status"`
	ConfigVersion int64     `json:"config_version"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type DocumentIngestRequest struct {
	SourceType  string
	SourceName  string
	Author      string
	Institution string
	Title       string
	FileName    string
	ContentType string
	Content     []byte
}

type ParseRun struct {
	ID            int64          `json:"id"`
	DocumentID    int64          `json:"document_id"`
	Status        string         `json:"status"`
	ParserName    string         `json:"parser_name"`
	ParserVersion string         `json:"parser_version"`
	ErrorMessage  string         `json:"error_message"`
	PageCount     int            `json:"page_count"`
	ContentText   string         `json:"content_text"`
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
