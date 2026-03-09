"""Tests for logfalcon.led.controller."""

from __future__ import annotations

import sys
import threading
import types
from unittest.mock import MagicMock, call, patch

from logfalcon.led.controller import (
    _PATTERNS,
    LEDController,
    LEDState,
)

# ---------------------------------------------------------------------------
# LEDState enum
# ---------------------------------------------------------------------------

class TestLEDStateEnum:
    def test_all_expected_states_exist(self):
        expected = {'OFF', 'BOOTING', 'BUSY', 'DONE', 'ERROR'}
        actual = {s.name for s in LEDState}
        assert actual == expected

    def test_states_are_unique(self):
        values = [s.value for s in LEDState]
        assert len(values) == len(set(values))


# ---------------------------------------------------------------------------
# _PATTERNS dict
# ---------------------------------------------------------------------------

class TestPatterns:
    def test_every_state_has_pattern(self):
        for state in LEDState:
            assert state in _PATTERNS, f'Missing pattern for {state}'

    def test_pattern_entries_are_tuple_pair(self):
        for _state, entry in _PATTERNS.items():
            steps, repeat = entry
            assert isinstance(steps, list)
            assert isinstance(repeat, bool)

    def test_off_pattern_is_empty_no_repeat(self):
        steps, repeat = _PATTERNS[LEDState.OFF]
        assert steps == []
        assert repeat is False

    def test_busy_pattern_repeats(self):
        _, repeat = _PATTERNS[LEDState.BUSY]
        assert repeat is True

    def test_done_pattern_does_not_repeat(self):
        _, repeat = _PATTERNS[LEDState.DONE]
        assert repeat is False

    def test_done_pattern_has_burst_and_solid(self):
        steps, _ = _PATTERNS[LEDState.DONE]
        # 5× rapid flash (50, 50) then one solid (3000, 1)
        assert len(steps) == 6
        rapid = steps[:5]
        assert all(on == 50 and off == 50 for on, off in rapid)
        solid_on, solid_off = steps[5]
        assert solid_on == 3000
        assert solid_off == 1

    def test_error_pattern_repeats(self):
        _, repeat = _PATTERNS[LEDState.ERROR]
        assert repeat is True


# ---------------------------------------------------------------------------
# LEDController.set_state()
# ---------------------------------------------------------------------------

class TestSetState:
    def test_state_is_stored(self):
        ctrl = LEDController(backend='sysfs')
        ctrl.set_state(LEDState.BUSY)
        assert ctrl._state is LEDState.BUSY

    def test_event_is_set_on_state_change(self):
        ctrl = LEDController(backend='sysfs')
        assert not ctrl._event.is_set()
        ctrl.set_state(LEDState.BUSY)
        assert ctrl._event.is_set()

    def test_event_not_set_when_state_unchanged(self):
        ctrl = LEDController(backend='sysfs')
        ctrl.set_state(LEDState.OFF)
        ctrl._event.clear()
        ctrl.set_state(LEDState.OFF)  # same state
        assert not ctrl._event.is_set()

    def test_set_state_is_thread_safe(self):
        ctrl = LEDController(backend='sysfs')
        errors = []

        def toggle(n):
            try:
                for _ in range(100):
                    ctrl.set_state(LEDState.BUSY if n % 2 == 0 else LEDState.DONE)
            except Exception as exc:
                errors.append(exc)

        threads = [threading.Thread(target=toggle, args=(i,)) for i in range(4)]
        for t in threads:
            t.start()
        for t in threads:
            t.join(timeout=5)
        assert not errors


# ---------------------------------------------------------------------------
# LEDController.start() / stop()
# ---------------------------------------------------------------------------

class TestStartStop:
    @patch.object(LEDController, '_sysfs_disable_trigger')
    @patch.object(LEDController, '_run')
    def test_start_creates_daemon_thread(self, mock_run, mock_trigger):
        ctrl = LEDController(backend='sysfs')
        ctrl.start()
        assert ctrl._thread is not None
        assert ctrl._thread.daemon is True
        assert ctrl._thread.name == 'led'
        ctrl._running = False
        ctrl._event.set()
        ctrl._thread.join(timeout=2)

    @patch.object(LEDController, '_sysfs_disable_trigger')
    @patch.object(LEDController, '_sysfs_restore_trigger')
    @patch.object(LEDController, '_set_raw')
    def test_stop_joins_thread(self, mock_raw, mock_restore, mock_disable):
        ctrl = LEDController(backend='sysfs')
        ctrl.start()
        ctrl.stop()
        assert ctrl._running is False
        mock_raw.assert_called_with(False)
        mock_restore.assert_called_once()


# ---------------------------------------------------------------------------
# Sysfs backend
# ---------------------------------------------------------------------------

class TestSysfsBackend:
    def test_sysfs_write_on(self):
        mock_path = MagicMock()
        with patch('logfalcon.led.controller._SYSFS_BRIGHTNESS', mock_path):
            ctrl = LEDController(backend='sysfs')
            ctrl._sysfs_write(True)
        mock_path.write_text.assert_called_once_with('1')

    def test_sysfs_write_off(self):
        mock_path = MagicMock()
        with patch('logfalcon.led.controller._SYSFS_BRIGHTNESS', mock_path):
            ctrl = LEDController(backend='sysfs')
            ctrl._sysfs_write(False)
        mock_path.write_text.assert_called_once_with('0')

    def test_sysfs_write_suppresses_oserror(self):
        mock_path = MagicMock()
        mock_path.write_text.side_effect = OSError('permission denied')
        with patch('logfalcon.led.controller._SYSFS_BRIGHTNESS', mock_path):
            ctrl = LEDController(backend='sysfs')
            # Should not raise
            ctrl._sysfs_write(True)


# ---------------------------------------------------------------------------
# GPIO backend
# ---------------------------------------------------------------------------

class TestGPIOBackend:
    def _make_mock_gpio(self):
        gpio = MagicMock()
        gpio.BCM = 11
        gpio.OUT = 0
        gpio.LOW = 0
        gpio.HIGH = 1
        return gpio

    def test_init_gpio_calls_setup(self):
        mock_gpio = self._make_mock_gpio()
        fake_module = types.ModuleType('RPi.GPIO')
        for attr in ('setmode', 'setup', 'output', 'BCM', 'OUT', 'LOW', 'HIGH'):
            setattr(fake_module, attr, getattr(mock_gpio, attr))

        rpi_pkg = types.ModuleType('RPi')
        rpi_pkg.GPIO = fake_module

        with patch.dict(sys.modules, {'RPi': rpi_pkg, 'RPi.GPIO': fake_module}):
            ctrl = LEDController(backend='gpio', gpio_pin=17)

        assert ctrl is not None
        mock_gpio.setmode.assert_called_once_with(mock_gpio.BCM)
        mock_gpio.setup.assert_called_once_with(17, mock_gpio.OUT, initial=mock_gpio.LOW)

    def test_gpio_output_called_on_set_raw(self):
        mock_gpio = self._make_mock_gpio()
        ctrl = LEDController.__new__(LEDController)
        ctrl._backend = 'gpio'
        ctrl._gpio_pin = 17
        ctrl._gpio = mock_gpio

        ctrl._set_raw(True)
        mock_gpio.output.assert_called_with(17, mock_gpio.HIGH)
        mock_gpio.output.reset_mock()

        ctrl._set_raw(False)
        mock_gpio.output.assert_called_with(17, mock_gpio.LOW)


# ---------------------------------------------------------------------------
# Pattern execution — BUSY blink intervals
# ---------------------------------------------------------------------------

class TestPatternExecution:
    @patch.object(LEDController, '_sysfs_disable_trigger')
    def test_busy_pattern_blinks(self, mock_trigger):
        """BUSY pattern should toggle the LED on/off repeatedly."""
        mock_brightness = MagicMock()
        with patch('logfalcon.led.controller._SYSFS_BRIGHTNESS', mock_brightness):
            ctrl = LEDController(backend='sysfs')
            ctrl.start()
            ctrl.set_state(LEDState.BUSY)

            import time
            time.sleep(0.4)

            ctrl.stop()

        on_calls = [c for c in mock_brightness.write_text.call_args_list if c == call('1')]
        off_calls = [c for c in mock_brightness.write_text.call_args_list if c == call('0')]
        # Expect at least one full on/off cycle
        assert len(on_calls) >= 1
        assert len(off_calls) >= 1


# ---------------------------------------------------------------------------
# _interruptible_sleep()
# ---------------------------------------------------------------------------

class TestInterruptibleSleep:
    def test_returns_false_when_not_interrupted(self):
        ctrl = LEDController(backend='sysfs')
        ctrl._state = LEDState.BUSY
        result = ctrl._interruptible_sleep(0.05, LEDState.BUSY)
        assert result is False

    def test_returns_true_when_state_changes(self):
        ctrl = LEDController(backend='sysfs')
        ctrl._state = LEDState.BUSY
        ctrl._lock = threading.Lock()
        ctrl._event = threading.Event()

        def change_state():
            import time
            time.sleep(0.02)
            ctrl.set_state(LEDState.DONE)

        t = threading.Thread(target=change_state)
        t.start()

        result = ctrl._interruptible_sleep(2.0, LEDState.BUSY)
        t.join(timeout=3)
        assert result is True


# ---------------------------------------------------------------------------
# DONE pattern shape
# ---------------------------------------------------------------------------

class TestDonePattern:
    def test_done_has_burst_solid_off_sequence(self):
        steps, repeat = _PATTERNS[LEDState.DONE]
        # 5 rapid flashes
        rapid = steps[:5]
        for on_ms, off_ms in rapid:
            assert on_ms == 50
            assert off_ms == 50
        # Long solid on
        solid_on, solid_off = steps[5]
        assert solid_on == 3000
        # Tiny off (essentially stays on during that step)
        assert solid_off <= 10
        # Non-repeating
        assert repeat is False
