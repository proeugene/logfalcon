package util

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ThermalZonePath is the sysfs path for the primary CPU temperature sensor.
// Exported as a variable so tests can override it (e.g., point at a test fixture).
var ThermalZonePath = "/sys/class/thermal/thermal_zone0/temp"

// CPUTemperature reads the CPU temperature in degrees Celsius from the
// Raspberry Pi's thermal zone sysfs interface. The file contains millidegrees
// Celsius (e.g., 45000 → 45.0°C).
//
// Returns 0, nil if the file doesn't exist (non-RPi platforms or missing driver).
// Returns an error if the file exists but is unreadable or malformed.
func CPUTemperature() (float64, error) {
	data, err := os.ReadFile(ThermalZonePath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("read thermal zone: %w", err)
	}

	millideg, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("parse thermal zone value: %w", err)
	}

	return float64(millideg) / 1000.0, nil
}

// ThermalWarningThreshold is the CPU temperature in °C above which a soft
// warning is logged. The Pi Zero 2 W can thermal-throttle above 80°C.
const ThermalWarningThreshold = 80.0
