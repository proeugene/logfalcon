"""TOML configuration loading with dataclass defaults.

Uses stdlib tomllib (Python 3.11+).
"""

from __future__ import annotations

import logging
import tomllib
from dataclasses import dataclass
from pathlib import Path

log = logging.getLogger(__name__)

_DEFAULT_CONFIG_PATH = Path('/etc/bbsyncer/bbsyncer.toml')
_LOCAL_CONFIG_PATH = Path(__file__).parent.parent / 'config' / 'bbsyncer.toml'


@dataclass
class Config:
    # Serial
    serial_baud: int = 115200
    serial_port: str = ''  # empty = auto-detect /dev/ttyACM*
    serial_timeout: float = 5.0

    # Storage
    storage_path: str = '/mnt/bbsyncer-logs'
    min_free_space_mb: int = 200
    storage_pressure_cleanup: bool = True

    # Sync behaviour
    erase_after_sync: bool = True
    flash_chunk_size: int = 16384
    erase_timeout_sec: int = 120
    flash_read_compression: bool = False  # disable compression for reliability

    # LED
    led_backend: str = 'sysfs'  # "sysfs" or "gpio"
    led_gpio_pin: int = 17

    # Web server
    web_port: int = 80
    hotspot_ssid: str = 'BF-Blackbox'
    hotspot_password: str = 'fpvpilot'  # noqa: S105


def load_config(path: str | None = None) -> Config:
    """Load config from TOML file, falling back to defaults.

    Search order:
    1. `path` argument (if provided)
    2. /etc/bbsyncer/bbsyncer.toml
    3. <repo>/config/bbsyncer.toml
    4. All defaults
    """
    candidates = []
    if path:
        candidates.append(Path(path))
    candidates += [_DEFAULT_CONFIG_PATH, _LOCAL_CONFIG_PATH]

    for candidate in candidates:
        if candidate.exists():
            log.debug('Loading config from %s', candidate)
            try:
                with open(candidate, 'rb') as f:
                    data = tomllib.load(f)
                return _apply(Config(), data)
            except Exception as exc:
                log.warning('Failed to load config %s: %s', candidate, exc)

    log.debug('Using default config (no config file found)')
    return Config()


def _apply(cfg: Config, data: dict) -> Config:
    """Apply TOML data dict to Config dataclass."""
    for key, value in data.items():
        if hasattr(cfg, key):
            setattr(cfg, key, value)
        else:
            log.warning('Unknown config key: %s', key)
    return cfg
