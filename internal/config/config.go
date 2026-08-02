// Package config loads and validates application configuration from environment variables.
package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
)

// Duration wraps time.Duration to also accept day-based strings like "7d".
type Duration struct {
	time.Duration
}

// UnmarshalText parses a duration string, supporting a trailing "d" for days.
func (d *Duration) UnmarshalText(text []byte) error {
	s := string(text)
	if strings.HasSuffix(s, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil || days < 0 {
			return fmt.Errorf("invalid day duration %q", s)
		}
		d.Duration = time.Duration(days) * 24 * time.Hour
		return nil
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	d.Duration = parsed
	return nil
}

// Config holds application settings, populated from environment variables.
type Config struct {
	DatabaseURL     string   `env:"DATABASE_URL,required"`
	JWTSecret       string   `env:"JWT_SECRET,required"`
	LogLevel        string   `env:"LOG_LEVEL" envDefault:"info"`
	Port            int      `env:"PORT" envDefault:"8080"`
	AccessTokenTTL  Duration `env:"ACCESS_TOKEN_TTL" envDefault:"15m"`
	RefreshTokenTTL Duration `env:"REFRESH_TOKEN_TTL" envDefault:"7d"`
}

// Load reads environment variables into a Config, failing on missing required values.
func Load() (*Config, error) {
	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}
