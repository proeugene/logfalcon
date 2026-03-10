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
	assertEqual(t, "HotspotSSID", cfg.HotspotSSID, "BF-Blackbox")
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
