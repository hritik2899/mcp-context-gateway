package main

import (
	"context"
	"errors"
	"flag"
	"github.com/hritik2899/mcp-context-gateway/internal/app"
	"github.com/hritik2899/mcp-context-gateway/internal/config"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	if err := run(); err != nil {
		slog.Error("gateway stopped", "error", err)
		os.Exit(1)
	}
}
func run() error {
	filename := flag.String("config", "", "path to JSON configuration")
	check := flag.Bool("check", false, "validate configuration and exit")
	flag.Parse()
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	slog.SetDefault(logger)
	c, err := config.Load(*filename)
	if err != nil {
		return err
	}
	if *check {
		logger.Info("configuration valid")
		return nil
	}
	application, err := app.New(context.Background(), c, logger)
	if err != nil {
		return err
	}
	server := &http.Server{Addr: c.Listen, Handler: application.Handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: c.RequestTimeout.Value() + 2*time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	errorsCh := make(chan error, 1)
	go func() { logger.Info("gateway listening", "address", c.Listen); errorsCh <- server.ListenAndServe() }()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(signals)
	var serveErr error
running:
	for {
		select {
		case serveErr = <-errorsCh:
			break running
		case sig := <-signals:
			if sig == syscall.SIGHUP {
				ctx, cancel := context.WithTimeout(context.Background(), c.DiscoveryTimeout.Value())
				err := application.Catalog.Refresh(ctx)
				cancel()
				if err != nil {
					logger.Warn("discovery refresh failed")
				}
				continue
			}
			break running
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), c.ShutdownTimeout.Value())
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		server.Close()
		logger.Warn("HTTP shutdown grace expired")
	}
	if err := application.Close(ctx); err != nil {
		logger.Warn("downstream session cleanup incomplete")
	}
	if errors.Is(serveErr, http.ErrServerClosed) {
		return nil
	}
	return serveErr
}
