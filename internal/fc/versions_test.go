package fc

import (
	"errors"
	"testing"
)

func TestCheckVersion_InRange(t *testing.T) {
	tests := []struct {
		variant string
		major   int
		minor   int
	}{
		{"BTFL", 1, 41}, // exactly at minimum
		{"BTFL", 1, 44}, // mid-range
		{"BTFL", 1, 47}, // exactly at maximum
		{"INAV", 1, 40}, // exactly at minimum
		{"INAV", 1, 43}, // mid-range
		{"INAV", 1, 46}, // exactly at maximum
	}
	for _, tc := range tests {
		err := CheckVersion(tc.variant, "4.0.0", tc.major, tc.minor)
		if err != nil {
			t.Errorf("CheckVersion(%q, %d.%d) = %v, want nil", tc.variant, tc.major, tc.minor, err)
		}
	}
}

func TestCheckVersion_TooOld_BTFL(t *testing.T) {
	err := CheckVersion("BTFL", "3.5.7", 1, 40)
	if err == nil {
		t.Fatal("expected VersionTooOldError, got nil")
	}
	var e *VersionTooOldError
	if !errors.As(err, &e) {
		t.Fatalf("expected *VersionTooOldError, got %T", err)
	}
	if e.Variant != "BTFL" {
		t.Errorf("Variant = %q, want BTFL", e.Variant)
	}
	if e.Major != 1 || e.Minor != 40 {
		t.Errorf("Detected API = %d.%d, want 1.40", e.Major, e.Minor)
	}
	if e.MinMinor != 41 {
		t.Errorf("MinMinor = %d, want 41", e.MinMinor)
	}
	if e.FirmwareVersion != "3.5.7" {
		t.Errorf("FirmwareVersion = %q, want 3.5.7", e.FirmwareVersion)
	}
}

func TestCheckVersion_TooOld_INAV(t *testing.T) {
	err := CheckVersion("INAV", "2.5.0", 1, 39)
	var e *VersionTooOldError
	if !errors.As(err, &e) {
		t.Fatalf("expected *VersionTooOldError, got %T", err)
	}
}

func TestCheckVersion_TooNew_BTFL(t *testing.T) {
	err := CheckVersion("BTFL", "4.9.0", 1, 49)
	if err == nil {
		t.Fatal("expected VersionTooNewError, got nil")
	}
	var e *VersionTooNewError
	if !errors.As(err, &e) {
		t.Fatalf("expected *VersionTooNewError, got %T", err)
	}
	if e.Variant != "BTFL" {
		t.Errorf("Variant = %q, want BTFL", e.Variant)
	}
	if e.Major != 1 || e.Minor != 49 {
		t.Errorf("Detected API = %d.%d, want 1.49", e.Major, e.Minor)
	}
	if e.MaxMinor != 48 {
		t.Errorf("MaxMinor = %d, want 48", e.MaxMinor)
	}
}

func TestCheckVersion_TooNew_INAV(t *testing.T) {
	err := CheckVersion("INAV", "8.0.0", 1, 47)
	var e *VersionTooNewError
	if !errors.As(err, &e) {
		t.Fatalf("expected *VersionTooNewError, got %T", err)
	}
}

func TestCheckVersion_UnknownVariant(t *testing.T) {
	// Unknown variant should pass through without error.
	err := CheckVersion("UNKN", "", 1, 0)
	if err != nil {
		t.Errorf("CheckVersion unknown variant = %v, want nil", err)
	}
}

func TestCheckVersion_ErrorMessages(t *testing.T) {
	oldErr := CheckVersion("BTFL", "3.5.7", 1, 40)
	if oldErr == nil {
		t.Fatal("expected error")
	}
	msg := oldErr.Error()
	if msg == "" {
		t.Error("VersionTooOldError message is empty")
	}
	// Should mention "Betaflight", the detected API version, and the minimum.
	for _, want := range []string{"Betaflight", "1.40", "1.41", "Betaflight 4.0"} {
		if !contains(msg, want) {
			t.Errorf("error message %q missing %q", msg, want)
		}
	}

	newErr := CheckVersion("BTFL", "4.9.0", 1, 49)
	if newErr == nil {
		t.Fatal("expected error")
	}
	newMsg := newErr.Error()
	for _, want := range []string{"Betaflight", "1.48", "1.48"} {
		if !contains(newMsg, want) {
			t.Errorf("too-new message %q missing %q", newMsg, want)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr(s, sub))
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
