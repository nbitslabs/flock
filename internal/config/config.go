package config

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

// Config holds all flock configuration.
// Priority: CLI flag > ENV var > config file > default.
type Config struct {
	OpenCodeURL string `toml:"opencode_url"`
	Addr        string `toml:"addr"`
	DBPath      string `toml:"db"`
}

// Load reads configuration from the given TOML file path, then overlays
// environment variables. CLI flags are handled by the caller after Load returns.
func Load(configPath string) *Config {
	cfg := &Config{
		OpenCodeURL: "http://localhost:3000",
		Addr:        ":8080",
		DBPath:      "flock.db",
	}

	// Load TOML config file (optional — missing file is not an error)
	if data, err := os.ReadFile(configPath); err == nil {
		if err := toml.Unmarshal(data, cfg); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to parse %s: %v\n", configPath, err)
		}
	}

	// ENV overrides
	if v := os.Getenv("OPENCODE_URL"); v != "" {
		cfg.OpenCodeURL = v
	}
	if v := os.Getenv("FLOCK_ADDR"); v != "" {
		cfg.Addr = v
	}
	if v := os.Getenv("FLOCK_DB"); v != "" {
		cfg.DBPath = v
	}

	return cfg
}
