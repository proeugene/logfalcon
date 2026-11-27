package sync

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"syscall"
	"time"

	"github.com/proeugene/logfalcon/internal/msp"
	goSerial "go.bug.st/serial"
)

const controllerReadinessWindow = 3 * time.Second

// openReadyPort replaces a fixed USB settling sleep with bounded, read-only
// readiness probes. It retries transient device availability and transport
// errors, never a rejected/malformed MSP reply or a compatibility decision.
// Full controller and storage validation still runs once after this succeeds.
func (o *Orchestrator) openReadyPort(portPath string, deadline time.Time) (goSerial.Port, string, error) {
	openPort := o.OpenPort
	if openPort == nil {
		openPort = goSerial.Open
	}
	var port goSerial.Port
	var watchdog *time.Timer
	ready := false
	defer func() {
		if watchdog != nil {
			watchdog.Stop()
		}
		if port != nil && !ready {
			_ = port.Close()
		}
	}()
	reason := "serial_unavailable"
	var lastErr error
	for attempt := 1; time.Now().Before(deadline); attempt++ {
		if err := StatusError(); err != nil {
			return nil, "status_unavailable", err
		}
		if port == nil {
			var err error
			port, err = openPort(portPath, &goSerial.Mode{BaudRate: o.Config.SerialBaud, DataBits: 8, StopBits: goSerial.OneStopBit, Parity: goSerial.NoParity})
			if err != nil {
				lastErr = err
				if !transientSerialError(err) {
					return nil, "serial_unavailable", err
				}
			} else {
				// A serial write can block too. Capture this handle, never the
				// mutable retry variable, so an old timer cannot close a new port.
				opened := port
				watchdog = time.AfterFunc(time.Until(deadline), func() { _ = opened.Close() })
			}
		}
		if port != nil {
			reason = "controller_not_ready"
			probeTimeout := min(time.Duration(o.Config.SerialTimeout*float64(time.Second)), 250*time.Millisecond, time.Until(deadline))
			if probeTimeout <= 0 {
				break
			}
			if err := port.SetReadTimeout(probeTimeout); err != nil {
				return nil, "serial_unavailable", err
			}
			probe := msp.NewClient(port, probeTimeout)
			probe.SetDeadline(deadline)
			_, _, err := probe.GetAPIVersion()
			if err == nil && time.Now().Before(deadline) && watchdog.Stop() {
				ready = true
				slog.Info("controller responded to readiness probe", "attempts", attempt)
				return port, "", nil
			}
			lastErr = err
			var timeout *msp.TimeoutError
			if err != nil && !errors.As(err, &timeout) {
				if !transientSerialError(err) {
					return nil, reason, err
				}
				watchdog.Stop()
				_ = port.Close()
				port = nil
			}
		}
		slog.Debug("waiting for controller readiness", "attempt", attempt, "reason", reason)
		if remaining := time.Until(deadline); remaining > 0 {
			time.Sleep(min(100*time.Millisecond, remaining))
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("readiness deadline exceeded")
	}
	return nil, reason, lastErr
}

func transientSerialError(err error) bool {
	var serialError *goSerial.PortError
	if errors.As(err, &serialError) {
		switch serialError.Code() {
		case goSerial.PortBusy, goSerial.PortNotFound, goSerial.PortClosed:
			return true
		}
	}
	return errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ENODEV) || errors.Is(err, syscall.EIO) || errors.Is(err, syscall.EBUSY) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.ErrClosedPipe) || errors.Is(err, os.ErrClosed)
}
