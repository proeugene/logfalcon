package web

import (
	"fmt"
	"io/fs"
	"net/url"
	"strings"

	"github.com/proeugene/logfalcon/internal/storage"
	lfSync "github.com/proeugene/logfalcon/internal/sync"
)

type transferPresentation struct {
	statusPresentation
	ActionHref  string
	ActionLabel string
}

// Stable reason codes carry the actionable distinction. Unknown or older
// statuses retain the cautious generic message; free-form errors are not parsed.
var transferNextActions = map[string]string{
	"unsupported_controller": "This controller is not supported. Check compatibility before trying again.",
	"unsupported_firmware":   "Check the supported firmware versions before trying again.",
	"fc_sd_card":             "Read the controller’s SD card directly. LogFalcon copies internal flash.",
	"blackbox_not_flash":     "Check that Blackbox uses internal flash in Betaflight, then reconnect.",
	"flash_busy":             "Wait for the controller to finish, then reconnect.",
	"flash_unsupported":      "This controller does not expose supported internal flash. Check its Blackbox storage setting.",
	"storage_full":           "Free space on the Pi, then reconnect. Keep the controller’s data.",
	"serial_unavailable":     "Check the USB data cable and the Pi’s USB data port, then reconnect the controller.",
	"controller_not_ready":   "Check controller power and the USB data cable, then reconnect.",
	"copy_failed":            "Keep the controller’s data. Check the cable, then reconnect to retry the copy.",
	"storage_failed":         "Keep the controller’s data. Check the Pi SD card before trying again.",
	"verification_failed":    "Keep the controller’s data. Check the Pi SD card and reconnect to make a new copy.",
	"status_unavailable":     "Completion is unconfirmed. Reconnect to the appliance and check the saved recordings.",
	"update_recovery":        "Check Software update. Reconnect the controller when update recovery is complete.",
	"erase_skipped":          "Copy saved. LogFalcon skipped erasure because it could not confirm the flash still matched the copied length. Wait for logging to stop, then reconnect to collect again.",
	"erase_unconfirmed":      "Controller erasure is unconfirmed. Check the saved recording before changing controller storage.",
}

func recordingSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

func presentTransfer(s lfSync.Status) transferPresentation {
	p := transferPresentation{statusPresentation: presentation(s.State), ActionHref: "#connection-help", ActionLabel: "Connection help"}
	if s.State == "error" {
		if next, ok := transferNextActions[s.ReasonCode]; ok {
			p.Body = next
		}
	}
	if s.State != "complete" {
		return p
	}
	p.ActionHref, p.ActionLabel = "#saved-logs", "View recordings"
	copy := s.SavedCopy
	if copy == nil || copy.Bytes <= 0 || !fs.ValidPath(copy.SessionID) || strings.Count(copy.SessionID, "/") != 1 {
		return p // Older status files cannot identify an exact saved recording.
	}
	controller := "Controller ID unavailable"
	if copy.ControllerUID != "" && copy.ControllerUID != "unknown" {
		controller = "Controller " + copy.ControllerUID
	}
	outcome := "Controller retention is unconfirmed."
	switch {
	case copy.EraseCompleted && copy.EraseAttempted && !copy.ControllerDataRetained:
		outcome = "Controller flash erased."
	case copy.EraseAttempted:
		outcome = "Controller erasure is unconfirmed."
	case copy.ControllerDataRetained:
		outcome = "Data retained on controller."
	}
	p.Body = fmt.Sprintf("%s · %s saved. %s", controller, recordingSize(copy.Bytes), outcome)
	file := url.URL{Path: "/download/" + copy.SessionID + "/" + storage.RawFlashFilename}
	p.ActionHref, p.ActionLabel = file.EscapedPath(), "Download saved .bbl"
	return p
}
