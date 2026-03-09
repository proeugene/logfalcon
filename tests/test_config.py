"""Tests for TOML configuration loading."""

from bbsyncer.config import Config, _apply, load_config


class TestLoadConfig:
    def test_default_config_no_file(self):
        cfg = load_config('/nonexistent/path/bbsyncer.toml')
        assert isinstance(cfg, Config)
        assert cfg.serial_baud == 115200
        assert cfg.storage_path == '/mnt/bbsyncer-logs'
        assert cfg.erase_after_sync is True
        assert cfg.storage_pressure_cleanup is True

    def test_load_from_file(self, tmp_path):
        toml_file = tmp_path / 'test.toml'
        toml_file.write_text(
            'serial_baud = 230400\nstorage_path = "/tmp/test-logs"\nerase_after_sync = false\nstorage_pressure_cleanup = false\n'
        )
        cfg = load_config(str(toml_file))
        assert cfg.serial_baud == 230400
        assert cfg.storage_path == '/tmp/test-logs'
        assert cfg.erase_after_sync is False
        assert cfg.storage_pressure_cleanup is False

    def test_partial_config_keeps_defaults(self, tmp_path):
        toml_file = tmp_path / 'partial.toml'
        toml_file.write_text('hotspot_ssid = "MyDrone"\n')
        cfg = load_config(str(toml_file))
        assert cfg.hotspot_ssid == 'MyDrone'
        assert cfg.serial_baud == 115200  # default preserved

    def test_unknown_keys_ignored(self, tmp_path):
        toml_file = tmp_path / 'unknown.toml'
        toml_file.write_text('unknown_key = "value"\nserial_baud = 9600\n')
        cfg = load_config(str(toml_file))
        assert cfg.serial_baud == 9600
        assert not hasattr(cfg, 'unknown_key')

    def test_invalid_toml_falls_back(self, tmp_path):
        toml_file = tmp_path / 'bad.toml'
        toml_file.write_bytes(b'\x00\x01\x02')  # invalid TOML
        cfg = load_config(str(toml_file))
        # Should fall back to defaults
        assert cfg.serial_baud == 115200


class TestApply:
    def test_apply_sets_fields(self):
        cfg = Config()
        _apply(cfg, {'web_port': 8080, 'min_free_space_mb': 500})
        assert cfg.web_port == 8080
        assert cfg.min_free_space_mb == 500

    def test_apply_returns_same_object(self):
        cfg = Config()
        result = _apply(cfg, {'web_port': 9090})
        assert result is cfg
