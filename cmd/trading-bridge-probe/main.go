package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"finance-sys/internal/bootstrap"
	"finance-sys/internal/config"
	"finance-sys/internal/telemetry"
	"finance-sys/internal/tradingbridgeclient"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	logger := telemetry.NewLogger("ERROR")
	snapshot, loader, err := bootstrap.LoadInitialSnapshot(ctx, logger)
	if loader != nil {
		defer loader.Close()
	}
	if err != nil {
		fail(err)
	}
	client := tradingbridgeclient.New(config.NewRuntime(snapshot))
	health, err := client.Health(ctx)
	if err != nil {
		fail(err)
	}
	result := map[string]any{
		"status": health.Status, "api": health.API, "sqlite": health.SQLite,
		"runner": health.Runner, "terminal": health.Terminal, "account": health.Account,
		"auth_state": health.AuthState, "kill_switch": health.KillSwitch,
		"account_id": health.AccountID, "strategy_id": health.StrategyID,
		"config_version": health.ConfigVersion, "runner_heartbeat_at": health.RunnerHeartbeatAt,
		"last_auth_success_at": health.LastAuthSuccessAt, "token_fingerprint": health.TokenFingerprint,
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		fail(err)
	}
}

func fail(err error) {
	_, _ = fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
