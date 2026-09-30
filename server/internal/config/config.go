// Package config loads server settings from environment variables.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all runtime settings.
type Config struct {
	Port            string        // PORT, default 8080
	DatabaseURL     string        // DATABASE_URL, required
	AllowedOrigins  []string      // ALLOWED_ORIGINS, comma-separated host patterns, default "localhost"
	FlushInterval   time.Duration // FLUSH_INTERVAL, default 500ms
	FlushMaxBatch   int           // FLUSH_MAX_BATCH, default 50
	ShutdownTimeout time.Duration // SHUTDOWN_TIMEOUT, default 10s
	LogLevel        string        // LOG_LEVEL: debug, info, warn, error; default info
}

// Load reads Config from the environment.
func Load() (Config, error) {
	c := Config{
		Port:        getenv("PORT", "8080"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
		LogLevel:    getenv("LOG_LEVEL", "info"),
	}
	if c.DatabaseURL == "" {
		return c, errors.New("DATABASE_URL is required")
	}
	for _, o := range strings.Split(getenv("ALLOWED_ORIGINS", "localhost"), ",") {
		if o = strings.TrimSpace(o); o != "" {
			c.AllowedOrigins = append(c.AllowedOrigins, o)
		}
	}

	var err error
	if c.FlushInterval, err = duration("FLUSH_INTERVAL", 500*time.Millisecond); err != nil {
		return c, err
	}
	if c.ShutdownTimeout, err = duration("SHUTDOWN_TIMEOUT", 10*time.Second); err != nil {
		return c, err
	}
	if c.FlushMaxBatch, err = strconv.Atoi(getenv("FLUSH_MAX_BATCH", "50")); err != nil || c.FlushMaxBatch < 1 {
		return c, fmt.Errorf("FLUSH_MAX_BATCH must be a positive integer")
	}
	return c, nil
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func duration(key string, def time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration like 500ms", key)
	}
	return d, nil
}
