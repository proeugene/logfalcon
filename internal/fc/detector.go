// Package fc provides flight-controller detection via MSP handshake.
package fc

import (
	"fmt"
	"log"

	"github.com/proeugene/logfalcon/internal/msp"
)

// FCInfo holds identification info from the MSP handshake.
type FCInfo struct {
	APIMajor       int
	APIMinor       int
	Variant        string // "BTFL" or "INAV"
	UID            string // hex string
	BlackboxDevice int
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
	GetFCVariant() (string, error)
	GetUID() (string, error)
	GetBlackboxConfig() (int, error)
}

// Detect runs the MSP handshake and returns FC info.
//
// Steps (matching the Python detect_fc):
//  1. GetAPIVersion — log result; wrap errors as DetectionError
//  2. GetFCVariant  — check against SupportedVariants; NotSupportedError if unknown
//  3. GetUID        — use "unknown" on error
//  4. GetBlackboxConfig — BTFL queries MSP; INAV skips (assumes flash)
//  5. SDCard device → SDCardError
func Detect(client MSPClient) (*FCInfo, error) {
	// 1. API version
	major, minor, err := client.GetAPIVersion()
	if err != nil {
		return nil, &DetectionError{Message: fmt.Sprintf("MSP API_VERSION failed: %v", err)}
	}
	log.Printf("MSP API version: %d.%d", major, minor)

	// 2. FC variant
	variant, err := client.GetFCVariant()
	if err != nil {
		return nil, &DetectionError{Message: fmt.Sprintf("MSP FC_VARIANT failed: %v", err)}
	}
	log.Printf("FC variant: %q", variant)

	if len(variant) > 4 {
		variant = variant[:4]
	}
	if !msp.SupportedVariants[variant] {
		return nil, &NotSupportedError{Variant: variant}
	}

	// 3. UID — best effort
	uid := "unknown"
	if u, err := client.GetUID(); err == nil {
		uid = u
		log.Printf("FC UID: %s", uid)
	} else {
		log.Printf("Could not read FC UID, using 'unknown'")
	}

	// 4. Blackbox config
	bbDevice := msp.BlackboxDeviceNone
	if variant == msp.BTFLVariant {
		deviceType, err := client.GetBlackboxConfig()
		if err != nil {
			log.Printf("Could not read BLACKBOX_CONFIG: %v", err)
		} else {
			bbDevice = deviceType
			log.Printf("Blackbox device type: %d", bbDevice)
		}
	} else {
		// iNav deprecated MSP_BLACKBOX_CONFIG — assume flash until
		// DATAFLASH_SUMMARY proves otherwise.
		bbDevice = msp.BlackboxDeviceFlash
		log.Printf("Non-Betaflight FC — skipping BLACKBOX_CONFIG, assuming flash")
	}

	// 5. SD card → error
	if bbDevice == msp.BlackboxDeviceSDCard {
		return nil, &SDCardError{}
	}

	return &FCInfo{
		APIMajor:       major,
		APIMinor:       minor,
		Variant:        variant,
		UID:            uid,
		BlackboxDevice: bbDevice,
	}, nil
}
