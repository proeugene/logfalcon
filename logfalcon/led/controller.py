"""LED state machine with sysfs and optional GPIO backends.

Runs in a background thread. The main thread sets the desired state via
`set_state()`; the LED thread executes the corresponding blink pattern.

Field-optimised: four visible patterns that are unmistakable at a glance.
  READY — solid on (Pi booted, ready for FC)
  BUSY  — fast steady blink (don't unplug)
  DONE  — rapid burst then long solid (safe to unplug)
  ERROR — SOS repeating (something went wrong)

Backends:
  sysfs  — /sys/class/leds/led0/ (Pi built-in ACT LED, no extra hardware)
  gpio   — RPi.GPIO pin (optional external LED)
"""

from __future__ import annotations

import contextlib
import logging
import threading
import time
from enum import Enum, auto
from pathlib import Path

log = logging.getLogger(__name__)

# sysfs paths for Pi built-in ACT LED
_SYSFS_LED = Path('/sys/class/leds/led0')
_SYSFS_BRIGHTNESS = _SYSFS_LED / 'brightness'
_SYSFS_TRIGGER = _SYSFS_LED / 'trigger'


class LEDState(Enum):
    OFF = auto()
    BOOTING = auto()  # slow heartbeat (1 s on / 1 s off) — Pi is starting up
    READY = auto()  # solid on — Pi booted, ready for FC
    BUSY = auto()  # fast blink (150 ms on / 150 ms off) — sync in progress
    DONE = auto()  # 5× rapid flash then 3 s solid then off — safe to unplug
    ERROR = auto()  # SOS repeating — something went wrong


_PATTERNS: dict[LEDState, tuple[list[tuple[int, int]], bool]] = {
    LEDState.OFF: ([], False),
    LEDState.BOOTING: ([(1000, 1000)], True),
    LEDState.READY: ([], True),  # solid on — empty steps + repeat=True
    LEDState.BUSY: ([(150, 150)], True),
    LEDState.DONE: (
        # 5× rapid flash (attention getter) then 3 s solid then off
        [(50, 50), (50, 50), (50, 50), (50, 50), (50, 50), (3000, 1)],
        False,
    ),
    LEDState.ERROR: (
        # SOS: 3×short, 3×long, 3×short, pause
        [(150, 150)] * 3 + [(400, 150)] * 3 + [(150, 150)] * 3 + [(700, 700)],
        True,
    ),
}


class LEDController:
    """Thread-safe LED controller."""

    def __init__(
        self,
        backend: str = 'sysfs',
        gpio_pin: int = 17,
    ) -> None:
        self._backend = backend
        self._gpio_pin = gpio_pin
        self._state = LEDState.OFF
        self._lock = threading.Lock()
        self._event = threading.Event()
        self._idle_event = threading.Event()
        self._thread: threading.Thread | None = None
        self._running = False

        if backend == 'gpio':
            self._init_gpio()

    def _init_gpio(self) -> None:
        try:
            import RPi.GPIO as GPIO  # type: ignore

            GPIO.setmode(GPIO.BCM)
            GPIO.setup(self._gpio_pin, GPIO.OUT, initial=GPIO.LOW)
            self._gpio = GPIO
            log.debug('GPIO LED initialized on pin %d', self._gpio_pin)
        except ImportError:
            log.warning('RPi.GPIO not available, falling back to sysfs')
            self._backend = 'sysfs'

    def start(self) -> None:
        """Start the background LED thread."""
        self._running = True
        # Disable trigger so we can control brightness directly
        if self._backend == 'sysfs':
            self._sysfs_disable_trigger()
        self._thread = threading.Thread(target=self._run, daemon=True, name='led')
        self._thread.start()
        log.debug('LED controller started (backend=%s)', self._backend)

    def stop(self) -> None:
        """Stop the LED thread and turn off the LED."""
        self._running = False
        self._event.set()
        if self._thread:
            self._thread.join(timeout=3)
        self._set_raw(False)
        if self._backend == 'sysfs':
            self._sysfs_restore_trigger()

    def wait_until_idle(self, timeout: float = 10.0) -> None:
        """Block until the current LED pattern completes (or timeout)."""
        self._idle_event.wait(timeout=timeout)

    def set_state(self, state: LEDState) -> None:
        with self._lock:
            if self._state != state:
                log.info('LED state → %s', state.name)
                self._state = state
                self._event.set()

    def _run(self) -> None:
        while self._running:
            with self._lock:
                state = self._state
            self._event.clear()
            self._idle_event.clear()
            self._execute_pattern(state)

    def _execute_pattern(self, state: LEDState) -> None:
        steps, repeat = _PATTERNS[state]

        if not steps:
            # OFF (repeat=False) → LED off; READY (repeat=True) → LED on
            self._set_raw(repeat)
            self._idle_event.set()
            self._event.wait()  # wait for state change
            return

        while True:
            for on_ms, off_ms in steps:
                if self._state_changed(state):
                    return
                self._set_raw(True)
                if self._interruptible_sleep(on_ms / 1000.0, state):
                    return
                self._set_raw(False)
                if off_ms > 0 and self._interruptible_sleep(off_ms / 1000.0, state):
                    return
            if not repeat:
                self._set_raw(False)
                self._idle_event.set()
                self._event.wait()  # wait for next state change
                return

    def _interruptible_sleep(self, seconds: float, original_state: LEDState) -> bool:
        """Sleep for *seconds*, return True if state changed during sleep."""
        end = time.monotonic() + seconds
        while time.monotonic() < end:
            remaining = end - time.monotonic()
            self._event.wait(timeout=min(remaining, 0.05))
            if self._state_changed(original_state):
                return True
        return False

    def _state_changed(self, original: LEDState) -> bool:
        with self._lock:
            return self._state != original

    def _set_raw(self, on: bool) -> None:
        if self._backend == 'sysfs':
            self._sysfs_write(on)
        elif self._backend == 'gpio':
            try:
                self._gpio.output(self._gpio_pin, self._gpio.HIGH if on else self._gpio.LOW)
            except Exception as exc:
                log.debug('GPIO write error: %s', exc)

    def _sysfs_write(self, on: bool) -> None:
        with contextlib.suppress(OSError):
            _SYSFS_BRIGHTNESS.write_text('1' if on else '0')

    def _sysfs_disable_trigger(self) -> None:
        with contextlib.suppress(OSError):
            _SYSFS_TRIGGER.write_text('none')

    def _sysfs_restore_trigger(self) -> None:
        with contextlib.suppress(OSError):
            _SYSFS_TRIGGER.write_text('mmc0')
