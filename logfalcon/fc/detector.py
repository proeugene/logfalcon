"""FC detection and identification via MSP handshake."""

from __future__ import annotations

import logging
from dataclasses import dataclass

from logfalcon.msp.client import MSPClient, MSPError
from logfalcon.msp.constants import (
    BLACKBOX_DEVICE_NONE,
    BLACKBOX_DEVICE_SDCARD,
    BTFL_VARIANT,
)

log = logging.getLogger(__name__)


@dataclass
class FCInfo:
    api_major: int
    api_minor: int
    variant: bytes  # e.g. b'BTFL'
    uid: str  # hex string, e.g. "12ab34cdef..."
    blackbox_device: int


class FCDetectionError(Exception):
    pass


class FCNotBetaflight(FCDetectionError):
    pass


class FCSDCardBlackbox(FCDetectionError):
    """FC uses SD card for blackbox — must be read directly."""

    pass


class FCBlackboxEmpty(FCDetectionError):
    """Flash is already empty — nothing to sync."""

    pass


def detect_fc(client: MSPClient) -> FCInfo:
    """Run MSP handshake, verify BTFL, return FC info.

    Raises:
        FCNotBetaflight: Variant != BTFL
        FCSDCardBlackbox: Blackbox device is SD card
        FCDetectionError: Other identification failure
    """
    try:
        major, minor = client.get_api_version()
        log.info('MSP API version: %d.%d', major, minor)
    except MSPError as exc:
        raise FCDetectionError(f'MSP API_VERSION failed: {exc}') from exc

    try:
        variant = client.get_fc_variant()
        log.info('FC variant: %r', variant)
    except MSPError as exc:
        raise FCDetectionError(f'MSP FC_VARIANT failed: {exc}') from exc

    if variant[:4] != BTFL_VARIANT:
        raise FCNotBetaflight(f'Expected BTFL variant, got {variant!r}')

    uid = 'unknown'
    try:
        uid = client.get_uid()
        log.info('FC UID: %s', uid)
    except MSPError:
        log.warning("Could not read FC UID, using 'unknown'")

    blackbox_device = BLACKBOX_DEVICE_NONE
    try:
        bb_cfg = client.get_blackbox_config()
        blackbox_device = bb_cfg.get('device', BLACKBOX_DEVICE_NONE)
        log.info('Blackbox device type: %d', blackbox_device)
    except MSPError as exc:
        log.warning('Could not read BLACKBOX_CONFIG: %s', exc)

    if blackbox_device == BLACKBOX_DEVICE_SDCARD:
        raise FCSDCardBlackbox(
            'FC uses SD card for blackbox — remove the FC SD card and read it directly'
        )

    return FCInfo(
        api_major=major,
        api_minor=minor,
        variant=variant,
        uid=uid,
        blackbox_device=blackbox_device,
    )
