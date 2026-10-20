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
	assertEqualBool(t, "StoragePressureCleanup", cfg.StoragePressureCleanup, false)

	// Sync behaviour
	assertEqualBool(t, "EraseAfterSync", cfg.EraseAfterSync, false)
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
	assertEqualBool(t, "DebugLogging", cfg.DebugLogging, true)
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
flash_read_compression = false
led_backend = "none"
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
	assertEqualBool(t, "FlashReadCompression", cfg.FlashReadCompression, false)
	assertEqual(t, "LEDBackend", cfg.LEDBackend, "none")
	assertEqual(t, "LEDGPIOPin", cfg.LEDGPIOPin, 22)
	assertEqual(t, "WebPort", cfg.WebPort, 8080)
	assertEqual(t, "HotspotSSID", cfg.HotspotSSID, "MyDrone")
	assertEqual(t, "HotspotPassword", cfg.HotspotPassword, "secret123")
	assertEqual(t, "IdleShutdownMinutes", cfg.IdleShutdownMinutes, 15)
}

func TestLoadMissing(t *testing.T) {
	if _, err := Load("/nonexistent/path/config.toml"); err == nil {
		t.Fatal("missing explicit config must fail")
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
	assertEqualBool(t, "StoragePressureCleanup", cfg.StoragePressureCleanup, false)
	assertEqualBool(t, "EraseAfterSync", cfg.EraseAfterSync, false)
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
led_backend = "none"
min_free_space_mb = 500
erase_after_sync = false`
	path := writeTempTOML(t, content)

	t.Setenv("LOGFALCON_STORAGE_PATH", "/env/storage")
	t.Setenv("LOGFALCON_WEB_PORT", "9090")
	t.Setenv("LOGFALCON_LED_BACKEND", "sysfs")
	t.Setenv("LOGFALCON_MIN_FREE_MB", "999")
	t.Setenv("LOGFALCON_ERASE_AFTER_SYNC", "true")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	assertEqual(t, "StoragePath (env)", cfg.StoragePath, "/env/storage")
	assertEqual(t, "WebPort (env)", cfg.WebPort, 9090)
	assertEqual(t, "LEDBackend (env)", cfg.LEDBackend, "sysfs")
	assertEqual(t, "MinFreeSpaceMB (env)", cfg.MinFreeSpaceMB, 999)
	assertEqualBool(t, "EraseAfterSync (env)", cfg.EraseAfterSync, true)
}

func TestEnvOverrideDefault(t *testing.T) {
	t.Setenv("LOGFALCON_STORAGE_PATH", "/env/default")
	t.Setenv("LOGFALCON_WEB_PORT", "3000")
	t.Setenv("LOGFALCON_LED_BACKEND", "none")
	t.Setenv("LOGFALCON_MIN_FREE_MB", "123")
	t.Setenv("LOGFALCON_ERASE_AFTER_SYNC", "false")

	cfg, err := Load("")
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
	for _, key := range []string{"LOGFALCON_WEB_PORT", "LOGFALCON_MIN_FREE_MB", "LOGFALCON_ERASE_AFTER_SYNC", "LOGFALCON_DEBUG"} {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, "invalid")
			if _, err := Load(""); err == nil {
				t.Fatal("invalid environment accepted")
			}
		})
	}
}

func TestEnvUnsetDoesNotOverride(t *testing.T) {
	content := `storage_path = "/tmp/logs"
web_port = 8080
led_backend = "none"
min_free_space_mb = 500
erase_after_sync = false`
	path := writeTempTOML(t, content)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	assertEqual(t, "StoragePath", cfg.StoragePath, "/tmp/logs")
	assertEqual(t, "WebPort", cfg.WebPort, 8080)
	assertEqual(t, "LEDBackend", cfg.LEDBackend, "none")
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

func TestInvalidConfiguration(t *testing.T) {
	for _, value := range []string{`erase_after_sync = tru`, `flash_chunk_size = 65536`, `flash_chunk_size = 0`, `serial_timeout = 0`, `serial_timeout = nan`, `web_port = 0`, `erase_after_sycn = true`, `storage_path = "relative"`, `led_backend = "gpio"`, `min_free_space_mb = -1`} {
		t.Run(value, func(t *testing.T) {
			if _, err := Load(writeTempTOML(t, value)); err == nil {
				t.Fatal("invalid config accepted")
			}
		})
	}
}
func TestEnvRangeValidation(t *testing.T) {
	for _, value := range []string{"0", "65536", "-1"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("LOGFALCON_WEB_PORT", value)
			if _, err := Load(""); err == nil {
				t.Fatal("invalid port accepted")
			}
		})
	}
}
