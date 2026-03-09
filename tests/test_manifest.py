"""Tests for manifest/session helpers."""

from __future__ import annotations

import json

from bbsyncer.storage.manifest import cleanup_oldest_sessions


def _create_session(storage, fc_name: str, session_name: str) -> None:
    session_dir = storage / fc_name / session_name
    session_dir.mkdir(parents=True)
    (session_dir / 'manifest.json').write_text(json.dumps({'version': 1}))
    (session_dir / 'raw_flash.bbl').write_bytes(b'log-data')


def test_cleanup_oldest_sessions_deletes_oldest_first(tmp_path):
    _create_session(tmp_path, 'fc_BTFL_uid-aaaa', '2026-01-01_120000')
    _create_session(tmp_path, 'fc_BTFL_uid-bbbb', '2026-01-02_120000')

    deleted = cleanup_oldest_sessions(tmp_path, required_free_bytes=10**30)

    assert deleted == [
        'fc_BTFL_uid-aaaa/2026-01-01_120000',
        'fc_BTFL_uid-bbbb/2026-01-02_120000',
    ]
    assert not any(tmp_path.iterdir())
