package config

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
