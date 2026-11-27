package fc

import "fmt"

// versionBound describes the minimum (hard block) and maximum protocol-reviewed (soft warn)
// MSP API version for a given FC variant.
type versionBound struct {
	MinMajor int
	MinMinor int
	MaxMajor int
	MaxMinor int
}

// variantVersions maps each supported FC variant to its version bounds.
//
// Betaflight: MSP API 1.41 (BF 4.0) changed the MSP_DATAFLASH_READ response
// format to include an explicit dataSize + compressionType header.  Firmware
// older than 4.0 uses a different wire format that would corrupt log data.
//
// iNav remains experimental and copying is disabled before transfer.
var variantVersions = map[string]versionBound{
	"BTFL": {MinMajor: 1, MinMinor: 41, MaxMajor: 1, MaxMinor: 48},
	"INAV": {MinMajor: 1, MinMinor: 40, MaxMajor: 1, MaxMinor: 46},
}

// VersionTooOldError is returned when the FC firmware is too old to be safely
// read.  It causes a hard stop — syncing would produce corrupted data.
type VersionTooOldError struct {
	Variant         string
	FirmwareVersion string // human-readable, e.g. "3.5.7" (empty if unknown)
	Major, Minor    int
	MinMajor        int
	MinMinor        int
}

func (e *VersionTooOldError) Error() string {
	name := variantName(e.Variant)
	fw := ""
	if e.FirmwareVersion != "" {
		fw = fmt.Sprintf(" (%s)", e.FirmwareVersion)
	}
	return fmt.Sprintf(
		"%s%s uses MSP API %d.%d which is too old — minimum required is API %d.%d (%s). "+
			"Please update your FC firmware.",
		name, fw, e.Major, e.Minor, e.MinMajor, e.MinMinor,
		minVersionLabel(e.Variant, e.MinMajor, e.MinMinor),
	)
}

// VersionTooNewError is returned when the FC firmware is newer than the highest
// protocol-reviewed API.  Syncing is allowed to proceed but the user is warned.
type VersionTooNewError struct {
	Variant         string
	FirmwareVersion string // human-readable, e.g. "4.7.0" (empty if unknown)
	Major, Minor    int
	MaxMajor        int
	MaxMinor        int
}

func (e *VersionTooNewError) Error() string {
	name := variantName(e.Variant)
	fw := ""
	if e.FirmwareVersion != "" {
		fw = fmt.Sprintf(" (%s)", e.FirmwareVersion)
	}
	return fmt.Sprintf(
		"%s%s uses MSP API %d.%d which is newer than protocol-reviewed (highest reviewed: API %d.%d). "+
			"Sync will proceed — if anything looks wrong, please report your firmware version.",
		name, fw, e.Major, e.Minor, e.MaxMajor, e.MaxMinor,
	)
}

// CheckVersion returns:
//   - nil              if the version is within the protocol-reviewed range
//   - *VersionTooOldError  if the version is below the minimum (hard stop)
//   - *VersionTooNewError  if the version is above max protocol-reviewed (soft warning)
//
// An unknown variant is treated as in-range (variant validation is handled
// separately in Detect).
func CheckVersion(variant, firmwareVersion string, major, minor int) error {
	bounds, ok := variantVersions[variant]
	if !ok {
		return nil
	}

	if apiLessThan(major, minor, bounds.MinMajor, bounds.MinMinor) {
		return &VersionTooOldError{
			Variant:         variant,
			FirmwareVersion: firmwareVersion,
			Major:           major,
			Minor:           minor,
			MinMajor:        bounds.MinMajor,
			MinMinor:        bounds.MinMinor,
		}
	}

	if apiGreaterThan(major, minor, bounds.MaxMajor, bounds.MaxMinor) {
		return &VersionTooNewError{
			Variant:         variant,
			FirmwareVersion: firmwareVersion,
			Major:           major,
			Minor:           minor,
			MaxMajor:        bounds.MaxMajor,
			MaxMinor:        bounds.MaxMinor,
		}
	}

	return nil
}

func apiLessThan(maj, min, refMaj, refMin int) bool {
	return maj < refMaj || (maj == refMaj && min < refMin)
}

func apiGreaterThan(maj, min, refMaj, refMin int) bool {
	return maj > refMaj || (maj == refMaj && min > refMin)
}

func variantName(variant string) string {
	switch variant {
	case "BTFL":
		return "Betaflight"
	case "INAV":
		return "iNav"
	default:
		return variant
	}
}

// minVersionLabel returns a human-friendly label for the minimum required
// version, e.g. "Betaflight 4.0" or "iNav 2.6".
func minVersionLabel(variant string, major, minor int) string {
	switch variant {
	case "BTFL":
		if major == 1 && minor == 41 {
			return "Betaflight 4.0"
		}
	case "INAV":
		if major == 1 && minor == 40 {
			return "iNav 2.6"
		}
	}
	return fmt.Sprintf("API %d.%d", major, minor)
}
