package service_test

import (
	"context"
	"testing"

	"finance-sys/internal/domain"
	"finance-sys/internal/service"

	"github.com/stretchr/testify/require"
)

type fakeSecurityLookup map[string]domain.SecurityLookupResult

func (f fakeSecurityLookup) Lookup(_ context.Context, query string) (domain.SecurityLookupResult, error) {
	return f[query], nil
}

func TestCandidateAssemblerResolvesDirectAndAliasAndSkipsUntrackable(t *testing.T) {
	assembler := service.NewCandidateAssembler(fakeSecurityLookup{
		"新易盛": {
			Query:      "新易盛",
			Normalized: "新易盛",
			DirectMatches: []domain.SecurityMaster{{
				TSCode:     "300502.SZ",
				Symbol:     "300502",
				Name:       "新易盛",
				Market:     "SZ",
				AssetType:  "STOCK",
				ListStatus: "L",
				IsActive:   true,
			}},
		},
		"旭创": {
			Query:      "旭创",
			Normalized: "旭创",
			AliasMatches: []domain.SecurityAliasMatch{{
				Alias: domain.SecurityAlias{Alias: "旭创"},
				Security: domain.SecurityMaster{
					TSCode:     "300308.SZ",
					Symbol:     "300308",
					Name:       "中际旭创",
					Market:     "SZ",
					AssetType:  "STOCK",
					ListStatus: "L",
					IsActive:   true,
				},
			}},
		},
	}, nil)

	trackable, resolutions, err := assembler.Assemble(context.Background(), []domain.PlanIntent{
		testIntent("新易盛"),
		testIntent("旭创"),
		testIntent("CPO板块"),
	})
	require.NoError(t, err)
	require.Len(t, trackable, 2)
	require.Equal(t, "300502.SZ", trackable[0].TSCode)
	require.Equal(t, "300502", trackable[0].Symbol)
	require.Equal(t, "新易盛", trackable[0].SecurityName)
	require.Equal(t, "300308.SZ", trackable[1].TSCode)
	require.Equal(t, "旭创", trackable[1].RawSymbol)

	require.Len(t, resolutions, 3)
	require.Equal(t, domain.InstrumentResolutionStatusResolved, resolutions[0].Status)
	require.Equal(t, domain.InstrumentResolutionStatusResolved, resolutions[1].Status)
	require.Equal(t, domain.InstrumentResolutionStatusUntrackable, resolutions[2].Status)
	require.Equal(t, domain.InstrumentTargetKindSector, resolutions[2].TargetKind)
}

func TestCandidateAssemblerRejectsNotFound(t *testing.T) {
	assembler := service.NewCandidateAssembler(fakeSecurityLookup{}, nil)

	trackable, resolutions, err := assembler.Assemble(context.Background(), []domain.PlanIntent{
		testIntent("不存在的股票简称"),
	})
	require.ErrorContains(t, err, `security not found for instrument "不存在的股票简称"`)
	require.Empty(t, trackable)
	require.Len(t, resolutions, 1)
	require.Equal(t, domain.InstrumentResolutionStatusNotFound, resolutions[0].Status)
}

func TestCandidateAssemblerKeepsResolvedTargetsWhenSomeTargetsAreNotFound(t *testing.T) {
	assembler := service.NewCandidateAssembler(fakeSecurityLookup{
		"ValidCo": {
			Query:      "ValidCo",
			Normalized: "validco",
			DirectMatches: []domain.SecurityMaster{{
				TSCode:    "600001.SH",
				Symbol:    "600001",
				Name:      "Valid Co",
				Market:    "SH",
				AssetType: "STOCK",
				IsActive:  true,
			}},
		},
	}, nil)

	trackable, resolutions, err := assembler.Assemble(context.Background(), []domain.PlanIntent{
		testIntent("ValidCo"),
		testIntent("MissingCo"),
	})

	require.NoError(t, err)
	require.Len(t, trackable, 1)
	require.Equal(t, "600001.SH", trackable[0].TSCode)
	require.Len(t, resolutions, 2)
	require.Equal(t, domain.InstrumentResolutionStatusResolved, resolutions[0].Status)
	require.Equal(t, domain.InstrumentResolutionStatusNotFound, resolutions[1].Status)
	require.Equal(t, "no active security matched", resolutions[1].Reason)
}

func TestCandidateAssemblerRejectsAmbiguousAlias(t *testing.T) {
	assembler := service.NewCandidateAssembler(fakeSecurityLookup{
		"重名标的": {
			Query:      "重名标的",
			Normalized: "重名标的",
			AliasMatches: []domain.SecurityAliasMatch{
				{
					Alias: domain.SecurityAlias{Alias: "重名标的"},
					Security: domain.SecurityMaster{
						TSCode:    "999981.SH",
						Symbol:    "999981",
						Name:      "重名标的一号",
						Market:    "SH",
						AssetType: "STOCK",
					},
				},
				{
					Alias: domain.SecurityAlias{Alias: "重名标的"},
					Security: domain.SecurityMaster{
						TSCode:    "999982.SH",
						Symbol:    "999982",
						Name:      "重名标的二号",
						Market:    "SH",
						AssetType: "STOCK",
					},
				},
			},
		},
	}, nil)

	trackable, resolutions, err := assembler.Assemble(context.Background(), []domain.PlanIntent{
		testIntent("重名标的"),
	})
	require.ErrorContains(t, err, `ambiguous instrument "重名标的": matched 2 active securities`)
	require.Empty(t, trackable)
	require.Len(t, resolutions, 1)
	require.Equal(t, domain.InstrumentResolutionStatusAmbiguous, resolutions[0].Status)
}

func TestCandidateAssemblerRejectsUntrackableOnly(t *testing.T) {
	assembler := service.NewCandidateAssembler(fakeSecurityLookup{}, nil)

	trackable, resolutions, err := assembler.Assemble(context.Background(), []domain.PlanIntent{
		testIntent("A股贵金属个股"),
	})
	require.ErrorContains(t, err, "no trackable securities resolved from 1 plan intents")
	require.Empty(t, trackable)
	require.Len(t, resolutions, 1)
	require.Equal(t, domain.InstrumentResolutionStatusUntrackable, resolutions[0].Status)
	require.Equal(t, domain.InstrumentTargetKindBroadPhrase, resolutions[0].TargetKind)
}

func testIntent(symbol string) domain.PlanIntent {
	return domain.PlanIntent{
		Analyst:        "tester",
		Institution:    "integration",
		Symbol:         symbol,
		Direction:      domain.TradeDirectionLong,
		ReferencePrice: 88.8,
		Thesis:         "source text supports the recommendation",
		Evidence:       []domain.EvidenceSpan{{ChunkIndex: 0, Text: "source evidence"}},
		Risks:          []string{"volatility"},
		Confidence:     0.81,
	}
}
