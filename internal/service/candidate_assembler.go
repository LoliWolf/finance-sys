package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"finance-sys/internal/domain"
)

type SecurityLookup interface {
	Lookup(ctx context.Context, query string) (domain.SecurityLookupResult, error)
}

type CandidateAssembler struct {
	security SecurityLookup
	logger   *slog.Logger
}

var errUntrackableInstrument = errors.New("untrackable instrument")

func NewCandidateAssembler(security SecurityLookup, logger *slog.Logger) *CandidateAssembler {
	return &CandidateAssembler{security: security, logger: logger}
}

func (a *CandidateAssembler) Assemble(ctx context.Context, intents []domain.PlanIntent) ([]domain.TrackablePlanIntent, []domain.InstrumentResolution, error) {
	if a == nil || a.security == nil {
		return nil, nil, fmt.Errorf("candidate assembler requires security lookup")
	}
	if len(intents) == 0 {
		return nil, nil, fmt.Errorf("no plan intents to assemble")
	}

	trackable := make([]domain.TrackablePlanIntent, 0, len(intents))
	resolutions := make([]domain.InstrumentResolution, 0, len(intents))
	var errs []error
	var skippedAmbiguousErrs []error
	var skippedNotFoundErrs []error

	for _, intent := range intents {
		item, resolution, err := a.assembleOne(ctx, intent)
		resolutions = append(resolutions, resolution)
		if err != nil {
			if resolution.Status == domain.InstrumentResolutionStatusUntrackable {
				if a.logger != nil {
					a.logger.InfoContext(ctx, "candidate assembler skipped untrackable target", "raw_symbol", resolution.RawSymbol, "target_kind", resolution.TargetKind, "reason", resolution.Reason)
				}
				continue
			}
			if resolution.Status == domain.InstrumentResolutionStatusAmbiguous {
				if a.logger != nil {
					a.logger.InfoContext(ctx, "candidate assembler skipped ambiguous target", "raw_symbol", resolution.RawSymbol, "target_kind", resolution.TargetKind, "reason", resolution.Reason)
				}
				skippedAmbiguousErrs = append(skippedAmbiguousErrs, err)
				continue
			}
			if resolution.Status == domain.InstrumentResolutionStatusNotFound && strings.TrimSpace(resolution.Reason) == "no active security matched" {
				if a.logger != nil {
					a.logger.InfoContext(ctx, "candidate assembler skipped unresolved target", "raw_symbol", resolution.RawSymbol, "target_kind", resolution.TargetKind, "reason", resolution.Reason)
				}
				skippedNotFoundErrs = append(skippedNotFoundErrs, err)
				continue
			}
			errs = append(errs, err)
			continue
		}
		trackable = append(trackable, item)
	}

	if len(errs) > 0 {
		return nil, resolutions, errors.Join(errs...)
	}
	if len(trackable) == 0 {
		if len(skippedAmbiguousErrs) > 0 {
			allErrs := append(skippedAmbiguousErrs, skippedNotFoundErrs...)
			return nil, resolutions, errors.Join(allErrs...)
		}
		if len(skippedNotFoundErrs) > 0 {
			return nil, resolutions, errors.Join(skippedNotFoundErrs...)
		}
		return nil, resolutions, fmt.Errorf("no trackable securities resolved from %d plan intents", len(intents))
	}
	return trackable, resolutions, nil
}

func (a *CandidateAssembler) assembleOne(ctx context.Context, intent domain.PlanIntent) (domain.TrackablePlanIntent, domain.InstrumentResolution, error) {
	rawSymbol := strings.TrimSpace(intent.Symbol)
	resolution := domain.InstrumentResolution{
		RawSymbol:       rawSymbol,
		NormalizedQuery: NormalizeSecurityAlias(rawSymbol),
		TargetKind:      domain.InstrumentTargetKindUnknown,
	}

	lookup, err := a.security.Lookup(ctx, rawSymbol)
	if err != nil {
		resolution.Status = domain.InstrumentResolutionStatusNotFound
		resolution.Reason = err.Error()
		return domain.TrackablePlanIntent{}, resolution, err
	}

	candidates := uniqueResolutionCandidates(lookup)
	resolution.Candidates = candidates
	switch len(candidates) {
	case 1:
		candidate := candidates[0]
		if candidate.AssetType != domain.AssetTypeAShare && candidate.AssetType != domain.AssetTypeETF && candidate.AssetType != domain.AssetTypeSector {
			resolution.Status = domain.InstrumentResolutionStatusUntrackable
			resolution.TargetKind = domain.InstrumentTargetKindUnknown
			resolution.Reason = fmt.Sprintf("unsupported asset type %q", candidate.AssetType)
			return domain.TrackablePlanIntent{}, resolution, errUntrackableInstrument
		}
		resolution.Status = domain.InstrumentResolutionStatusResolved
		resolution.TargetKind = targetKindForAsset(candidate.AssetType)
		resolution.Reason = "matched active security master"
		return toTrackablePlanIntent(intent, candidate), resolution, nil
	default:
		if len(candidates) > 1 {
			resolution.Status = domain.InstrumentResolutionStatusAmbiguous
			resolution.TargetKind = domain.InstrumentTargetKindUnknown
			resolution.Reason = fmt.Sprintf("matched %d active securities", len(candidates))
			return domain.TrackablePlanIntent{}, resolution, fmt.Errorf("ambiguous instrument %q: matched %d active securities", rawSymbol, len(candidates))
		}
	}

	if kind, reason, ok := classifyUntrackableTarget(rawSymbol); ok {
		resolution.Status = domain.InstrumentResolutionStatusUntrackable
		resolution.TargetKind = kind
		resolution.Reason = reason
		return domain.TrackablePlanIntent{}, resolution, errUntrackableInstrument
	}

	resolution.Status = domain.InstrumentResolutionStatusNotFound
	resolution.TargetKind = domain.InstrumentTargetKindUnknown
	resolution.Reason = "no active security matched"
	return domain.TrackablePlanIntent{}, resolution, fmt.Errorf("security not found for instrument %q", rawSymbol)
}

func uniqueResolutionCandidates(lookup domain.SecurityLookupResult) []domain.InstrumentResolutionCandidate {
	candidates := make([]domain.InstrumentResolutionCandidate, 0, len(lookup.DirectMatches)+len(lookup.AliasMatches))
	seen := make(map[string]struct{})
	add := func(security domain.SecurityMaster, source string) {
		key := security.TSCode
		if key == "" {
			key = fmt.Sprintf("%d", security.ID)
		}
		if _, exists := seen[key]; exists {
			return
		}
		assetType, ok := mapSecurityAssetType(security.AssetType)
		if !ok {
			assetType = domain.AssetType(security.AssetType)
		}
		market, ok := mapSecurityMarket(security)
		if !ok {
			market = domain.Market(strings.ToUpper(strings.TrimSpace(security.Market)))
		}
		seen[key] = struct{}{}
		candidates = append(candidates, domain.InstrumentResolutionCandidate{
			TSCode:      strings.ToUpper(strings.TrimSpace(security.TSCode)),
			Symbol:      strings.TrimSpace(security.Symbol),
			Name:        strings.TrimSpace(security.Name),
			AssetType:   assetType,
			Market:      market,
			MatchSource: source,
		})
	}

	for _, item := range lookup.DirectMatches {
		add(item, "direct")
	}
	for _, item := range lookup.AliasMatches {
		add(item.Security, "alias")
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].TSCode < candidates[j].TSCode
	})
	return candidates
}

func toTrackablePlanIntent(intent domain.PlanIntent, candidate domain.InstrumentResolutionCandidate) domain.TrackablePlanIntent {
	return domain.TrackablePlanIntent{
		Analyst:            intent.Analyst,
		Institution:        intent.Institution,
		RawSymbol:          strings.TrimSpace(intent.Symbol),
		TSCode:             candidate.TSCode,
		Symbol:             candidate.Symbol,
		SecurityName:       candidate.Name,
		AssetType:          candidate.AssetType,
		Market:             candidate.Market,
		Direction:          intent.Direction,
		ReferencePrice:     intent.ReferencePrice,
		ReferencePriceNote: intent.ReferencePriceNote,
		Thesis:             intent.Thesis,
		Evidence:           intent.Evidence,
		Risks:              intent.Risks,
		Confidence:         intent.Confidence,
	}
}

func mapSecurityAssetType(value string) (domain.AssetType, bool) {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "STOCK", "A_SHARE", "ASHARE":
		return domain.AssetTypeAShare, true
	case "ETF":
		return domain.AssetTypeETF, true
	case "SECTOR":
		return domain.AssetTypeSector, true
	default:
		return "", false
	}
}

func mapSecurityMarket(security domain.SecurityMaster) (domain.Market, bool) {
	switch strings.ToUpper(strings.TrimSpace(security.Market)) {
	case "SH":
		return domain.MarketSH, true
	case "SZ":
		return domain.MarketSZ, true
	case "BJ":
		return domain.MarketBJ, true
	case "DC":
		return domain.MarketDC, true
	}
	switch {
	case strings.HasSuffix(strings.ToUpper(security.TSCode), ".SH"):
		return domain.MarketSH, true
	case strings.HasSuffix(strings.ToUpper(security.TSCode), ".SZ"):
		return domain.MarketSZ, true
	case strings.HasSuffix(strings.ToUpper(security.TSCode), ".BJ"):
		return domain.MarketBJ, true
	case strings.HasSuffix(strings.ToUpper(security.TSCode), ".DC"):
		return domain.MarketDC, true
	}
	switch strings.ToUpper(strings.TrimSpace(security.Exchange)) {
	case "SSE", "SHSE":
		return domain.MarketSH, true
	case "SZSE":
		return domain.MarketSZ, true
	case "BSE":
		return domain.MarketBJ, true
	case "DC":
		return domain.MarketDC, true
	default:
		return "", false
	}
}

func targetKindForAsset(assetType domain.AssetType) domain.InstrumentTargetKind {
	if assetType == domain.AssetTypeETF {
		return domain.InstrumentTargetKindETF
	}
	if assetType == domain.AssetTypeSector {
		return domain.InstrumentTargetKindSector
	}
	return domain.InstrumentTargetKindStock
}

func classifyUntrackableTarget(raw string) (domain.InstrumentTargetKind, string, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return domain.InstrumentTargetKindUnknown, "", false
	}
	switch {
	case strings.Contains(value, "板块"):
		return domain.InstrumentTargetKindSector, "sector is not directly tradable in M3", true
	case strings.Contains(value, "行业"):
		return domain.InstrumentTargetKindIndustry, "industry is not directly tradable in M3", true
	case strings.Contains(value, "指数"):
		return domain.InstrumentTargetKindIndex, "index is not directly tradable in M3", true
	case strings.Contains(value, "主题"), strings.Contains(value, "概念"):
		return domain.InstrumentTargetKindTheme, "theme is not directly tradable in M3", true
	case strings.Contains(value, "相关标的"), strings.Contains(value, "龙头股"), strings.Contains(value, "个股"):
		return domain.InstrumentTargetKindBroadPhrase, "broad phrase is not a single tradable security", true
	case strings.HasPrefix(value, "A股"), strings.HasPrefix(value, "港股"), strings.HasPrefix(value, "美股"):
		return domain.InstrumentTargetKindBroadPhrase, "market-wide phrase is not a single tradable security", true
	default:
		return domain.InstrumentTargetKindUnknown, "", false
	}
}
