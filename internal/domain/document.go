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
	ObjectKey     string    `json:"object_key"`
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
	RequiresOCR   bool           `json:"requires_ocr"`
	ErrorMessage  string         `json:"error_message"`
	PageCount     int            `json:"page_count"`
	TextDensity   float64        `json:"text_density"`
	ContentText   string         `json:"content_text"`
	CleanedText   string         `json:"cleaned_text"`
	Sections      []Section      `json:"sections"`
	Chunks        []Chunk        `json:"chunks"`
	Tables        []Table        `json:"tables"`
	RawMetadata   map[string]any `json:"raw_metadata"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
}

type Section struct {
	Heading string `json:"heading"`
	Text    string `json:"text"`
}

type Chunk struct {
	Index int    `json:"index"`
	Text  string `json:"text"`
}

type Table struct {
	Title string     `json:"title"`
	Rows  [][]string `json:"rows"`
}
