package main

import (
	"context"
	"fmt"
	"net/http"
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

	cfg := app.Runtime.Config()
	server := &http.Server{
		Addr:              fmt.Sprintf("%s:%d", cfg.Service.HTTP.Host, cfg.Service.HTTP.Port),
		Handler:           app.HTTPServer.Router(),
		ReadTimeout:       time.Duration(cfg.Service.HTTP.ReadTimeoutMS) * time.Millisecond,
		WriteTimeout:      time.Duration(cfg.Service.HTTP.WriteTimeoutMS) * time.Millisecond,
		IdleTimeout:       time.Duration(cfg.Service.HTTP.IdleTimeoutMS) * time.Millisecond,
		ReadHeaderTimeout: 5 * time.Second,
		MaxHeaderBytes:    cfg.Service.HTTP.MaxHeaderBytes,
	}

	if app.Watcher != nil {
		go app.Watcher.Run(ctx)
	}

	go func() {
		app.Logger.Info("api server started", "addr", server.Addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			panic(err)
		}
	}()

	waitForShutdown()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.Service.HTTP.ShutdownTimeoutSeconds)*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
}

func waitForShutdown() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	<-signals
}
