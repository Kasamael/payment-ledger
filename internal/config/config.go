package config

import (
	"cmp"
	"errors"
	"fmt"
	"time"
)

type Config struct {
	HTTPAddr        string
	DatabaseURL     string
	ShutdownTimeout time.Duration
}

func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		HTTPAddr:    cmp.Or(getenv("HTTP_ADDR"), "127.0.0.1:8095"),
		DatabaseURL: getenv("DATABASE_URL"),
	}

	var errs []error
	if cfg.DatabaseURL == "" {
		errs = append(errs, errors.New("database url is required"))
	}

	timeout, err := time.ParseDuration(cmp.Or(getenv("SHUTDOWN_TIMEOUT"), "10s"))
	if err != nil {
		errs = append(errs, fmt.Errorf("invalid timeout: %w", err))
	}
	cfg.ShutdownTimeout = timeout

	return cfg, errors.Join(errs...)
}
