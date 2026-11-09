package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"payment_ledger/internal/config"
	"payment_ledger/internal/httpapi"
	"payment_ledger/internal/postgres"
	"syscall"
	"time"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(ctx, logger); err != nil {
		logger.Error("Service is stopped with error", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *slog.Logger) error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	pool, err := postgres.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	if err := postgres.Migrate(ctx, pool); err != nil {
		return err
	}

	server := &http.Server{
		Handler:           httpapi.Routes(pool),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	listener, err := net.Listen("tcp", cfg.HTTPAddr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("HTTP server listening", "addr", listener.Addr().String())
		serverErr <- server.Serve(listener)
	}()

	select {
	case err := <-serverErr:
		return fmt.Errorf("server error: %w", err)
	case <-ctx.Done():
		logger.Info("Shutting down...")
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return errors.Join(fmt.Errorf("shutdown: %w", err), server.Close())
	}
	logger.Info("Shutdown complete correctly")
	return nil
}
