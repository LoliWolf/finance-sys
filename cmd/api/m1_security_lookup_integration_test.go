package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"

	"finance-sys/internal/config"
	"finance-sys/internal/dal"
	"finance-sys/internal/domain"
	"finance-sys/internal/domain/db_model"
	"finance-sys/internal/service"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestHTTPM1SecurityLookupWithNacosBootstrap(t *testing.T) {
	if os.Getenv("FINANCE_SYS_M1_NACOS_INTEGRATION") != "1" {
		t.Skip("set FINANCE_SYS_M1_NACOS_INTEGRATION=1 to run; this test writes to the Nacos-configured MySQL database")
	}
	if os.Getenv("FINANCE_SYS_M1_NACOS_DML_ACK") != "write-real-db" {
		t.Skip("set FINANCE_SYS_M1_NACOS_DML_ACK=write-real-db after manually acknowledging real database writes")
	}

	loadBootstrapEnvFile(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	app := buildIntegrationApp(t, ctx)
	defer app.Close()

	cfg := cloneConfig(t, app.Runtime.Config())
	cfg.Security.Auth.Enabled = false
	app.Runtime.Update(&config.Snapshot{
		Config:   cfg,
		Source:   app.Runtime.Current().Source,
		SHA256:   app.Runtime.Current().SHA256,
		LoadedAt: app.Runtime.Current().LoadedAt,
		Raw:      app.Runtime.Current().Raw,
	})

	seedM1SecurityLookupFixtures(t, ctx, app.DB)

	baseURL, shutdown := startMainHTTPServerForTest(t, app)
	defer shutdown()

	tsCodeLookup := securityLookup(t, baseURL, "300502.SZ")
	requireSecurityDirectMatch(t, tsCodeLookup, "300502.SZ", "新易盛")

	nameLookup := securityLookup(t, baseURL, "新易盛")
	requireSecurityDirectMatch(t, nameLookup, "300502.SZ", "新易盛")

	aliasLookup := securityLookup(t, baseURL, "旭创")
	requireSecurityAliasMatch(t, aliasLookup, "300308.SZ", "中际旭创", "旭创")

	requireSecurityLookupStatus(t, baseURL, "", http.StatusBadRequest)
	requireSecurityLookupStatus(t, baseURL, "CPO板块", http.StatusNotFound)
	requireSecurityLookupStatus(t, baseURL, "A股贵金属个股", http.StatusNotFound)
	requireSecurityLookupStatus(t, baseURL, "M1退市样例", http.StatusNotFound)
}

func seedM1SecurityLookupFixtures(t *testing.T, ctx context.Context, db *gorm.DB) {
	t.Helper()

	records := []db_model.SecurityMaster{
		{
			TSCode:     "300502.SZ",
			Symbol:     "300502",
			Name:       "新易盛",
			FullName:   "成都新易盛通信技术股份有限公司",
			Exchange:   "SZSE",
			Market:     "SZ",
			AssetType:  "STOCK",
			ListStatus: "L",
			Industry:   "通信设备",
			IsActive:   true,
			Source:     "TEST",
			RawJSON:    []byte(`{"fixture":"m1_security_lookup"}`),
		},
		{
			TSCode:     "300308.SZ",
			Symbol:     "300308",
			Name:       "中际旭创",
			FullName:   "中际旭创股份有限公司",
			Exchange:   "SZSE",
			Market:     "SZ",
			AssetType:  "STOCK",
			ListStatus: "L",
			Industry:   "通信设备",
			IsActive:   true,
			Source:     "TEST",
			RawJSON:    []byte(`{"fixture":"m1_security_lookup"}`),
		},
		{
			TSCode:     "899999.BJ",
			Symbol:     "899999",
			Name:       "M1退市样例",
			FullName:   "M1退市样例股份有限公司",
			Exchange:   "BSE",
			Market:     "BJ",
			AssetType:  "STOCK",
			ListStatus: "D",
			Industry:   "测试",
			IsActive:   false,
			Source:     "TEST",
			RawJSON:    []byte(`{"fixture":"m1_security_lookup","active":false}`),
		},
	}
	for i := range records {
		require.NoError(t, dal.SecurityMasters.UpsertByTSCode(ctx, db, &records[i]))
	}

	aliasSecurity, err := dal.SecurityMasters.QueryByTSCode(ctx, db, "300308.SZ")
	require.NoError(t, err)
	require.NoError(t, dal.SecurityAliases.UpsertByAliasAndSecurityID(ctx, db, &db_model.SecurityAlias{
		SecurityMasterID: aliasSecurity.ID,
		AliasName:        "旭创",
		NormalizedAlias:  service.NormalizeSecurityAlias("旭创"),
		AliasType:        "COMMON_NAME",
		Source:           "TEST",
		Confidence:       1,
		IsActive:         true,
	}))
}

func securityLookup(t *testing.T, baseURL, query string) domain.SecurityLookupResult {
	t.Helper()
	status, body := securityLookupBody(t, baseURL, query)
	require.Equal(t, http.StatusOK, status, string(body))
	var payload domain.SecurityLookupResult
	require.NoError(t, json.Unmarshal(body, &payload))
	return payload
}

func securityLookupBody(t *testing.T, baseURL, query string) (int, []byte) {
	t.Helper()
	resp, err := http.Get(fmt.Sprintf("%s/api/v1/admin/security/lookup?query=%s", baseURL, url.QueryEscape(query)))
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, body
}

func requireSecurityLookupStatus(t *testing.T, baseURL, query string, expected int) {
	t.Helper()
	status, body := securityLookupBody(t, baseURL, query)
	require.Equal(t, expected, status, string(body))
}

func requireSecurityDirectMatch(t *testing.T, result domain.SecurityLookupResult, tsCode, name string) {
	t.Helper()
	for _, item := range result.DirectMatches {
		if item.TSCode == tsCode && item.Name == name && item.IsActive {
			return
		}
	}
	t.Fatalf("direct match %s %s not found in %+v", tsCode, name, result.DirectMatches)
}

func requireSecurityAliasMatch(t *testing.T, result domain.SecurityLookupResult, tsCode, name, alias string) {
	t.Helper()
	for _, item := range result.AliasMatches {
		if item.Security.TSCode == tsCode && item.Security.Name == name && item.Alias.Alias == alias && item.Security.IsActive {
			return
		}
	}
	t.Fatalf("alias match %s %s %s not found in %+v", alias, tsCode, name, result.AliasMatches)
}
