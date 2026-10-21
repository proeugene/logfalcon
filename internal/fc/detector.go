// Package fc provides flight-controller detection via MSP handshake.
package fc

import (
	"fmt"
	"log/slog"

	"github.com/proeugene/logfalcon/internal/msp"
)

// FCInfo holds identification info from the MSP handshake.
type FCInfo struct {
	APIMajor        int
	APIMinor        int
	FirmwareVersion string // e.g. "4.5.0" — empty if MSPFCVersion not supported
	Variant         string // "BTFL" or "INAV"
	UID             string // hex string
	BlackboxDevice  int
	Warning         string // non-empty when firmware is newer than protocol-reviewed
}

// DetectionError indicates a generic MSP identification failure.
type DetectionError struct {
	Message string
}

func (e *DetectionError) Error() string { return e.Message }

// NotSupportedError indicates the FC variant is not Betaflight or iNav.
type NotSupportedError struct {
	Variant string
}

func (e *NotSupportedError) Error() string {
	return fmt.Sprintf("unsupported FC variant %q", e.Variant)
}

// SDCardError indicates the FC uses an SD card for blackbox storage.
type SDCardError struct{}

func (e *SDCardError) Error() string {
	return "FC uses SD card for blackbox — remove the FC SD card and read it directly"
}

// BlackboxEmptyError indicates the flash is already empty.
type BlackboxEmptyError struct{}

func (e *BlackboxEmptyError) Error() string {
	return "flash is already empty — nothing to sync"
}

// MSPClient is the interface the detector needs from the MSP client.
// Using an interface allows testing without a real serial port.
type MSPClient interface {
	GetAPIVersion() (int, int, error)
	GetFCVersion() (string, error)
	GetFCVariant() (string, error)
	GetUID() (string, error)
	GetBlackboxConfig() (int, error)
}

// Detect runs the MSP handshake and returns FC info.
//
// Steps:
//  1. GetAPIVersion — log result; wrap errors as DetectionError
//  2. GetFCVariant  — check against SupportedVariants; NotSupportedError if unknown
//  3. GetFCVersion  — human-readable firmware version string (best effort)
//  4. CheckVersion  — VersionTooOldError = hard stop; VersionTooNewError = warning
//  5. GetUID        — use "unknown" on error
//  6. GetBlackboxConfig — BTFL queries MSP; iNav is rejected before this step
//  7. SDCard device → SDCardError
func Detect(client MSPClient) (*FCInfo, error) {
	// 1. API version
	major, minor, err := client.GetAPIVersion()
	if err != nil {
		return nil, &DetectionError{Message: fmt.Sprintf("MSP API_VERSION failed: %v", err)}
	}
	slog.Info("MSP API version", "major", major, "minor", minor)

	// 2. FC variant
	variant, err := client.GetFCVariant()
	if err != nil {
		return nil, &DetectionError{Message: fmt.Sprintf("MSP FC_VARIANT failed: %v", err)}
	}
	slog.Info("FC variant", "variant", variant)

	if len(variant) > 4 {
		variant = variant[:4]
	}
	if !msp.SupportedVariants[variant] {
		return nil, &NotSupportedError{Variant: variant}
	}
	// Its current DATAFLASH_SUMMARY wire layout has not been validated here.
	// Do not guess storage state, particularly when automatic erase is enabled.
	if variant == msp.INAVVariant {
		return nil, &DetectionError{Message: "iNav — Experimental; copying currently disabled"}
	}

	// 3. Firmware version string (best effort — some older FC builds don't support this)
	firmwareVersion := ""
	if fv, err := client.GetFCVersion(); err == nil {
		firmwareVersion = fv
		slog.Info("FC firmware version", "version", firmwareVersion)
	} else {
		slog.Debug("MSPFCVersion not available", "error", err)
	}

	// 4. Version check — block if too old, warn if too new
	warning := ""
	if verr := CheckVersion(variant, firmwareVersion, major, minor); verr != nil {
		switch verr.(type) {
		case *VersionTooOldError:
			return nil, verr
		case *VersionTooNewError:
			warning = verr.Error()
			slog.Warn("FC firmware newer than protocol-reviewed", "warning", warning)
		}
	}

	// 5. UID — best effort
	uid := "unknown"
	if u, err := client.GetUID(); err == nil {
		uid = u
		slog.Info("FC UID", "uid", uid)
	} else {
		slog.Warn("could not read FC UID, using 'unknown'")
	}

	// 6. Betaflight blackbox configuration. iNav was rejected above.
	bbDevice, err := client.GetBlackboxConfig()
	if err != nil {
		return nil, &DetectionError{Message: "Cannot establish the configured blackbox storage device"}
	}
	slog.Info("blackbox device type", "device", bbDevice)

	// 7. SD card → error
	if bbDevice == msp.BlackboxDeviceSDCard {
		return nil, &SDCardError{}
	}
	if bbDevice != msp.BlackboxDeviceFlash {
		return nil, &DetectionError{Message: "Blackbox must be configured to use internal flash"}
	}

	return &FCInfo{
		APIMajor:        major,
		APIMinor:        minor,
		FirmwareVersion: firmwareVersion,
		Variant:         variant,
		UID:             uid,
		BlackboxDevice:  bbDevice,
		Warning:         warning,
	}, nil
}
