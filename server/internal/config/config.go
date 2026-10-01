// Package config loads server settings from environment variables.
package config

import (
	"errors"
	"fmt"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all runtime settings.
type Config struct {
	Port              string         // PORT, default 8080
	DatabaseURL       string         // DATABASE_URL, required
	AllowedOrigins    []string       // ALLOWED_ORIGINS, comma-separated host patterns, default "localhost"
	FlushInterval     time.Duration  // FLUSH_INTERVAL, default 500ms
	FlushMaxBatch     int            // FLUSH_MAX_BATCH, default 50
	ShutdownTimeout   time.Duration  // SHUTDOWN_TIMEOUT, default 10s
	HistoryRefreshMin int            // HISTORY_REFRESH_MIN, default 1000; see hub.Options
	RoomTTL           time.Duration  // ROOM_TTL, default 720h (30 days) without activity
	ExpiryInterval    time.Duration  // EXPIRY_INTERVAL, default 1h between expiry sweeps
	MaxPeers          int            // MAX_PEERS, default 25 per room
	MaxHistoryBytes   int            // MAX_HISTORY_BYTES, default 8388608 (8 MiB) per room, server backstop
	RoomCreateBurst   int            // ROOM_CREATE_BURST, default 10 new rooms at once per IP
	RoomCreatePerHour int            // ROOM_CREATE_PER_HOUR, default 60 (refill rate per IP)
	TrustedProxies    []netip.Prefix // TRUSTED_PROXIES, CIDRs allowed to set X-Forwarded-For; default private ranges
	StaticDir         string         // STATIC_DIR, built web app to serve; empty = API only
	LogLevel          string         // LOG_LEVEL: debug, info, warn, error; default info
}

// Load reads Config from the environment.
func Load() (Config, error) {
	c := Config{
		Port:        getenv("PORT", "8080"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
		LogLevel:    getenv("LOG_LEVEL", "info"),
		StaticDir:   os.Getenv("STATIC_DIR"),
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
	if c.RoomTTL, err = duration("ROOM_TTL", 30*24*time.Hour); err != nil {
		return c, err
	}
	if c.ExpiryInterval, err = duration("EXPIRY_INTERVAL", time.Hour); err != nil {
		return c, err
	}
	if c.FlushMaxBatch, err = positiveInt("FLUSH_MAX_BATCH", 50); err != nil {
		return c, err
	}
	if c.HistoryRefreshMin, err = positiveInt("HISTORY_REFRESH_MIN", 1000); err != nil {
		return c, err
	}
	if c.MaxPeers, err = positiveInt("MAX_PEERS", 25); err != nil {
		return c, err
	}
	if c.MaxHistoryBytes, err = positiveInt("MAX_HISTORY_BYTES", 8<<20); err != nil {
		return c, err
	}
	if c.RoomCreateBurst, err = positiveInt("ROOM_CREATE_BURST", 10); err != nil {
		return c, err
	}
	if c.RoomCreatePerHour, err = positiveInt("ROOM_CREATE_PER_HOUR", 60); err != nil {
		return c, err
	}
	if v := os.Getenv("TRUSTED_PROXIES"); v != "" {
		for _, cidr := range strings.Split(v, ",") {
			p, err := netip.ParsePrefix(strings.TrimSpace(cidr))
			if err != nil {
				return c, fmt.Errorf("TRUSTED_PROXIES: %w", err)
			}
			c.TrustedProxies = append(c.TrustedProxies, p)
		}
	}
	return c, nil
}

func positiveInt(key string, def int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("%s must be a positive integer", key)
	}
	return n, nil
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
