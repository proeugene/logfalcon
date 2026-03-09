"""Tests for MSPClient — low-level protocol and high-level FC commands."""

from __future__ import annotations

import struct
from unittest.mock import MagicMock, patch

import pytest

from logfalcon.msp.client import MSPClient, MSPError, MSPTimeoutError
from logfalcon.msp.constants import (
    MSP_API_VERSION,
    MSP_DATAFLASH_READ,
    MSP_DATAFLASH_SUMMARY,
    MSP_FC_VARIANT,
    MSP_UID,
)
from logfalcon.msp.crc import crc8_dvb_s2, crc8_xor
from logfalcon.msp.framing import MSPFrame, encode_v1


# ---------------------------------------------------------------------------
# Helpers: build raw response bytes that the FrameDecoder will accept
# ---------------------------------------------------------------------------

def _build_v1_response(code: int, payload: bytes = b'') -> bytes:
    """Build a valid MSP v1 response frame ($M> direction)."""
    size = len(payload)
    checksum = crc8_xor(bytes([size, code]) + payload)
    return b'$M>' + bytes([size, code]) + payload + bytes([checksum])


def _build_v2_response(code: int, payload: bytes = b'') -> bytes:
    """Build a valid MSP v2 response frame ($X> direction)."""
    size = len(payload)
    header = bytes([
        0,  # flag
        code & 0xFF,
        (code >> 8) & 0xFF,
        size & 0xFF,
        (size >> 8) & 0xFF,
    ])
    crc = crc8_dvb_s2(header + payload)
    return b'$X>' + header + payload + bytes([crc])


# ---------------------------------------------------------------------------
# Fixtures
# ---------------------------------------------------------------------------

@pytest.fixture
def mock_serial():
    """Return a MagicMock pretending to be serial.Serial."""
    ser = MagicMock()
    ser.is_open = True
    return ser


@pytest.fixture
def client(mock_serial):
    """Return an MSPClient with mock serial already wired up."""
    with patch('logfalcon.msp.client.serial.Serial', return_value=mock_serial):
        c = MSPClient('/dev/ttyTEST', timeout=1.0)
        c.open()
    return c


# ---------------------------------------------------------------------------
# 1. _flush_frames
# ---------------------------------------------------------------------------

class TestFlushFrames:
    def test_removes_matching_frames_and_pending(self, client):
        frame_a = MSPFrame(version=1, direction=ord('>'), code=1, payload=b'\x00')
        frame_b = MSPFrame(version=1, direction=ord('>'), code=2, payload=b'\x01')
        client._decoder.frames = [frame_a, frame_b]
        client._pending[1] = frame_a

        client._flush_frames(1)

        assert all(f.code != 1 for f in client._decoder.frames)
        assert 1 not in client._pending
        # code=2 frame is untouched
        assert any(f.code == 2 for f in client._decoder.frames)

    def test_noop_when_nothing_buffered(self, client):
        client._flush_frames(99)  # should not raise


# ---------------------------------------------------------------------------
# 2. request() — v1 and v2 framing
# ---------------------------------------------------------------------------

class TestRequest:
    def test_v1_request_response(self, client, mock_serial):
        payload = b'\x07\x01\x2a'  # protocol=7, major=1, minor=42
        response_bytes = _build_v1_response(MSP_API_VERSION, payload)

        mock_serial.read.side_effect = [response_bytes, b'']

        frame = client.request(MSP_API_VERSION)

        assert frame.code == MSP_API_VERSION
        assert frame.payload == payload
        assert frame.version == 1
        mock_serial.write.assert_called_once()

    def test_v2_request_response(self, client, mock_serial):
        payload = b'\x07\x01\x2a'
        response_bytes = _build_v2_response(MSP_API_VERSION, payload)

        mock_serial.read.side_effect = [response_bytes, b'']

        frame = client.request(MSP_API_VERSION)

        assert frame.code == MSP_API_VERSION
        assert frame.payload == payload
        assert frame.version == 2


# ---------------------------------------------------------------------------
# 3. request() timeout
# ---------------------------------------------------------------------------

class TestRequestTimeout:
    def test_raises_timeout_when_no_response(self, client, mock_serial):
        mock_serial.read.return_value = b''

        with pytest.raises(MSPTimeoutError, match='Timeout'):
            client.request(MSP_API_VERSION)


# ---------------------------------------------------------------------------
# 4. get_api_version
# ---------------------------------------------------------------------------

class TestGetApiVersion:
    def test_parses_version_tuple(self, client, mock_serial):
        payload = b'\x00\x01\x2e'  # protocol=0, major=1, minor=46
        mock_serial.read.side_effect = [_build_v1_response(MSP_API_VERSION, payload), b'']

        major, minor = client.get_api_version()

        assert major == 1
        assert minor == 46

    def test_short_payload_raises(self, client, mock_serial):
        payload = b'\x00\x01'  # only 2 bytes — too short
        mock_serial.read.side_effect = [_build_v1_response(MSP_API_VERSION, payload), b'']

        with pytest.raises(MSPError, match='Short'):
            client.get_api_version()


# ---------------------------------------------------------------------------
# 5. get_fc_variant
# ---------------------------------------------------------------------------

class TestGetFCVariant:
    def test_returns_4_byte_variant(self, client, mock_serial):
        payload = b'BTFL'
        mock_serial.read.side_effect = [_build_v1_response(MSP_FC_VARIANT, payload), b'']

        variant = client.get_fc_variant()

        assert variant == b'BTFL'
        assert len(variant) == 4


# ---------------------------------------------------------------------------
# 6. get_uid
# ---------------------------------------------------------------------------

class TestGetUID:
    def test_returns_hex_string(self, client, mock_serial):
        uid_bytes = bytes(range(12))  # 00010203...0b
        mock_serial.read.side_effect = [_build_v1_response(MSP_UID, uid_bytes), b'']

        uid = client.get_uid()

        assert uid == uid_bytes.hex()
        assert len(uid) == 24

    def test_short_uid_returns_unknown(self, client, mock_serial):
        mock_serial.read.side_effect = [_build_v1_response(MSP_UID, b'\x01\x02'), b'']

        assert client.get_uid() == 'unknown'


# ---------------------------------------------------------------------------
# 7. get_dataflash_summary
# ---------------------------------------------------------------------------

class TestGetDataflashSummary:
    def test_parses_summary_dict(self, client, mock_serial):
        flags = 0x03
        sectors = 512
        total_size = 0x200000
        used_size = 0x100000
        payload = struct.pack('<BIII', flags, sectors, total_size, used_size)

        mock_serial.read.side_effect = [
            _build_v1_response(MSP_DATAFLASH_SUMMARY, payload),
            b'',
        ]

        summary = client.get_dataflash_summary()

        assert summary['flags'] == 0x03
        assert summary['sectors'] == 512
        assert summary['total_size'] == 0x200000
        assert summary['used_size'] == 0x100000
        assert summary['supported'] is True
        assert summary['ready'] is True

    def test_short_payload_raises(self, client, mock_serial):
        payload = b'\x03\x00\x02'  # too short
        mock_serial.read.side_effect = [
            _build_v1_response(MSP_DATAFLASH_SUMMARY, payload),
            b'',
        ]

        with pytest.raises(MSPError, match='Short'):
            client.get_dataflash_summary()


# ---------------------------------------------------------------------------
# 8. send_flash_read_request
# ---------------------------------------------------------------------------

class TestSendFlashReadRequest:
    def test_encodes_correct_frame(self, client, mock_serial):
        address = 0x1000
        size = 4096

        client.send_flash_read_request(address, size, compression=False)

        expected_payload = struct.pack('<IHB', address, size, 0)
        expected_frame = encode_v1(MSP_DATAFLASH_READ, expected_payload)
        mock_serial.write.assert_called_once_with(expected_frame)

    def test_compression_flag(self, client, mock_serial):
        client.send_flash_read_request(0, 256, compression=True)

        expected_payload = struct.pack('<IHB', 0, 256, 1)
        expected_frame = encode_v1(MSP_DATAFLASH_READ, expected_payload)
        mock_serial.write.assert_called_once_with(expected_frame)


# ---------------------------------------------------------------------------
# 9. receive_flash_read_response
# ---------------------------------------------------------------------------

class TestReceiveFlashReadResponse:
    def test_parses_address_and_data(self, client, mock_serial):
        address = 0x2000
        data = b'\xde\xad\xbe\xef'
        data_size = len(data)
        compression_type = 0
        payload = struct.pack('<IHB', address, data_size, compression_type) + data

        mock_serial.read.side_effect = [
            _build_v1_response(MSP_DATAFLASH_READ, payload),
            b'',
        ]

        addr, result_data = client.receive_flash_read_response()

        assert addr == address
        assert result_data == data

    def test_short_response_raises(self, client, mock_serial):
        payload = b'\x00\x01\x02'  # only 3 bytes — too short
        mock_serial.read.side_effect = [
            _build_v1_response(MSP_DATAFLASH_READ, payload),
            b'',
        ]

        with pytest.raises(MSPError, match='Short'):
            client.receive_flash_read_response()


# ---------------------------------------------------------------------------
# 10. Corrupted frame handling
# ---------------------------------------------------------------------------

class TestCorruptedFrames:
    def test_bad_v1_crc_is_silently_dropped(self, client, mock_serial):
        """A frame with wrong checksum should be dropped, leading to timeout."""
        good_payload = b'\x00\x01\x2e'
        raw = bytearray(_build_v1_response(MSP_API_VERSION, good_payload))
        raw[-1] ^= 0xFF  # corrupt checksum

        mock_serial.read.return_value = bytes(raw)

        with pytest.raises(MSPTimeoutError):
            client.request(MSP_API_VERSION)

    def test_bad_v2_crc_is_silently_dropped(self, client, mock_serial):
        """A v2 frame with wrong CRC should be dropped, leading to timeout."""
        raw = bytearray(_build_v2_response(MSP_API_VERSION, b'\x00\x01\x2e'))
        raw[-1] ^= 0xFF  # corrupt CRC

        mock_serial.read.return_value = bytes(raw)

        with pytest.raises(MSPTimeoutError):
            client.request(MSP_API_VERSION)

    def test_garbage_then_valid_frame(self, client, mock_serial):
        """Random garbage before a valid frame should not prevent decoding."""
        garbage = b'\xff\xfe\xfd\x00\x01\x02'  # random junk bytes
        valid = _build_v1_response(MSP_API_VERSION, b'\x00\x01\x2e')

        mock_serial.read.side_effect = [garbage + valid, b'']

        frame = client.request(MSP_API_VERSION)
        assert frame.code == MSP_API_VERSION
        assert frame.payload == b'\x00\x01\x2e'
