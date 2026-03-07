package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"finance-sys/internal/bootstrap"
)

func main() {
	ctx := context.Background()
	app, err := bootstrap.Build(ctx)
	if err != nil {
		panic(err)
	}

	if app.Watcher != nil {
		go app.Watcher.Run(ctx)
	}
	app.Scheduler.Start()
	app.Logger.Info("worker started")

	waitForShutdown()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Duration(app.Runtime.Config().Service.Worker.GracefulShutdownSeconds)*time.Second)
	defer cancel()
	_ = app.Scheduler.Stop(shutdownCtx)
}

func waitForShutdown() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	<-signals
}
