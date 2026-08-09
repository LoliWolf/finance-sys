package domain

import "time"

type SecurityMaster struct {
	ID         int64      `json:"id"`
	TSCode     string     `json:"ts_code"`
	Symbol     string     `json:"symbol"`
	Name       string     `json:"name"`
	FullName   string     `json:"full_name"`
	Exchange   string     `json:"exchange"`
	Market     string     `json:"market"`
	AssetType  string     `json:"asset_type"`
	ListStatus string     `json:"list_status"`
	ListDate   *time.Time `json:"list_date,omitempty"`
	DelistDate *time.Time `json:"delist_date,omitempty"`
	Industry   string     `json:"industry"`
	SectorType string     `json:"sector_type"`
	IsActive   bool       `json:"is_active"`
	Source     string     `json:"source"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

type SecurityAlias struct {
	ID               int64     `json:"id"`
	SecurityMasterID int64     `json:"security_master_id"`
	Alias            string    `json:"alias"`
	NormalizedAlias  string    `json:"normalized_alias"`
	AliasType        string    `json:"alias_type"`
	Source           string    `json:"source"`
	Confidence       float64   `json:"confidence"`
	IsActive         bool      `json:"is_active"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type SecurityAliasMatch struct {
	Alias    SecurityAlias  `json:"alias"`
	Security SecurityMaster `json:"security"`
}

type SecurityLookupResult struct {
	Query         string               `json:"query"`
	Normalized    string               `json:"normalized"`
	DirectMatches []SecurityMaster     `json:"direct_matches"`
	AliasMatches  []SecurityAliasMatch `json:"alias_matches"`
}

func (r SecurityLookupResult) Empty() bool {
	return len(r.DirectMatches) == 0 && len(r.AliasMatches) == 0
}
