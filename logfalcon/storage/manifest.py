"""Session directory creation and manifest.json read/write."""

from __future__ import annotations

import json
import logging
import os
import shutil
from datetime import UTC, datetime
from pathlib import Path

from logfalcon.fc.detector import FCInfo
from logfalcon.util.disk_space import free_bytes

log = logging.getLogger(__name__)

MANIFEST_FILENAME = 'manifest.json'
RAW_FLASH_FILENAME = 'raw_flash.bbl'


def _atomic_json_write(path: Path, data: dict) -> None:
    """Write *data* as JSON to *path* with fsync for durability."""
    fd = os.open(str(path), os.O_WRONLY | os.O_CREAT | os.O_TRUNC)
    try:
        os.write(fd, json.dumps(data, indent=2).encode())
        os.fsync(fd)
    finally:
        os.close(fd)


def make_session_dir(storage_root: Path, fc_info: FCInfo) -> Path:
    """Create and return a new timestamped session directory.

    Layout::

        <storage_root>/fc_BTFL_uid-<uid8>/<YYYY-MM-DD_HHMMSS>/
    """
    # Use first 8 chars of UID (or 'unknown') for the directory name
    uid_short = fc_info.uid[:8] if fc_info.uid != 'unknown' else 'unknown'
    fc_dir = storage_root / f'fc_BTFL_uid-{uid_short}'
    timestamp = datetime.now().strftime('%Y-%m-%d_%H%M%S')
    session_dir = fc_dir / timestamp
    session_dir.mkdir(parents=True, exist_ok=True)
    log.info('Created session directory: %s', session_dir)
    return session_dir


def write_manifest(
    session_dir: Path,
    fc_info: FCInfo,
    sha256: str,
    used_size: int,
    erase_completed: bool = False,
    erase_attempted: bool = False,
    timing: dict[str, float] | None = None,
) -> Path:
    """Write manifest.json to session_dir. Returns path to the file."""
    manifest = {
        'version': 1,
        'created_utc': datetime.now(UTC).isoformat(),
        'fc': {
            'variant': fc_info.variant.decode('ascii', errors='replace'),
            'uid': fc_info.uid,
            'api_version': f'{fc_info.api_major}.{fc_info.api_minor}',
            'blackbox_device': fc_info.blackbox_device,
        },
        'file': {
            'name': RAW_FLASH_FILENAME,
            'sha256': sha256,
            'bytes': used_size,
        },
        'erase_attempted': erase_attempted,
        'erase_completed': erase_completed,
    }
    if timing:
        manifest['timing'] = timing
    path = session_dir / MANIFEST_FILENAME
    _atomic_json_write(path, manifest)
    log.debug('Wrote manifest to %s', path)
    return path


def update_manifest_erase(
    session_dir: Path, erase_completed: bool, timing: dict[str, float] | None = None
) -> None:
    """Update erase_completed field in an existing manifest."""
    path = session_dir / MANIFEST_FILENAME
    try:
        data = json.loads(path.read_text())
        data['erase_completed'] = erase_completed
        data['erase_attempted'] = True
        if timing:
            data['timing'] = timing
        _atomic_json_write(path, data)
        log.debug('Updated manifest erase_completed=%s', erase_completed)
    except (OSError, json.JSONDecodeError) as exc:
        log.error('Failed to update manifest: %s', exc)


def list_sessions(storage_root: Path) -> list[dict]:
    """Return a list of all sessions on the Pi SD card, newest first."""
    sessions = []
    for fc_dir in sorted(storage_root.iterdir()):
        if not fc_dir.is_dir():
            continue
        for session_dir in sorted(fc_dir.iterdir(), reverse=True):
            if not session_dir.is_dir():
                continue
            manifest_path = session_dir / MANIFEST_FILENAME
            bbl_path = session_dir / RAW_FLASH_FILENAME
            if not manifest_path.exists():
                continue
            try:
                manifest = json.loads(manifest_path.read_text())
            except json.JSONDecodeError as exc:
                log.warning('Skipping corrupted manifest %s: %s', manifest_path, exc)
                continue
            sessions.append(
                {
                    'session_id': f'{fc_dir.name}/{session_dir.name}',
                    'fc_dir': fc_dir.name,
                    'session_dir': session_dir.name,
                    'path': str(session_dir),
                    'bbl_path': str(bbl_path) if bbl_path.exists() else None,
                    'manifest': manifest,
                }
            )
    return sessions


def cleanup_oldest_sessions(storage_root: Path, required_free_bytes: int) -> list[str]:
    """Delete oldest sessions until *required_free_bytes* is available.

    Returns deleted session ids in deletion order.
    """
    deleted: list[str] = []
    if required_free_bytes <= 0 or not storage_root.exists():
        return deleted

    candidates: list[tuple[str, Path]] = []
    for fc_dir in sorted(storage_root.iterdir()):
        if not fc_dir.is_dir():
            continue
        for session_dir in sorted(fc_dir.iterdir()):
            if not session_dir.is_dir():
                continue
            candidates.append((f'{fc_dir.name}/{session_dir.name}', session_dir))

    for session_id, session_dir in candidates:
        if free_bytes(storage_root) >= required_free_bytes:
            break
        shutil.rmtree(session_dir)
        if not any(session_dir.parent.iterdir()):
            session_dir.parent.rmdir()
        deleted.append(session_id)
        log.warning('Deleted old session to reclaim storage: %s', session_id)

    return deleted
