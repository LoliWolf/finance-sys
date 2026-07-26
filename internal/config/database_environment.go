package config

import (
	"net"
	"net/url"
	"strconv"
	"strings"
)

const (
	FinanceSysEnvironmentVariable = "FINANCE_SYS_ENV"
	ProductionEnvironmentValue    = "PROD"
)

type DatabaseProfile string

const (
	DatabaseProfileProduction DatabaseProfile = "production"
	DatabaseProfileTest       DatabaseProfile = "test"
)

// SelectDatabaseForEnvironment makes Database the effective runtime database.
// Production is deliberately fail-closed: only the exact value PROD selects it.
// Every other value, including an empty value, selects DatabaseTest.
func SelectDatabaseForEnvironment(cfg *Config, environment string) DatabaseProfile {
	if cfg == nil {
		return DatabaseProfileTest
	}
	if environment == ProductionEnvironmentValue {
		cfg.Database = cfg.DatabaseProduction
		cfg.SelectedDatabaseProfile = DatabaseProfileProduction
		return DatabaseProfileProduction
	}
	cfg.Database = cfg.DatabaseTest
	cfg.SelectedDatabaseProfile = DatabaseProfileTest
	return DatabaseProfileTest
}

// ApplyRuntimeEnvironment selects the database and HTTP port from the two
// explicit profiles in the single Nacos configuration. Production is selected
// only by the exact value PROD; every other value selects the test profile.
func ApplyRuntimeEnvironment(cfg *Config, environment string) DatabaseProfile {
	profile := SelectDatabaseForEnvironment(cfg, environment)
	if cfg == nil {
		return profile
	}

	effectivePort := cfg.Service.HTTP.PortTest
	if environment == ProductionEnvironmentValue {
		effectivePort = cfg.Service.HTTP.PortProduction
	}
	cfg.Service.HTTP.Port = effectivePort
	cfg.Agent.InternalAPIBaseURL = internalAPIBaseURLForPort(
		cfg.Agent.InternalAPIBaseURL,
		cfg.Service.HTTP.PortProduction,
		cfg.Service.HTTP.PortTest,
		effectivePort,
	)
	return profile
}

func internalAPIBaseURLForPort(rawURL string, productionPort int, testPort int, effectivePort int) string {
	if strings.TrimSpace(rawURL) == "" {
		return rawURL
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Hostname() == "" {
		return rawURL
	}
	configuredPort, err := strconv.Atoi(parsed.Port())
	if err != nil || (configuredPort != productionPort && configuredPort != testPort) {
		return rawURL
	}
	host := parsed.Hostname()
	parsed.Host = net.JoinHostPort(host, strconv.Itoa(effectivePort))
	return parsed.String()
}
