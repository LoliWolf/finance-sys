package domain

import "time"

type ConfigSnapshot struct {
	ID            int64     `json:"id"`
	ConfigVersion int64     `json:"config_version"`
	Source        string    `json:"source"`
	SHA256        string    `json:"sha256"`
	RawJSON       string    `json:"raw_json"`
	CreatedAt     time.Time `json:"created_at"`
}
