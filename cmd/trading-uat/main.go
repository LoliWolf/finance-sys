package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"finance-sys/internal/bootstrap"
	tradingservice "finance-sys/internal/trading/service"
)

func main() {
	uatKey := flag.String("uat-key", "", "unique UAT key")
	symbol := flag.String("symbol", "600000", "six-digit stock symbol")
	market := flag.String("market", "SH", "SH or SZ")
	side := flag.String("side", "BUY", "BUY or SELL")
	confirm := flag.String("confirm", "", "must equal SIMULATION_ONE_LOT or SIMULATION_ONE_LOT_SELL")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	app, err := bootstrap.Build(ctx)
	if err != nil {
		fail(err)
	}
	defer app.Close()
	view, err := app.TradingService.ExecuteOneLotUAT(ctx, tradingservice.OneLotUATRequest{
		UATKey: *uatKey, Symbol: *symbol, Market: *market, Action: *side, Confirm: *confirm,
	})
	if err != nil {
		fail(err)
	}
	result := map[string]any{"run_id": view.Run.ID, "run_status": view.Run.Status}
	if len(view.Intents) == 1 {
		result["intent_id"] = view.Intents[0].ID
		result["intent_status"] = view.Intents[0].Status
		result["rejection_code"] = view.Intents[0].RejectionCode
	}
	if len(view.Orders) == 1 {
		result["order_id"] = view.Orders[0].ID
		result["client_order_id"] = view.Orders[0].ClientOrderID
		result["order_status"] = view.Orders[0].Status
		result["symbol"] = view.Orders[0].Symbol
		result["side"] = view.Orders[0].Side
		result["volume"] = view.Orders[0].Volume
		result["limit_price"] = view.Orders[0].LimitPrice
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		fail(err)
	}
}

func fail(err error) {
	_, _ = fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
