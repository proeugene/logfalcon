"""Tests for sync orchestrator logic — using mocks to avoid real hardware."""

from __future__ import annotations

import json
import tempfile
from pathlib import Path
from unittest.mock import MagicMock, patch

import pytest

from bbsyncer.config import Config
from bbsyncer.fc.detector import FCInfo
from bbsyncer.led.controller import LEDController, LEDState
from bbsyncer.msp.constants import BLACKBOX_DEVICE_FLASH
from bbsyncer.sync.orchestrator import SyncOrchestrator, SyncResult


def make_config(storage_path: str, erase_after_sync: bool = True) -> Config:
    cfg = Config()
    cfg.storage_path = storage_path
    cfg.erase_after_sync = erase_after_sync
    cfg.min_free_space_mb = 0  # disable free space check in tests
    cfg.flash_chunk_size = 8
    cfg.serial_timeout = 1.0
    return cfg


def make_led() -> LEDController:
    led = MagicMock(spec=LEDController)
    return led


def make_fc_info() -> FCInfo:
    return FCInfo(
        api_major=1,
        api_minor=42,
        variant=b'BTFL',
        uid='deadbeef12345678abcd',
        blackbox_device=BLACKBOX_DEVICE_FLASH,
    )


@pytest.fixture
def tmpdir():
    with tempfile.TemporaryDirectory() as d:
        yield d


class TestSyncOrchestratorSuccess:
    def test_successful_sync_no_erase(self, tmpdir):
        """Full sync flow with erase_after_sync=False (dry run)."""
        flash_data = b'H7\x00\x01' * 4  # 16 bytes of fake flash data

        cfg = make_config(tmpdir, erase_after_sync=False)
        led = make_led()

        with patch('bbsyncer.sync.orchestrator.MSPClient') as MockClient:
            client_instance = MockClient.return_value.__enter__.return_value

            # FC identification
            client_instance.get_api_version.return_value = (1, 42)
            client_instance.get_fc_variant.return_value = b'BTFL'
            client_instance.get_uid.return_value = 'deadbeef12345678'
            client_instance.get_blackbox_config.return_value = {'device': BLACKBOX_DEVICE_FLASH}

            # Flash summary: 16 bytes used
            client_instance.get_dataflash_summary.return_value = {
                'flags': 0x03,
                'sectors': 512,
                'total_size': 8192,
                'used_size': len(flash_data),
                'supported': True,
                'ready': True,
            }

            # Flash read: return in two 8-byte chunks (pipelined)
            chunk_calls = [
                (0, flash_data[:8]),
                (8, flash_data[8:]),
            ]
            call_idx = [0]

            def fake_receive(compression=False):
                result = chunk_calls[call_idx[0]]
                call_idx[0] = min(call_idx[0] + 1, len(chunk_calls) - 1)
                return result

            client_instance.receive_flash_read_response.side_effect = fake_receive

            orch = SyncOrchestrator(cfg, led, dry_run=False)
            result = orch.run('/dev/ttyACM0')

        assert result == SyncResult.SUCCESS

        # Verify files were created
        storage = Path(tmpdir)
        sessions = list(storage.rglob('raw_flash.bbl'))
        assert len(sessions) == 1
        bbl = sessions[0]
        assert bbl.read_bytes() == flash_data

        # Verify manifest
        manifest_path = bbl.parent / 'manifest.json'
        assert manifest_path.exists()
        manifest = json.loads(manifest_path.read_text())
        assert manifest['fc']['uid'] == 'deadbeef12345678'
        assert manifest['file']['bytes'] == len(flash_data)
        assert 'sha256' in manifest['file']
        assert manifest['erase_completed'] is False
        assert manifest['timing']['stream_sec'] >= 0
        assert manifest['timing']['verify_sec'] >= 0
        assert manifest['timing']['total_sec'] >= manifest['timing']['stream_sec']

    def test_already_empty_flash(self, tmpdir):
        """Flash with used_size=0 should return ALREADY_EMPTY."""
        cfg = make_config(tmpdir)
        led = make_led()

        with patch('bbsyncer.sync.orchestrator.MSPClient') as MockClient:
            client_instance = MockClient.return_value.__enter__.return_value
            client_instance.get_api_version.return_value = (1, 42)
            client_instance.get_fc_variant.return_value = b'BTFL'
            client_instance.get_uid.return_value = 'aabb'
            client_instance.get_blackbox_config.return_value = {'device': BLACKBOX_DEVICE_FLASH}
            client_instance.get_dataflash_summary.return_value = {
                'flags': 0x03,
                'sectors': 512,
                'total_size': 8192,
                'used_size': 0,
                'supported': True,
                'ready': True,
            }

            orch = SyncOrchestrator(cfg, led)
            result = orch.run('/dev/ttyACM0')

        assert result == SyncResult.ALREADY_EMPTY
        led.set_state.assert_called_with(LEDState.ALREADY_EMPTY)

    def test_not_betaflight(self, tmpdir):
        """Non-BTFL FC should return ERROR."""
        cfg = make_config(tmpdir)
        led = make_led()

        with patch('bbsyncer.sync.orchestrator.MSPClient') as MockClient:
            client_instance = MockClient.return_value.__enter__.return_value
            client_instance.get_api_version.return_value = (1, 42)
            client_instance.get_fc_variant.return_value = b'INAV'
            client_instance.get_uid.return_value = 'aabb'
            client_instance.get_blackbox_config.return_value = {'device': 1}

            orch = SyncOrchestrator(cfg, led)
            result = orch.run('/dev/ttyACM0')

        assert result == SyncResult.ERROR

    def test_dry_run_skips_erase(self, tmpdir):
        """dry_run=True should skip erase even if erase_after_sync=True."""
        flash_data = b'\xde\xad\xbe\xef' * 2

        cfg = make_config(tmpdir, erase_after_sync=True)
        led = make_led()

        with patch('bbsyncer.sync.orchestrator.MSPClient') as MockClient:
            client_instance = MockClient.return_value.__enter__.return_value
            client_instance.get_api_version.return_value = (1, 42)
            client_instance.get_fc_variant.return_value = b'BTFL'
            client_instance.get_uid.return_value = 'cafebabe'
            client_instance.get_blackbox_config.return_value = {'device': BLACKBOX_DEVICE_FLASH}
            client_instance.get_dataflash_summary.return_value = {
                'flags': 0x03,
                'sectors': 512,
                'total_size': 8192,
                'used_size': len(flash_data),
                'supported': True,
                'ready': True,
            }

            call_idx = [0]
            chunks = [(0, flash_data[:4]), (4, flash_data[4:])]

            def fake_receive(compression=False):
                r = chunks[call_idx[0]]
                call_idx[0] = min(call_idx[0] + 1, len(chunks) - 1)
                return r

            client_instance.receive_flash_read_response.side_effect = fake_receive

            orch = SyncOrchestrator(cfg, led, dry_run=True)
            result = orch.run('/dev/ttyACM0')

        assert result == SyncResult.DRY_RUN
        client_instance.erase_flash.assert_not_called()

    def test_auto_cleanup_reclaims_space(self, tmpdir):
        flash_data = b'H7\x00\x01' * 4
        cfg = make_config(tmpdir, erase_after_sync=False)
        cfg.min_free_space_mb = 200
        cfg.storage_pressure_cleanup = True
        led = make_led()

        with (
            patch('bbsyncer.sync.orchestrator.MSPClient') as MockClient,
            patch('bbsyncer.sync.orchestrator.free_mb', side_effect=[100.0, 250.0]),
            patch(
                'bbsyncer.sync.orchestrator.cleanup_oldest_sessions',
                return_value=['fc_BTFL_uid-old/2026-01-01_120000'],
            ) as mock_cleanup,
        ):
            client_instance = MockClient.return_value.__enter__.return_value
            client_instance.get_api_version.return_value = (1, 42)
            client_instance.get_fc_variant.return_value = b'BTFL'
            client_instance.get_uid.return_value = 'deadbeef12345678'
            client_instance.get_blackbox_config.return_value = {'device': BLACKBOX_DEVICE_FLASH}
            client_instance.get_dataflash_summary.return_value = {
                'flags': 0x03,
                'sectors': 512,
                'total_size': 8192,
                'used_size': len(flash_data),
                'supported': True,
                'ready': True,
            }
            chunks = [(0, flash_data[:8]), (8, flash_data[8:])]
            call_idx = [0]

            def fake_receive(compression=False):
                result = chunks[call_idx[0]]
                call_idx[0] = min(call_idx[0] + 1, len(chunks) - 1)
                return result

            client_instance.receive_flash_read_response.side_effect = fake_receive

            orch = SyncOrchestrator(cfg, led, dry_run=False)
            result = orch.run('/dev/ttyACM0')

        assert result == SyncResult.SUCCESS
        mock_cleanup.assert_called_once()


class TestSyncOrchestratorErrors:
    def test_sd_card_blackbox(self, tmpdir):
        cfg = make_config(tmpdir)
        led = make_led()

        with patch('bbsyncer.sync.orchestrator.MSPClient') as MockClient:
            client_instance = MockClient.return_value.__enter__.return_value
            client_instance.get_api_version.return_value = (1, 42)
            client_instance.get_fc_variant.return_value = b'BTFL'
            client_instance.get_uid.return_value = 'aabb'
            client_instance.get_blackbox_config.return_value = {'device': 2}  # SDCARD

            orch = SyncOrchestrator(cfg, led)
            result = orch.run('/dev/ttyACM0')

        assert result == SyncResult.ERROR

    def test_too_many_read_errors(self, tmpdir):
        from bbsyncer.msp.client import MSPError

        cfg = make_config(tmpdir, erase_after_sync=False)
        cfg.flash_chunk_size = 8
        led = make_led()

        with patch('bbsyncer.sync.orchestrator.MSPClient') as MockClient:
            client_instance = MockClient.return_value.__enter__.return_value
            client_instance.get_api_version.return_value = (1, 42)
            client_instance.get_fc_variant.return_value = b'BTFL'
            client_instance.get_uid.return_value = 'aabb'
            client_instance.get_blackbox_config.return_value = {'device': BLACKBOX_DEVICE_FLASH}
            client_instance.get_dataflash_summary.return_value = {
                'flags': 0x03,
                'sectors': 512,
                'total_size': 8192,
                'used_size': 64,
                'supported': True,
                'ready': True,
            }
            # Always raise MSPError on receive
            client_instance.receive_flash_read_response.side_effect = MSPError('Serial timeout')

            orch = SyncOrchestrator(cfg, led)
            result = orch.run('/dev/ttyACM0')

        assert result == SyncResult.ERROR
