"""Main sync state machine — 10-step algorithm.

Steps:
  1. Wait (handled by systemd ExecStartPre)
  2. Identify FC (MSP handshake, verify BTFL)
  3. Query flash state (summary)
  4. Check Pi storage
  5. Prepare output (session dir + file)
  6. Stream flash read → file [LED=BUSY]
  7. Verify integrity [LED=BUSY]
  8. Write manifest
  9. Erase FC flash [LED=BUSY]
 10. Signal result [LED=DONE / ERROR]
"""

from __future__ import annotations

import glob as glob_module
import logging
import threading
import time
from enum import Enum, auto
from pathlib import Path

from logfalcon.config import Config
from logfalcon.fc.detector import (
    FCDetectionError,
    FCInfo,
    FCNotBetaflight,
    FCSDCardBlackbox,
    detect_fc,
)
from logfalcon.led.controller import LEDController, LEDState
from logfalcon.msp.client import MSPClient, MSPError
from logfalcon.storage.manifest import (
    cleanup_oldest_sessions,
    make_session_dir,
    update_manifest_erase,
    write_manifest,
)
from logfalcon.storage.writer import StreamWriter
from logfalcon.util.disk_space import free_mb

log = logging.getLogger(__name__)

_MAX_CONSECUTIVE_ERRORS = 5
_ERASE_POLL_INTERVAL = 2.0  # seconds between erase polls


class SyncResult(Enum):
    SUCCESS = auto()
    ALREADY_EMPTY = auto()
    ERROR = auto()
    DRY_RUN = auto()


# Shared sync status — read by web server (thread-safe)
_status_lock = threading.Lock()
_current_status: dict = {'state': 'idle', 'progress': 0, 'message': 'Ready for the next sync.'}


def get_status() -> dict:
    with _status_lock:
        return dict(_current_status)


def _set_status(state: str, progress: int = 0, message: str = '') -> None:
    with _status_lock:
        _current_status['state'] = state
        _current_status['progress'] = progress
        _current_status['message'] = message


class SyncOrchestrator:
    """Runs the full blackbox sync workflow."""

    def __init__(
        self,
        config: Config,
        led: LEDController,
        dry_run: bool = False,
    ) -> None:
        self.config = config
        self.led = led
        self.dry_run = dry_run

    def run(self, port: str) -> SyncResult:
        """Run the full sync workflow. Returns SyncResult."""
        try:
            return self._run(port)
        except Exception as exc:
            log.exception('Unexpected error during sync: %s', exc)
            self.led.set_state(LEDState.ERROR)
            _set_status(
                'error', message='Unexpected sync error. Check the service log for details.'
            )
            return SyncResult.ERROR

    def _run(self, port: str) -> SyncResult:
        cfg = self.config
        total_started = time.monotonic()
        timings: dict[str, float] = {}

        # --- Step 2: Identify FC ---
        log.info('Step 2: Identifying FC on %s', port)
        _set_status('identifying', message='Checking the flight controller over MSP.')
        identify_started = time.monotonic()
        with MSPClient(port, cfg.serial_baud, cfg.serial_timeout) as client:
            result = self._identify_fc(client)
            if isinstance(result, SyncResult):
                return result
            fc_info = result
            timings['identify_sec'] = round(time.monotonic() - identify_started, 3)

            # --- Step 3: Query flash state ---
            log.info('Step 3: Querying flash state')
            _set_status('querying', message='Reading blackbox flash usage from the FC.')
            query_started = time.monotonic()
            result = self._query_flash_state(client)
            if isinstance(result, SyncResult):
                return result
            used_size, _total_size = result
            timings['query_sec'] = round(time.monotonic() - query_started, 3)

            # --- Steps 4–5: Check storage and prepare output ---
            log.info('Step 4: Checking Pi storage')
            result = self._check_storage(fc_info, used_size)
            if isinstance(result, SyncResult):
                return result
            session_dir, bbl_path, writer = result

            # --- Step 6: Stream flash read ---
            log.info('Step 6: Reading %d bytes from flash → %s', used_size, bbl_path)
            self.led.set_state(LEDState.BUSY)
            _set_status('syncing', 0, message='Copying blackbox flash to the Pi SD card.')
            stream_started = time.monotonic()
            result = self._read_flash(client, writer, used_size)
            if isinstance(result, SyncResult):
                return result
            timings['stream_sec'] = round(time.monotonic() - stream_started, 3)

            # --- Step 7: Verify integrity ---
            log.info('Step 7: Verifying integrity')
            self.led.set_state(LEDState.BUSY)
            _set_status('verifying', message='Verifying the copied file before erase.')
            verify_started = time.monotonic()
            result = self._verify_integrity(writer, used_size)
            if isinstance(result, SyncResult):
                return result
            file_sha256 = result
            timings['verify_sec'] = round(time.monotonic() - verify_started, 3)

            # --- Step 8: Write manifest ---
            log.info('Step 8: Writing manifest')
            timings['total_sec'] = round(time.monotonic() - total_started, 3)
            write_manifest(
                session_dir,
                fc_info,
                sha256=file_sha256,
                used_size=used_size,
                erase_completed=False,
                erase_attempted=False,
                timing=timings,
            )

            if self.dry_run:
                log.info('DRY RUN — skipping erase')
                self.led.set_state(LEDState.DONE)
                _set_status('idle', message='Copy complete. Dry run kept the FC flash untouched.')
                return SyncResult.DRY_RUN

            if not cfg.erase_after_sync:
                log.info('erase_after_sync=false — skipping erase')
                self.led.set_state(LEDState.DONE)
                _set_status('idle', message='Copy complete. Erase was skipped by configuration.')
                return SyncResult.SUCCESS

            # --- Step 9: Erase FC flash ---
            log.info('Step 9: Erasing FC flash')
            self.led.set_state(LEDState.BUSY)
            _set_status('erasing', message='Erasing the FC flash now that the copy is verified.')
            result = self._erase_flash(client, session_dir, timings, total_started)
            if isinstance(result, SyncResult):
                return result

        # --- Step 10: Signal result ---
        log.info('Step 10: Sync complete — SUCCESS')
        self.led.set_state(LEDState.DONE)
        _set_status('idle', message='Sync complete — safe to unplug and fly again.')
        return SyncResult.SUCCESS

    def _identify_fc(self, client: MSPClient) -> FCInfo | SyncResult:
        """Step 2: Detect and identify the flight controller."""
        try:
            fc_info = detect_fc(client)
        except (FCSDCardBlackbox, FCNotBetaflight, FCDetectionError) as exc:
            log.error('FC detection failed: %s', exc)
            self.led.set_state(LEDState.ERROR)
            _set_status('error', message=str(exc))
            return SyncResult.ERROR

        log.info('FC identified: variant=%r uid=%s', fc_info.variant, fc_info.uid)
        return fc_info

    def _query_flash_state(self, client: MSPClient) -> tuple[int, int] | SyncResult:
        """Step 3: Query dataflash summary. Returns (used_size, total_size)."""
        try:
            summary = client.get_dataflash_summary()
        except MSPError as exc:
            log.error('Failed to get flash summary: %s', exc)
            self.led.set_state(LEDState.ERROR)
            _set_status('error', message='Could not read the FC flash summary.')
            return SyncResult.ERROR

        log.info(
            'Flash: supported=%s ready=%s used=%d total=%d',
            summary['supported'],
            summary['ready'],
            summary['used_size'],
            summary['total_size'],
        )

        if not summary['supported']:
            log.error('FC flash not supported')
            self.led.set_state(LEDState.ERROR)
            _set_status('error', message='This FC does not expose supported flash storage.')
            return SyncResult.ERROR

        if not summary['ready']:
            log.error('FC flash not ready (may be busy)')
            self.led.set_state(LEDState.ERROR)
            _set_status(
                'error', message='The FC flash is busy right now. Try again in a moment.'
            )
            return SyncResult.ERROR

        used_size = summary['used_size']
        if used_size == 0:
            log.info('Flash is empty — nothing to sync')
            self.led.set_state(LEDState.DONE)
            _set_status('idle', message='Flash already empty — nothing to sync.')
            return SyncResult.ALREADY_EMPTY

        return (used_size, summary['total_size'])

    def _check_storage(
        self, fc_info: FCInfo, used_size: int
    ) -> tuple[Path, Path, StreamWriter] | SyncResult:
        """Steps 4–5: Check Pi storage and prepare output directory."""
        cfg = self.config
        storage_path = Path(cfg.storage_path)
        storage_path.mkdir(parents=True, exist_ok=True)
        required_mb = (used_size / (1024 * 1024)) + cfg.min_free_space_mb
        available_mb = free_mb(storage_path)
        log.info('Storage: required=%.1f MB available=%.1f MB', required_mb, available_mb)
        if available_mb < required_mb:
            reclaimed_sessions: list[str] = []
            if cfg.storage_pressure_cleanup:
                _set_status(
                    'querying',
                    message='Storage is tight, cleaning up the oldest sessions first.',
                )
                reclaimed_sessions = cleanup_oldest_sessions(
                    storage_path,
                    required_free_bytes=int(required_mb * 1024 * 1024),
                )
                available_mb = free_mb(storage_path)
                if reclaimed_sessions:
                    log.warning(
                        'Reclaimed storage by deleting %d old session(s): %s',
                        len(reclaimed_sessions),
                        ', '.join(reclaimed_sessions),
                    )
                log.info('Storage after cleanup: available=%.1f MB', available_mb)
            if available_mb < required_mb:
                log.error(
                    'Insufficient Pi storage: %.1f MB available, %.1f MB required',
                    available_mb,
                    required_mb,
                )
                self.led.set_state(LEDState.ERROR)
                _set_status(
                    'error',
                    message='Not enough free space on the Pi SD card to copy this log safely.',
                )
                return SyncResult.ERROR

        # --- Step 5: Prepare output ---
        log.info('Step 5: Preparing output directory')
        session_dir = make_session_dir(storage_path, fc_info)
        bbl_path = session_dir / 'raw_flash.bbl'
        writer = StreamWriter(bbl_path)
        writer.open()
        return (session_dir, bbl_path, writer)

    def _read_flash(
        self, client: MSPClient, writer: StreamWriter, used_size: int
    ) -> None | SyncResult:
        """Step 6: Stream flash data from FC to file. Returns None on success."""
        cfg = self.config
        address = 0
        consecutive_errors = 0

        try:
            # Send first request (prime the pipeline)
            first_chunk_size = min(cfg.flash_chunk_size, used_size - address)
            client.send_flash_read_request(
                address, first_chunk_size, compression=cfg.flash_read_compression
            )

            while address < used_size:
                try:
                    chunk_addr, data = client.receive_flash_read_response(
                        compression=cfg.flash_read_compression
                    )
                except MSPError as exc:
                    consecutive_errors += 1
                    log.warning(
                        'Flash read error at 0x%08x (attempt %d/%d): %s',
                        address,
                        consecutive_errors,
                        _MAX_CONSECUTIVE_ERRORS,
                        exc,
                    )
                    if consecutive_errors >= _MAX_CONSECUTIVE_ERRORS:
                        log.error('Too many consecutive read errors — aborting')
                        writer.abort()
                        self.led.set_state(LEDState.ERROR)
                        _set_status(
                            'error',
                            message='Too many FC read errors. Try another USB cable and sync again.',
                        )
                        return SyncResult.ERROR
                    time.sleep(0.1)
                    # Re-send the same request on error
                    client.send_flash_read_request(
                        address,
                        min(cfg.flash_chunk_size, used_size - address),
                        compression=cfg.flash_read_compression,
                    )
                    continue

                if chunk_addr != address:
                    log.warning(
                        'Address mismatch: expected 0x%08x got 0x%08x — retrying',
                        address,
                        chunk_addr,
                    )
                    consecutive_errors += 1
                    if consecutive_errors >= _MAX_CONSECUTIVE_ERRORS:
                        log.error('Too many address mismatches — aborting')
                        writer.abort()
                        self.led.set_state(LEDState.ERROR)
                        _set_status(
                            'error',
                            message='The FC returned inconsistent data. Reconnect and try again.',
                        )
                        return SyncResult.ERROR
                    # Re-send the same request on mismatch
                    client.send_flash_read_request(
                        address,
                        min(cfg.flash_chunk_size, used_size - address),
                        compression=cfg.flash_read_compression,
                    )
                    continue

                if not data:
                    log.info('FC returned 0 bytes at 0x%08x — end of data', address)
                    break

                consecutive_errors = 0

                # Pipeline: send next request BEFORE processing current data
                next_address = address + len(data)
                if next_address < used_size:
                    next_chunk_size = min(cfg.flash_chunk_size, used_size - next_address)
                    client.send_flash_read_request(
                        next_address,
                        next_chunk_size,
                        compression=cfg.flash_read_compression,
                    )

                writer.write(data)
                address = next_address

                progress = int(address * 100 / used_size)
                _set_status(
                    'syncing', progress, message='Copying blackbox flash to the Pi SD card.'
                )
                if address % (cfg.flash_chunk_size * 64) < cfg.flash_chunk_size:
                    log.debug('Read 0x%08x / 0x%08x (%d%%)', address, used_size, progress)

        except Exception as exc:
            log.exception('Unexpected error during flash read: %s', exc)
            writer.abort()
            self.led.set_state(LEDState.ERROR)
            _set_status('error', message='Unexpected error while copying flash data.')
            return SyncResult.ERROR

        writer.close()
        log.info('Flash read complete: %d bytes written', writer.bytes_written)
        return None

    def _verify_integrity(self, writer: StreamWriter, used_size: int) -> str | SyncResult:
        """Step 7: Verify SHA-256 integrity. Returns file_sha256 on success."""
        if writer.bytes_written != used_size:
            log.error(
                'Size mismatch: wrote %d bytes, expected %d', writer.bytes_written, used_size
            )
            self.led.set_state(LEDState.ERROR)
            _set_status(
                'error', message='The copied file size did not match the FC flash size.'
            )
            return SyncResult.ERROR

        match, file_sha256 = writer.verify_against_file()
        if not match:
            log.error('SHA-256 verification failed — NOT erasing FC flash')
            self.led.set_state(LEDState.ERROR)
            _set_status(
                'error',
                message='Verification failed, so the FC flash was left untouched.',
            )
            return SyncResult.ERROR

        log.info('Integrity OK — SHA-256: %s', file_sha256)
        return file_sha256

    def _erase_flash(
        self,
        client: MSPClient,
        session_dir: Path,
        timings: dict[str, float],
        total_started: float,
    ) -> None | SyncResult:
        """Step 9: Erase FC flash and update manifest. Returns None on success."""
        erase_started = time.monotonic()

        erase_ok = self._wait_for_erase(client)
        timings['erase_sec'] = round(time.monotonic() - erase_started, 3)
        timings['total_sec'] = round(time.monotonic() - total_started, 3)
        update_manifest_erase(session_dir, erase_completed=erase_ok, timing=timings)

        if not erase_ok:
            log.error('Flash erase did not complete within timeout')
            self.led.set_state(LEDState.ERROR)
            _set_status(
                'error',
                message='Copy succeeded, but erase did not finish before timeout.',
            )
            return SyncResult.ERROR

        log.info('Flash erase confirmed')
        return None

    def _wait_for_erase(self, client: MSPClient) -> bool:
        """Send erase command and poll until flash is empty or timeout."""
        client.erase_flash()
        deadline = time.monotonic() + self.config.erase_timeout_sec
        while time.monotonic() < deadline:
            time.sleep(_ERASE_POLL_INTERVAL)
            try:
                summary = client.get_dataflash_summary()
            except MSPError as exc:
                log.warning('Error polling flash summary during erase: %s', exc)
                continue
            log.debug('Erase poll: used=%d ready=%s', summary['used_size'], summary['ready'])
            if summary['used_size'] == 0 and summary['ready']:
                return True
        return False


def auto_detect_port() -> str | None:
    """Return first available /dev/ttyACM* port, or None."""
    ports = sorted(glob_module.glob('/dev/ttyACM*'))
    if ports:
        log.info('Auto-detected port: %s', ports[0])
        return ports[0]
    return None
