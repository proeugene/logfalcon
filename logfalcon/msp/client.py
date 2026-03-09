"""Async MSP client — send/await response, high-level FC commands.

Uses asyncio + pyserial-asyncio for non-blocking serial I/O.
All public methods are async coroutines.
"""

from __future__ import annotations

import logging
import struct
import time

import serial
import serial.serialutil

from .constants import (
    DATAFLASH_COMPRESSION_HUFFMAN,
    DATAFLASH_FLAG_READY,
    DATAFLASH_FLAG_SUPPORTED,
    MSP_API_VERSION,
    MSP_BLACKBOX_CONFIG,
    MSP_DATAFLASH_ERASE,
    MSP_DATAFLASH_READ,
    MSP_DATAFLASH_SUMMARY,
    MSP_FC_VARIANT,
    MSP_UID,
)
from .framing import FrameDecoder, MSPFrame, encode_v1
from .huffman import huffman_decode

log = logging.getLogger(__name__)

_RESPONSE_TIMEOUT = 5.0  # seconds
_READ_CHUNK = 4096  # bytes to read from serial at a time


class MSPError(Exception):
    pass


class MSPTimeoutError(MSPError):
    pass


class MSPClient:
    """Synchronous (thread-friendly) MSP client wrapping pyserial.

    Designed for use from a single thread (the orchestrator). Not thread-safe.
    """

    def __init__(self, port: str, baud: int = 115200, timeout: float = _RESPONSE_TIMEOUT) -> None:
        self._port = port
        self._baud = baud
        self._timeout = timeout
        self._ser: serial.Serial | None = None
        self._decoder = FrameDecoder()
        self._pending: dict[int, MSPFrame] = {}

    def open(self) -> None:
        self._ser = serial.Serial(
            self._port,
            baudrate=self._baud,
            timeout=0.01,  # non-blocking reads
            write_timeout=2.0,
        )
        log.debug('Opened serial port %s at %d baud', self._port, self._baud)

    def close(self) -> None:
        if self._ser and self._ser.is_open:
            self._ser.close()
            log.debug('Closed serial port %s', self._port)

    def __enter__(self) -> MSPClient:
        self.open()
        return self

    def __exit__(self, *_: object) -> None:
        self.close()

    # ------------------------------------------------------------------
    # Low-level send/receive
    # ------------------------------------------------------------------

    def send(self, code: int, payload: bytes = b'') -> None:
        """Send an MSP v1 request to the FC."""
        frame = encode_v1(code, payload)
        log.debug('TX code=%d payload_len=%d', code, len(payload))
        self._ser.write(frame)

    def receive(self, code: int) -> MSPFrame:
        """Block until a response frame for *code* is received or timeout."""
        deadline = time.monotonic() + self._timeout
        while time.monotonic() < deadline:
            chunk = self._ser.read(_READ_CHUNK)
            if chunk:
                self._decoder.feed(chunk)
            # Drain newly decoded frames into pending dict (O(1) pop by code)
            if self._decoder.frames:
                frames, self._decoder.frames = self._decoder.frames, []
                for f in frames:
                    self._pending[f.code] = f
            frame = self._pending.pop(code, None)
            if frame is not None and frame.direction == ord('>'):
                log.debug('RX code=%d payload_len=%d', code, len(frame.payload))
                return frame
        raise MSPTimeoutError(f'Timeout waiting for MSP response code={code}')

    def _flush_frames(self, code: int) -> None:
        """Remove any buffered frames and pending flag for the given MSP code."""
        self._decoder.frames = [f for f in self._decoder.frames if f.code != code]
        self._pending.pop(code, None)

    def request(self, code: int, payload: bytes = b'') -> MSPFrame:
        """Send request and wait for matching response."""
        # Flush any stale frames for this code
        self._flush_frames(code)
        self.send(code, payload)
        return self.receive(code)

    # ------------------------------------------------------------------
    # High-level commands
    # ------------------------------------------------------------------

    def get_api_version(self) -> tuple[int, int]:
        """Return (major, minor) API version."""
        frame = self.request(MSP_API_VERSION)
        if len(frame.payload) < 3:
            raise MSPError('Short API_VERSION response')
        # payload: protocol_version(1) + api_major(1) + api_minor(1) + ...
        return frame.payload[1], frame.payload[2]

    def get_fc_variant(self) -> bytes:
        """Return 4-byte FC variant string, e.g. b'BTFL'."""
        frame = self.request(MSP_FC_VARIANT)
        return frame.payload[:4]

    def get_uid(self) -> str:
        """Return FC unique ID as a hex string."""
        frame = self.request(MSP_UID)
        if len(frame.payload) < 12:
            return 'unknown'
        uid_bytes = frame.payload[:12]
        return uid_bytes.hex()

    def get_blackbox_config(self) -> dict:
        """Return blackbox device type and config."""
        frame = self.request(MSP_BLACKBOX_CONFIG)
        if len(frame.payload) < 1:
            raise MSPError('Short BLACKBOX_CONFIG response')
        return {
            'device': frame.payload[0],
        }

    def get_dataflash_summary(self) -> dict:
        """Return flash summary: flags, sectors, total_size, used_size."""
        frame = self.request(MSP_DATAFLASH_SUMMARY)
        if len(frame.payload) < 13:
            raise MSPError(f'Short DATAFLASH_SUMMARY response (len={len(frame.payload)})')
        flags, sectors, total_size, used_size = struct.unpack_from('<BIII', frame.payload)
        return {
            'flags': flags,
            'sectors': sectors,
            'total_size': total_size,
            'used_size': used_size,
            'supported': bool(flags & DATAFLASH_FLAG_SUPPORTED),
            'ready': bool(flags & DATAFLASH_FLAG_READY),
        }

    def send_flash_read_request(self, address: int, size: int, compression: bool = False) -> None:
        """Send a DATAFLASH_READ request without waiting for response."""
        payload = struct.pack('<IHB', address, size, 1 if compression else 0)
        # Flush stale frames for this code
        self._flush_frames(MSP_DATAFLASH_READ)
        self.send(MSP_DATAFLASH_READ, payload)

    def _parse_flash_read_payload(self, payload: bytes) -> tuple[int, bytes]:
        """Parse a DATAFLASH_READ response payload into (address, data)."""
        if len(payload) < 7:
            raise MSPError(f'Short DATAFLASH_READ response (len={len(payload)})')

        chunk_addr, data_size, compression_type = struct.unpack_from('<IHB', payload)
        raw_data = payload[7 : 7 + data_size]

        if compression_type == DATAFLASH_COMPRESSION_HUFFMAN:
            if len(raw_data) < 2:
                raise MSPError('Compressed chunk too short for char count header')
            char_count = struct.unpack_from('<H', raw_data)[0]
            data = huffman_decode(raw_data[2:], char_count)
        else:
            data = raw_data

        return chunk_addr, data

    def receive_flash_read_response(self, compression: bool = False) -> tuple[int, bytes]:
        """Receive and decode a DATAFLASH_READ response."""
        frame = self.receive(MSP_DATAFLASH_READ)
        return self._parse_flash_read_payload(frame.payload)

    def read_flash_chunk(
        self,
        address: int,
        size: int,
        compression: bool = False,
    ) -> tuple[int, bytes]:
        """Read *size* bytes from flash starting at *address*.

        Returns (actual_address, data_bytes).
        Response format: addr(4B LE) + data_size(2B LE) + compression_type(1B) + data[data_size]
        """
        payload = struct.pack('<IHB', address, size, 1 if compression else 0)
        frame = self.request(MSP_DATAFLASH_READ, payload)
        return self._parse_flash_read_payload(frame.payload)

    def erase_flash(self) -> None:
        """Send MSP_DATAFLASH_ERASE (fire-and-forget; FC erases asynchronously)."""
        self.send(MSP_DATAFLASH_ERASE)
        log.info('Sent DATAFLASH_ERASE command')
