package service

import (
	"context"
	"errors"
	"fmt"
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
	securityTSCodeRe = regexp.MustCompile(`^(?:\d{6}\.(?:SH|SZ|BJ)|BK\d{4}\.DC)$`)
	securitySymbolRe = regexp.MustCompile(`^(?:\d{6}|BK\d{4})$`)
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
		Query:      query,
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
	if securitySymbolRe.MatchString(upperQuery) {
		rows, err := dal.SecurityMasters.QueryBySymbol(ctx, s.db, upperQuery)
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

func (s *SecurityService) ResolveTradableCandidates(ctx context.Context, query string, maxCandidates int) ([]domain.InstrumentResolutionCandidate, error) {
	return s.resolveCandidates(ctx, query, maxCandidates, false)
}

func (s *SecurityService) ResolveTrackableCandidates(ctx context.Context, query string, maxCandidates int) ([]domain.InstrumentResolutionCandidate, error) {
	return s.resolveCandidates(ctx, query, maxCandidates, true)
}

func (s *SecurityService) resolveCandidates(ctx context.Context, query string, maxCandidates int, includeSectors bool) ([]domain.InstrumentResolutionCandidate, error) {
	lookup, err := s.Lookup(ctx, query)
	if err != nil {
		return nil, err
	}
	candidates := filterTrackableResolutionCandidates(uniqueResolutionCandidates(lookup), includeSectors)
	if maxCandidates > 0 && len(candidates) > maxCandidates {
		candidates = candidates[:maxCandidates]
	}
	return candidates, nil
}

func (s *SecurityService) VerifyTradableCandidate(ctx context.Context, tsCode string) (*domain.InstrumentResolutionCandidate, error) {
	return s.verifyCandidate(ctx, tsCode, false)
}

func (s *SecurityService) VerifyTrackableCandidate(ctx context.Context, tsCode string) (*domain.InstrumentResolutionCandidate, error) {
	return s.verifyCandidate(ctx, tsCode, true)
}

func (s *SecurityService) verifyCandidate(ctx context.Context, tsCode string, includeSectors bool) (*domain.InstrumentResolutionCandidate, error) {
	tsCode = strings.ToUpper(strings.TrimSpace(tsCode))
	if !securityTSCodeRe.MatchString(tsCode) {
		return nil, fmt.Errorf("invalid ts_code %q", tsCode)
	}
	candidates, err := s.resolveCandidates(ctx, tsCode, 2, includeSectors)
	if err != nil {
		return nil, err
	}
	for i := range candidates {
		if candidates[i].TSCode == tsCode {
			return &candidates[i], nil
		}
	}
	return nil, dal.ErrNotFound
}

func isActiveSecurity(row db_model.SecurityMaster) bool {
	return row.IsActive && row.ListStatus == "L"
}

func filterTrackableResolutionCandidates(candidates []domain.InstrumentResolutionCandidate, includeSectors bool) []domain.InstrumentResolutionCandidate {
	tradable := candidates[:0]
	for _, candidate := range candidates {
		if candidate.AssetType == domain.AssetTypeAShare || candidate.AssetType == domain.AssetTypeETF || (includeSectors && candidate.AssetType == domain.AssetTypeSector) {
			tradable = append(tradable, candidate)
		}
	}
	return tradable
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
		SectorType: row.SectorType,
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
