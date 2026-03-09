"""Tests for manifest/session helpers."""

from __future__ import annotations

import json
import logging
from pathlib import Path

from logfalcon.fc.detector import FCInfo
from logfalcon.storage.manifest import (
    MANIFEST_FILENAME,
    RAW_FLASH_FILENAME,
    cleanup_oldest_sessions,
    list_sessions,
    make_session_dir,
    write_manifest,
)

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def _make_fc_info(
    uid: str = '12ab34cdef567890',
    variant: bytes = b'BTFL',
    api_major: int = 1,
    api_minor: int = 46,
    blackbox_device: int = 1,
) -> FCInfo:
    return FCInfo(
        api_major=api_major,
        api_minor=api_minor,
        variant=variant,
        uid=uid,
        blackbox_device=blackbox_device,
    )


def _create_session(storage: Path, fc_name: str, session_name: str) -> Path:
    session_dir = storage / fc_name / session_name
    session_dir.mkdir(parents=True)
    (session_dir / MANIFEST_FILENAME).write_text(json.dumps({'version': 1}))
    (session_dir / RAW_FLASH_FILENAME).write_bytes(b'log-data')
    return session_dir


# ---------------------------------------------------------------------------
# make_session_dir()
# ---------------------------------------------------------------------------

class TestMakeSessionDir:
    def test_directory_structure(self, tmp_path):
        fc = _make_fc_info(uid='aabbccdd11223344')
        session_dir = make_session_dir(tmp_path, fc)
        # Parent should contain the uid prefix
        assert 'fc_BTFL_uid-aabbccdd' in session_dir.parent.name
        # Session dir name looks like a timestamp
        assert len(session_dir.name) >= 15  # YYYY-MM-DD_HHMMSS
        assert session_dir.is_dir()

    def test_unknown_uid(self, tmp_path):
        fc = _make_fc_info(uid='unknown')
        session_dir = make_session_dir(tmp_path, fc)
        assert 'uid-unknown' in session_dir.parent.name

    def test_short_uid_uses_full(self, tmp_path):
        fc = _make_fc_info(uid='abcd')
        session_dir = make_session_dir(tmp_path, fc)
        assert 'uid-abcd' in session_dir.parent.name


# ---------------------------------------------------------------------------
# write_manifest()
# ---------------------------------------------------------------------------

class TestWriteManifest:
    def test_json_structure(self, tmp_path):
        fc = _make_fc_info()
        session_dir = tmp_path / 'session'
        session_dir.mkdir()
        path = write_manifest(session_dir, fc, sha256='abc123', used_size=4096)
        data = json.loads(path.read_text())

        assert data['version'] == 1
        assert 'created_utc' in data
        assert data['fc']['variant'] == 'BTFL'
        assert data['fc']['uid'] == fc.uid
        assert data['fc']['api_version'] == '1.46'
        assert data['fc']['blackbox_device'] == 1
        assert data['file']['name'] == RAW_FLASH_FILENAME
        assert data['file']['sha256'] == 'abc123'
        assert data['file']['bytes'] == 4096
        assert data['erase_attempted'] is False
        assert data['erase_completed'] is False

    def test_with_timing(self, tmp_path):
        fc = _make_fc_info()
        session_dir = tmp_path / 'session'
        session_dir.mkdir()
        timing = {'read_s': 12.5, 'erase_s': 3.2}
        path = write_manifest(session_dir, fc, sha256='x', used_size=0, timing=timing)
        data = json.loads(path.read_text())
        assert data['timing'] == timing

    def test_atomic_write_produces_valid_json(self, tmp_path):
        fc = _make_fc_info()
        session_dir = tmp_path / 'session'
        session_dir.mkdir()
        path = write_manifest(session_dir, fc, sha256='x', used_size=0)
        assert path.exists()
        # Must be parseable JSON
        data = json.loads(path.read_text())
        assert isinstance(data, dict)


# ---------------------------------------------------------------------------
# list_sessions()
# ---------------------------------------------------------------------------

class TestListSessions:
    def test_lists_sessions_newest_first(self, tmp_path):
        _create_session(tmp_path, 'fc_BTFL_uid-aaaa', '2026-01-01_100000')
        _create_session(tmp_path, 'fc_BTFL_uid-aaaa', '2026-01-02_100000')

        sessions = list_sessions(tmp_path)
        assert len(sessions) == 2
        # newest first within each fc_dir
        assert sessions[0]['session_dir'] == '2026-01-02_100000'
        assert sessions[1]['session_dir'] == '2026-01-01_100000'

    def test_session_has_expected_keys(self, tmp_path):
        _create_session(tmp_path, 'fc_BTFL_uid-aaaa', '2026-01-01_100000')
        sessions = list_sessions(tmp_path)
        s = sessions[0]
        assert 'session_id' in s
        assert 'fc_dir' in s
        assert 'session_dir' in s
        assert 'path' in s
        assert 'bbl_path' in s
        assert 'manifest' in s

    def test_corrupted_manifest_skipped(self, tmp_path, caplog):
        session_dir = tmp_path / 'fc_BTFL_uid-aaaa' / '2026-01-01_100000'
        session_dir.mkdir(parents=True)
        (session_dir / MANIFEST_FILENAME).write_text('{bad json!!')

        with caplog.at_level(logging.WARNING):
            sessions = list_sessions(tmp_path)

        assert sessions == []
        assert any('corrupted' in r.message.lower() or 'Skipping' in r.message for r in caplog.records)

    def test_empty_directory(self, tmp_path):
        sessions = list_sessions(tmp_path)
        assert sessions == []

    def test_bbl_path_none_when_missing(self, tmp_path):
        session_dir = tmp_path / 'fc_BTFL_uid-aaaa' / '2026-01-01_100000'
        session_dir.mkdir(parents=True)
        (session_dir / MANIFEST_FILENAME).write_text(json.dumps({'version': 1}))
        # No .bbl file
        sessions = list_sessions(tmp_path)
        assert sessions[0]['bbl_path'] is None

    def test_multiple_fc_dirs(self, tmp_path):
        _create_session(tmp_path, 'fc_BTFL_uid-aaaa', '2026-01-01_100000')
        _create_session(tmp_path, 'fc_BTFL_uid-bbbb', '2026-01-01_100000')
        sessions = list_sessions(tmp_path)
        assert len(sessions) == 2


# ---------------------------------------------------------------------------
# cleanup_oldest_sessions()
# ---------------------------------------------------------------------------

class TestCleanupOldestSessions:
    def test_deletes_oldest_first(self, tmp_path):
        _create_session(tmp_path, 'fc_BTFL_uid-aaaa', '2026-01-01_120000')
        _create_session(tmp_path, 'fc_BTFL_uid-bbbb', '2026-01-02_120000')

        deleted = cleanup_oldest_sessions(tmp_path, required_free_bytes=10**30)

        assert deleted == [
            'fc_BTFL_uid-aaaa/2026-01-01_120000',
            'fc_BTFL_uid-bbbb/2026-01-02_120000',
        ]
        assert not any(tmp_path.iterdir())

    def test_respects_limit_stops_when_enough_space(self, tmp_path):
        _create_session(tmp_path, 'fc_BTFL_uid-aaaa', '2026-01-01_120000')
        _create_session(tmp_path, 'fc_BTFL_uid-aaaa', '2026-01-02_120000')

        # Require 0 bytes — nothing should be deleted
        deleted = cleanup_oldest_sessions(tmp_path, required_free_bytes=0)
        assert deleted == []
        # Both sessions still exist
        sessions = list(tmp_path.rglob(MANIFEST_FILENAME))
        assert len(sessions) == 2

    def test_returns_empty_for_nonexistent_root(self, tmp_path):
        deleted = cleanup_oldest_sessions(tmp_path / 'nope', required_free_bytes=10**30)
        assert deleted == []

    def test_returns_empty_for_negative_required(self, tmp_path):
        _create_session(tmp_path, 'fc_BTFL_uid-aaaa', '2026-01-01_120000')
        deleted = cleanup_oldest_sessions(tmp_path, required_free_bytes=-1)
        assert deleted == []

    def test_removes_empty_fc_dir_after_last_session(self, tmp_path):
        _create_session(tmp_path, 'fc_BTFL_uid-aaaa', '2026-01-01_120000')
        cleanup_oldest_sessions(tmp_path, required_free_bytes=10**30)
        # The fc_dir itself should be gone
        assert not (tmp_path / 'fc_BTFL_uid-aaaa').exists()
