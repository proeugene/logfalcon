// Package config provides TOML configuration loading with struct defaults.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"reflect"
	"strconv"

	toml "github.com/pelletier/go-toml/v2"
)

const (
	systemConfigPath = "/etc/logfalcon/logfalcon.toml"
	localConfigPath  = "./config/logfalcon.toml"
)

// Config holds all runtime configuration for LogFalcon.
type Config struct {
	// Serial
	SerialBaud    int     `toml:"serial_baud"`
	SerialPort    string  `toml:"serial_port"`
	SerialTimeout float64 `toml:"serial_timeout"`

	// Storage
	StoragePath            string `toml:"storage_path"`
	MinFreeSpaceMB         int    `toml:"min_free_space_mb"`
	StoragePressureCleanup bool   `toml:"storage_pressure_cleanup"`

	// Sync behaviour
	EraseAfterSync       bool `toml:"erase_after_sync"`
	FlashChunkSize       int  `toml:"flash_chunk_size"`
	EraseTimeoutSec      int  `toml:"erase_timeout_sec"`
	FlashReadCompression bool `toml:"flash_read_compression"`

	// LED
	LEDBackend string `toml:"led_backend"`
	LEDGPIOPin int    `toml:"led_gpio_pin"`

	// Web server
	WebPort         int    `toml:"web_port"`
	HotspotSSID     string `toml:"hotspot_ssid"`
	HotspotPassword string `toml:"hotspot_password"`

	// Power management
	IdleShutdownMinutes int `toml:"idle_shutdown_minutes"`
}

// Default returns a Config populated with all default values.
func Default() *Config {
	return &Config{
		SerialBaud:    921600,
		SerialPort:    "",
		SerialTimeout: 5.0,

		StoragePath:            "/mnt/logfalcon-logs",
		MinFreeSpaceMB:         200,
		StoragePressureCleanup: true,

		EraseAfterSync:       true,
		FlashChunkSize:       4096,
		EraseTimeoutSec:      120,
		FlashReadCompression: false,

		LEDBackend: "sysfs",
		LEDGPIOPin: 17,

		WebPort:         80,
		HotspotSSID:     "LogFalcon",
		HotspotPassword: "fpvpilot",

		IdleShutdownMinutes: 0,
	}
}

// Load reads a TOML configuration file and returns a Config.
//
// Search order:
//  1. path argument (if non-empty)
//  2. /etc/logfalcon/logfalcon.toml
//  3. ./config/logfalcon.toml
//  4. All defaults
func Load(path string) (*Config, error) {
	candidates := make([]string, 0, 3)
	if path != "" {
		candidates = append(candidates, path)
	}
	candidates = append(candidates, systemConfigPath, localConfigPath)

	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err != nil {
			continue
		}
		slog.Debug("loading config", "path", candidate)
		cfg, err := loadFile(candidate)
		if err != nil {
			slog.Warn("failed to load config", "path", candidate, "error", err)
			continue
		}
		applyEnvOverrides(cfg)
		return cfg, nil
	}

	slog.Debug("using default config (no config file found)")
	cfg := Default()
	applyEnvOverrides(cfg)
	return cfg, nil
}

// loadFile reads a single TOML file and applies its values over defaults.
func loadFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	// Decode into a map first to detect unknown keys.
	var raw map[string]any
	if err := toml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}
	warnUnknownKeys(raw)

	// Decode into Config (start from defaults).
	cfg := Default()
	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("applying config values: %w", err)
	}
	return cfg, nil
}

// warnUnknownKeys logs a warning for any TOML key that does not match a Config field tag.
func warnUnknownKeys(raw map[string]any) {
	known := knownKeys()
	for key := range raw {
		if !known[key] {
			slog.Warn("unknown config key", "key", key)
		}
	}
}

// knownKeys returns the set of valid TOML key names derived from Config struct tags.
func knownKeys() map[string]bool {
	t := reflect.TypeOf(Config{})
	keys := make(map[string]bool, t.NumField())
	for i := range t.NumField() {
		tag := t.Field(i).Tag.Get("toml")
		if tag != "" {
			keys[tag] = true
		}
	}
	return keys
}

// applyEnvOverrides reads environment variables and overrides config fields
// when the variable is set and valid. Invalid values log a warning and are
// silently ignored, preserving the current value.
func applyEnvOverrides(cfg *Config) {
	if v, ok := os.LookupEnv("LOGFALCON_STORAGE_PATH"); ok {
		cfg.StoragePath = v
	}
	if v, ok := os.LookupEnv("LOGFALCON_WEB_PORT"); ok {
		n, err := strconv.Atoi(v)
		if err != nil {
			slog.Warn("invalid LOGFALCON_WEB_PORT, ignoring", "value", v, "error", err)
		} else if n < 1 || n > 65535 {
			slog.Warn("invalid LOGFALCON_WEB_PORT, must be 1-65535, ignoring", "value", n)
		} else {
			cfg.WebPort = n
		}
	}
	if v, ok := os.LookupEnv("LOGFALCON_LED_BACKEND"); ok {
		cfg.LEDBackend = v
	}
	if v, ok := os.LookupEnv("LOGFALCON_MIN_FREE_MB"); ok {
		n, err := strconv.Atoi(v)
		if err != nil {
			slog.Warn("invalid LOGFALCON_MIN_FREE_MB, ignoring", "value", v, "error", err)
		} else if n < 0 {
			slog.Warn("invalid LOGFALCON_MIN_FREE_MB, must be >= 0, ignoring", "value", n)
		} else {
			cfg.MinFreeSpaceMB = n
		}
	}
	if v, ok := os.LookupEnv("LOGFALCON_ERASE_AFTER_SYNC"); ok {
		b, err := strconv.ParseBool(v)
		if err != nil {
			slog.Warn("invalid LOGFALCON_ERASE_AFTER_SYNC, ignoring", "value", v, "error", err)
		} else {
			cfg.EraseAfterSync = b
		}
	}
}
