package config

type DatabaseDriver string

const (
	DatabaseDriverMySQL DatabaseDriver = "mysql"
)

type LogLevel string

const (
	LogLevelDebug LogLevel = "DEBUG"
	LogLevelInfo  LogLevel = "INFO"
	LogLevelWarn  LogLevel = "WARN"
	LogLevelError LogLevel = "ERROR"
)

type LLMProvider string

const (
	LLMProviderOpenAICompatible LLMProvider = "openai_compatible"
)

type RuleStrategy string

const (
	RuleStrategyTextReferencePrice RuleStrategy = "TEXT_REFERENCE_PRICE"
)
