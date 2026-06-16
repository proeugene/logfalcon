package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefault(t *testing.T) {
	cfg := Default()

	// Serial
	assertEqual(t, "SerialBaud", cfg.SerialBaud, 921600)
	assertEqual(t, "SerialPort", cfg.SerialPort, "")
	assertEqualFloat(t, "SerialTimeout", cfg.SerialTimeout, 5.0)

	// Storage
	assertEqual(t, "StoragePath", cfg.StoragePath, "/mnt/logfalcon-logs")
	assertEqual(t, "MinFreeSpaceMB", cfg.MinFreeSpaceMB, 200)
	assertEqualBool(t, "StoragePressureCleanup", cfg.StoragePressureCleanup, true)

	// Sync behaviour
	assertEqualBool(t, "EraseAfterSync", cfg.EraseAfterSync, true)
	assertEqual(t, "FlashChunkSize", cfg.FlashChunkSize, 4096)
	assertEqual(t, "EraseTimeoutSec", cfg.EraseTimeoutSec, 120)
	assertEqualBool(t, "FlashReadCompression", cfg.FlashReadCompression, false)

	// LED
	assertEqual(t, "LEDBackend", cfg.LEDBackend, "sysfs")
	assertEqual(t, "LEDGPIOPin", cfg.LEDGPIOPin, 17)

	// Web server
	assertEqual(t, "WebPort", cfg.WebPort, 80)
	assertEqual(t, "HotspotSSID", cfg.HotspotSSID, "LogFalcon")
	assertEqual(t, "HotspotPassword", cfg.HotspotPassword, "fpvpilot")

	// Power management
	assertEqual(t, "IdleShutdownMinutes", cfg.IdleShutdownMinutes, 0)
}

func TestLoadFromFile(t *testing.T) {
	content := `
serial_baud = 921600
serial_port = "/dev/ttyUSB0"
serial_timeout = 10.0
storage_path = "/tmp/logs"
min_free_space_mb = 500
storage_pressure_cleanup = false
erase_after_sync = false
flash_chunk_size = 8192
erase_timeout_sec = 60
flash_read_compression = true
led_backend = "gpio"
led_gpio_pin = 22
web_port = 8080
hotspot_ssid = "MyDrone"
hotspot_password = "secret123"
idle_shutdown_minutes = 15
`
	path := writeTempTOML(t, content)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	assertEqual(t, "SerialBaud", cfg.SerialBaud, 921600)
	assertEqual(t, "SerialPort", cfg.SerialPort, "/dev/ttyUSB0")
	assertEqualFloat(t, "SerialTimeout", cfg.SerialTimeout, 10.0)
	assertEqual(t, "StoragePath", cfg.StoragePath, "/tmp/logs")
	assertEqual(t, "MinFreeSpaceMB", cfg.MinFreeSpaceMB, 500)
	assertEqualBool(t, "StoragePressureCleanup", cfg.StoragePressureCleanup, false)
	assertEqualBool(t, "EraseAfterSync", cfg.EraseAfterSync, false)
	assertEqual(t, "FlashChunkSize", cfg.FlashChunkSize, 8192)
	assertEqual(t, "EraseTimeoutSec", cfg.EraseTimeoutSec, 60)
	assertEqualBool(t, "FlashReadCompression", cfg.FlashReadCompression, true)
	assertEqual(t, "LEDBackend", cfg.LEDBackend, "gpio")
	assertEqual(t, "LEDGPIOPin", cfg.LEDGPIOPin, 22)
	assertEqual(t, "WebPort", cfg.WebPort, 8080)
	assertEqual(t, "HotspotSSID", cfg.HotspotSSID, "MyDrone")
	assertEqual(t, "HotspotPassword", cfg.HotspotPassword, "secret123")
	assertEqual(t, "IdleShutdownMinutes", cfg.IdleShutdownMinutes, 15)
}

func TestLoadMissing(t *testing.T) {
	cfg, err := Load("/nonexistent/path/config.toml")
	if err != nil {
		t.Fatalf("Load returned error for missing file: %v", err)
	}
	// Should return defaults when no file is found.
	expected := Default()
	if *cfg != *expected {
		t.Errorf("expected default config, got %+v", cfg)
	}
}

func TestPartialOverride(t *testing.T) {
	content := `
web_port = 9090
hotspot_ssid = "CustomSSID"
`
	path := writeTempTOML(t, content)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	// Overridden fields.
	assertEqual(t, "WebPort", cfg.WebPort, 9090)
	assertEqual(t, "HotspotSSID", cfg.HotspotSSID, "CustomSSID")

	// Non-overridden fields remain at defaults.
	assertEqual(t, "SerialBaud", cfg.SerialBaud, 921600)
	assertEqualFloat(t, "SerialTimeout", cfg.SerialTimeout, 5.0)
	assertEqual(t, "StoragePath", cfg.StoragePath, "/mnt/logfalcon-logs")
	assertEqual(t, "MinFreeSpaceMB", cfg.MinFreeSpaceMB, 200)
	assertEqualBool(t, "StoragePressureCleanup", cfg.StoragePressureCleanup, true)
	assertEqualBool(t, "EraseAfterSync", cfg.EraseAfterSync, true)
	assertEqual(t, "FlashChunkSize", cfg.FlashChunkSize, 4096)
	assertEqual(t, "EraseTimeoutSec", cfg.EraseTimeoutSec, 120)
	assertEqualBool(t, "FlashReadCompression", cfg.FlashReadCompression, false)
	assertEqual(t, "LEDBackend", cfg.LEDBackend, "sysfs")
	assertEqual(t, "LEDGPIOPin", cfg.LEDGPIOPin, 17)
	assertEqual(t, "HotspotPassword", cfg.HotspotPassword, "fpvpilot")
	assertEqual(t, "IdleShutdownMinutes", cfg.IdleShutdownMinutes, 0)
}

func TestEnvOverrideFile(t *testing.T) {
	content := `storage_path = "/tmp/logs"
web_port = 8080
led_backend = "gpio"
min_free_space_mb = 500
erase_after_sync = false`
	path := writeTempTOML(t, content)

	t.Setenv("LOGFALCON_STORAGE_PATH", "/env/storage")
	t.Setenv("LOGFALCON_WEB_PORT", "9090")
	t.Setenv("LOGFALCON_LED_BACKEND", "ws2812")
	t.Setenv("LOGFALCON_MIN_FREE_MB", "999")
	t.Setenv("LOGFALCON_ERASE_AFTER_SYNC", "true")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	assertEqual(t, "StoragePath (env)", cfg.StoragePath, "/env/storage")
	assertEqual(t, "WebPort (env)", cfg.WebPort, 9090)
	assertEqual(t, "LEDBackend (env)", cfg.LEDBackend, "ws2812")
	assertEqual(t, "MinFreeSpaceMB (env)", cfg.MinFreeSpaceMB, 999)
	assertEqualBool(t, "EraseAfterSync (env)", cfg.EraseAfterSync, true)
}

func TestEnvOverrideDefault(t *testing.T) {
	t.Setenv("LOGFALCON_STORAGE_PATH", "/env/default")
	t.Setenv("LOGFALCON_WEB_PORT", "3000")
	t.Setenv("LOGFALCON_LED_BACKEND", "none")
	t.Setenv("LOGFALCON_MIN_FREE_MB", "123")
	t.Setenv("LOGFALCON_ERASE_AFTER_SYNC", "false")

	cfg, err := Load("/nonexistent/path/config.toml")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	assertEqual(t, "StoragePath", cfg.StoragePath, "/env/default")
	assertEqual(t, "WebPort", cfg.WebPort, 3000)
	assertEqual(t, "LEDBackend", cfg.LEDBackend, "none")
	assertEqual(t, "MinFreeSpaceMB", cfg.MinFreeSpaceMB, 123)
	assertEqualBool(t, "EraseAfterSync", cfg.EraseAfterSync, false)
}

func TestEnvInvalidValues(t *testing.T) {
	t.Setenv("LOGFALCON_WEB_PORT", "not-a-number")
	t.Setenv("LOGFALCON_MIN_FREE_MB", "also-bad")
	t.Setenv("LOGFALCON_ERASE_AFTER_SYNC", "nope")

	cfg, err := Load("/nonexistent/path/config.toml")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	expected := Default()
	assertEqual(t, "WebPort (invalid env)", cfg.WebPort, expected.WebPort)
	assertEqual(t, "MinFreeSpaceMB (invalid env)", cfg.MinFreeSpaceMB, expected.MinFreeSpaceMB)
	assertEqualBool(t, "EraseAfterSync (invalid env)", cfg.EraseAfterSync, expected.EraseAfterSync)
}

func TestEnvUnsetDoesNotOverride(t *testing.T) {
	content := `storage_path = "/tmp/logs"
web_port = 8080
led_backend = "gpio"
min_free_space_mb = 500
erase_after_sync = false`
	path := writeTempTOML(t, content)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	assertEqual(t, "StoragePath", cfg.StoragePath, "/tmp/logs")
	assertEqual(t, "WebPort", cfg.WebPort, 8080)
	assertEqual(t, "LEDBackend", cfg.LEDBackend, "gpio")
	assertEqual(t, "MinFreeSpaceMB", cfg.MinFreeSpaceMB, 500)
	assertEqualBool(t, "EraseAfterSync", cfg.EraseAfterSync, false)
}

// --- helpers ---

func writeTempTOML(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.toml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write temp TOML: %v", err)
	}
	return path
}

func assertEqual[T comparable](t *testing.T, field string, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("%s: got %v, want %v", field, got, want)
	}
}

func assertEqualFloat(t *testing.T, field string, got, want float64) {
	t.Helper()
	if got != want {
		t.Errorf("%s: got %f, want %f", field, got, want)
	}
}

func assertEqualBool(t *testing.T, field string, got, want bool) {
	t.Helper()
	if got != want {
		t.Errorf("%s: got %v, want %v", field, got, want)
	}
}

func TestEnvRangeValidation(t *testing.T) {
	// Port out of range — should preserve default (80).
	t.Run("port_zero", func(t *testing.T) {
		t.Setenv("LOGFALCON_WEB_PORT", "0")
		cfg, err := Load("/nonexistent/path/config.toml")
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.WebPort != 80 {
			t.Errorf("WebPort = %d, want 80 (default preserved)", cfg.WebPort)
		}
	})

	t.Run("port_too_high", func(t *testing.T) {
		t.Setenv("LOGFALCON_WEB_PORT", "65536")
		cfg, err := Load("/nonexistent/path/config.toml")
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.WebPort != 80 {
			t.Errorf("WebPort = %d, want 80 (default preserved)", cfg.WebPort)
		}
	})

	t.Run("port_negative", func(t *testing.T) {
		t.Setenv("LOGFALCON_WEB_PORT", "-1")
		cfg, err := Load("/nonexistent/path/config.toml")
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.WebPort != 80 {
			t.Errorf("WebPort = %d, want 80 (default preserved)", cfg.WebPort)
		}
	})

	t.Run("min_free_negative", func(t *testing.T) {
		t.Setenv("LOGFALCON_MIN_FREE_MB", "-500")
		cfg, err := Load("/nonexistent/path/config.toml")
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.MinFreeSpaceMB != 200 {
			t.Errorf("MinFreeSpaceMB = %d, want 200 (default preserved)", cfg.MinFreeSpaceMB)
		}
	})

	// Edge: valid boundaries must be accepted.
	t.Run("port_min_valid", func(t *testing.T) {
		t.Setenv("LOGFALCON_WEB_PORT", "1")
		cfg, err := Load("/nonexistent/path/config.toml")
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.WebPort != 1 {
			t.Errorf("WebPort = %d, want 1", cfg.WebPort)
		}
	})

	t.Run("port_max_valid", func(t *testing.T) {
		t.Setenv("LOGFALCON_WEB_PORT", "65535")
		cfg, err := Load("/nonexistent/path/config.toml")
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.WebPort != 65535 {
			t.Errorf("WebPort = %d, want 65535", cfg.WebPort)
		}
	})

	t.Run("min_free_zero_valid", func(t *testing.T) {
		t.Setenv("LOGFALCON_MIN_FREE_MB", "0")
		cfg, err := Load("/nonexistent/path/config.toml")
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.MinFreeSpaceMB != 0 {
			t.Errorf("MinFreeSpaceMB = %d, want 0", cfg.MinFreeSpaceMB)
		}
	})
}
