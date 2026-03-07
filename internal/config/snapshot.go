package config

import (
	"encoding/json"
	"time"

	"finance-sys/internal/utils"
)

func NewSnapshot(cfg *Config, raw []byte, source string, loadedAt time.Time) (*Snapshot, error) {
	if len(raw) == 0 {
		var err error
		raw, err = json.Marshal(cfg)
		if err != nil {
			return nil, err
		}
	}
	return &Snapshot{
		Config:   cfg,
		Source:   source,
		SHA256:   utils.SHA256Hex(raw),
		LoadedAt: loadedAt.UTC(),
		Raw:      raw,
	}, nil
}
