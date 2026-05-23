package service

import (
	"context"
	"errors"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"finance-sys/internal/dal"
	"finance-sys/internal/domain"
	"finance-sys/internal/domain/db_model"

	"gorm.io/gorm"
)

var (
	securityTSCodeRe = regexp.MustCompile(`^\d{6}\.(SH|SZ|BJ)$`)
	securitySymbolRe = regexp.MustCompile(`^\d{6}$`)
)

type SecurityService struct {
	db     *gorm.DB
	logger *slog.Logger
}

func NewSecurityService(db *gorm.DB, logger *slog.Logger) *SecurityService {
	return &SecurityService{db: db, logger: logger}
}

func NormalizeSecurityAlias(value string) string {
	fields := strings.Fields(strings.TrimSpace(value))
	return strings.ToLower(strings.Join(fields, " "))
}

func (s *SecurityService) Lookup(ctx context.Context, query string) (domain.SecurityLookupResult, error) {
	query = strings.TrimSpace(query)
	result := domain.SecurityLookupResult{
		Query:       query,
		Normalized: NormalizeSecurityAlias(query),
	}
	if query == "" {
		return result, nil
	}

	seenDirect := make(map[int64]struct{})
	addDirect := func(rows []db_model.SecurityMaster) {
		for i := range rows {
			if !isActiveSecurity(rows[i]) {
				continue
			}
			if _, exists := seenDirect[rows[i].ID]; exists {
				continue
			}
			seenDirect[rows[i].ID] = struct{}{}
			result.DirectMatches = append(result.DirectMatches, mapSecurityMaster(&rows[i]))
		}
	}

	upperQuery := strings.ToUpper(query)
	if securityTSCodeRe.MatchString(upperQuery) {
		row, err := dal.SecurityMasters.QueryByTSCode(ctx, s.db, upperQuery)
		if err != nil && !errors.Is(err, dal.ErrNotFound) {
			return result, err
		}
		if row != nil {
			addDirect([]db_model.SecurityMaster{*row})
		}
	}
	if securitySymbolRe.MatchString(query) {
		rows, err := dal.SecurityMasters.QueryBySymbol(ctx, s.db, query)
		if err != nil {
			return result, err
		}
		addDirect(rows)
	}
	nameRows, err := dal.SecurityMasters.QueryActiveByName(ctx, s.db, query)
	if err != nil {
		return result, err
	}
	addDirect(nameRows)

	if result.Normalized != "" {
		aliases, err := dal.SecurityAliases.QueryActiveByNormalizedAlias(ctx, s.db, result.Normalized)
		if err != nil {
			return result, err
		}
		for i := range aliases {
			security, err := dal.SecurityMasters.QueryByID(ctx, s.db, aliases[i].SecurityMasterID)
			if err != nil {
				if errors.Is(err, dal.ErrNotFound) {
					continue
				}
				return result, err
			}
			if !isActiveSecurity(*security) {
				continue
			}
			result.AliasMatches = append(result.AliasMatches, domain.SecurityAliasMatch{
				Alias:    mapSecurityAlias(&aliases[i]),
				Security: mapSecurityMaster(security),
			})
		}
	}

	if s.logger != nil {
		s.logger.InfoContext(ctx, "security lookup completed", "query", query, "direct_count", len(result.DirectMatches), "alias_count", len(result.AliasMatches))
	}
	return result, nil
}

func isActiveSecurity(row db_model.SecurityMaster) bool {
	return row.IsActive && row.ListStatus == "L"
}

func mapSecurityMaster(row *db_model.SecurityMaster) domain.SecurityMaster {
	return domain.SecurityMaster{
		ID:         row.ID,
		TSCode:     row.TSCode,
		Symbol:     row.Symbol,
		Name:       row.Name,
		FullName:   row.FullName,
		Exchange:   row.Exchange,
		Market:     row.Market,
		AssetType:  row.AssetType,
		ListStatus: row.ListStatus,
		ListDate:   utcPtr(row.ListDate),
		DelistDate: utcPtr(row.DelistDate),
		Industry:   row.Industry,
		IsActive:   row.IsActive,
		Source:     row.Source,
		CreatedAt:  row.CreatedAt.UTC(),
		UpdatedAt:  row.UpdatedAt.UTC(),
	}
}

func mapSecurityAlias(row *db_model.SecurityAlias) domain.SecurityAlias {
	return domain.SecurityAlias{
		ID:               row.ID,
		SecurityMasterID: row.SecurityMasterID,
		Alias:            row.AliasName,
		NormalizedAlias:  row.NormalizedAlias,
		AliasType:        row.AliasType,
		Source:           row.Source,
		Confidence:       row.Confidence,
		IsActive:         row.IsActive,
		CreatedAt:        row.CreatedAt.UTC(),
		UpdatedAt:        row.UpdatedAt.UTC(),
	}
}

func utcPtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	converted := value.UTC()
	return &converted
}
