// Package config provides TOML configuration loading with struct defaults.
package config

import (
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
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
	SetupCompleted  bool   `toml:"setup_completed"`
	DeviceIdentity  string `toml:"device_identity"`
	HotspotSSID     string `toml:"hotspot_ssid"`
	HotspotPassword string `toml:"hotspot_password"`

	// Power management
	IdleShutdownMinutes int    `toml:"idle_shutdown_minutes"`
	StatusPath          string `toml:"status_path"`
	DebugLogging        bool   `toml:"debug_logging"`
}

// Default returns a Config populated with all default values.
func Default() *Config {
	return &Config{
		SerialBaud:    921600,
		SerialPort:    "",
		SerialTimeout: 5.0,

		StoragePath:            "/mnt/logfalcon-logs",
		MinFreeSpaceMB:         200,
		StoragePressureCleanup: false,

		EraseAfterSync:       false,
		FlashChunkSize:       4096,
		EraseTimeoutSec:      120,
		FlashReadCompression: false,

		LEDBackend: "sysfs",
		LEDGPIOPin: 17,

		WebPort:         80,
		HotspotSSID:     "LogFalcon",
		HotspotPassword: "fpvpilot",

		IdleShutdownMinutes: 0,
		StatusPath:          "/run/logfalcon/status.json",
		DebugLogging:        true,
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
		cfg, err := loadFile(path)
		if err != nil {
			return nil, fmt.Errorf("invalid explicit configuration %s", path)
		}
		return finish(cfg)
	}
	candidates = append(candidates, systemConfigPath, localConfigPath)

	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("cannot access configuration %s", candidate)
		}
		slog.Debug("loading config", "path", candidate)
		cfg, err := loadFile(candidate)
		if err != nil {
			return nil, fmt.Errorf("invalid configuration %s", candidate)
		}
		return finish(cfg)
	}

	slog.Debug("using default config (no config file found)")
	cfg := Default()
	return finish(cfg)
}

func finish(cfg *Config) (*Config, error) {
	if err := applyEnvOverrides(cfg); err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Validate rejects settings that could hang transfers or enable unsafe fallbacks.
func (c *Config) Validate() error {
	if c.FlashReadCompression {
		return fmt.Errorf("flash compression was removed; set flash_read_compression=false")
	}
	if c.SerialBaud < 1 || math.IsNaN(c.SerialTimeout) || math.IsInf(c.SerialTimeout, 0) || c.SerialTimeout <= 0 || c.SerialTimeout > 60 {
		return fmt.Errorf("serial baud/timeout out of range")
	}
	if c.FlashChunkSize < 1 || c.FlashChunkSize > 16384 {
		return fmt.Errorf("flash_chunk_size must be 1-16384")
	}
	if c.MinFreeSpaceMB < 0 || c.MinFreeSpaceMB > 1048576 || c.EraseTimeoutSec < 1 || c.EraseTimeoutSec > 600 {
		return fmt.Errorf("storage reserve/erase timeout out of range")
	}
	if !filepath.IsAbs(c.StoragePath) || !filepath.IsAbs(c.StatusPath) {
		return fmt.Errorf("storage_path and status_path must be absolute")
	}
	if c.WebPort < 1 || c.WebPort > 65535 || c.IdleShutdownMinutes < 0 {
		return fmt.Errorf("web port/shutdown timeout out of range")
	}
	if len(c.HotspotSSID) < 1 || len(c.HotspotSSID) > 32 || len(c.HotspotPassword) < 8 || len(c.HotspotPassword) > 63 {
		return fmt.Errorf("hotspot credentials have invalid lengths")
	}
	for _, value := range []string{c.HotspotSSID, c.HotspotPassword} {
		for _, ch := range value {
			if ch < 32 || ch > 126 {
				return fmt.Errorf("hotspot credentials must be printable ASCII")
			}
		}
	}
	if c.LEDBackend != "sysfs" && c.LEDBackend != "none" {
		return fmt.Errorf("led_backend must be sysfs or none; gpio is not implemented")
	}
	return nil
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
	for key := range raw {
		if !knownKeys()[key] {
			return nil, fmt.Errorf("unknown configuration key %s", key)
		}
	}

	// Decode into Config (start from defaults).
	cfg := Default()
	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("applying config values: %w", err)
	}
	return cfg, nil
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
// when set. Invalid values stop startup rather than silently enabling defaults.
func applyEnvOverrides(cfg *Config) error {
	if v, ok := os.LookupEnv("LOGFALCON_STORAGE_PATH"); ok {
		cfg.StoragePath = v
	}
	if v, ok := os.LookupEnv("LOGFALCON_WEB_PORT"); ok {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("invalid LOGFALCON_WEB_PORT")
		} else if n < 1 || n > 65535 {
			return fmt.Errorf("LOGFALCON_WEB_PORT must be 1-65535")
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
			return fmt.Errorf("invalid LOGFALCON_MIN_FREE_MB")
		} else if n < 0 {
			return fmt.Errorf("LOGFALCON_MIN_FREE_MB must be nonnegative")
		} else {
			cfg.MinFreeSpaceMB = n
		}
	}
	if v, ok := os.LookupEnv("LOGFALCON_ERASE_AFTER_SYNC"); ok {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("invalid LOGFALCON_ERASE_AFTER_SYNC")
		} else {
			cfg.EraseAfterSync = b
		}
	}
	if v, ok := os.LookupEnv("LOGFALCON_DEBUG"); ok {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("invalid LOGFALCON_DEBUG")
		}
		cfg.DebugLogging = b
	}
	return nil
}
