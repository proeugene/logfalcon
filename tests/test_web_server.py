"""Tests for web server routes."""

import io
import json
from unittest.mock import patch

import pytest

from logfalcon.config import Config
from logfalcon.web import server as web_server_module
from logfalcon.web.server import (
    _CSRF_TOKEN,
    _HTTPError,
    _make_handler,
    _render_index,
    _resolve_session_path,
)


@pytest.fixture(autouse=True)
def _clear_sessions_cache():
    """Reset the module-level sessions cache between tests."""
    web_server_module._sessions_cache = (0.0, [])
    yield
    web_server_module._sessions_cache = (0.0, [])


@pytest.fixture
def storage(tmp_path):
    # Create a fake session
    fc_dir = tmp_path / 'fc_BTFL_uid-deadbeef'
    session_dir = fc_dir / '2026-02-26_143012'
    session_dir.mkdir(parents=True)
    manifest = {
        'version': 1,
        'created_utc': '2026-02-26T14:30:12Z',
        'fc': {
            'variant': 'BTFL',
            'uid': 'deadbeef12345678',
            'api_version': '1.45',
            'blackbox_device': 3,
        },
        'file': {'name': 'raw_flash.bbl', 'bytes': 1024, 'sha256': 'abc123'},
        'erase_attempted': True,
        'erase_completed': True,
    }
    (session_dir / 'manifest.json').write_text(json.dumps(manifest))
    (session_dir / 'raw_flash.bbl').write_bytes(b'\x00' * 1024)
    return tmp_path


class TestResolveSessionPath:
    def test_valid_session_id(self, tmp_path):
        path = _resolve_session_path(tmp_path, 'fc_BTFL_uid-abc/2026-01-01_120000')
        assert path == tmp_path / 'fc_BTFL_uid-abc' / '2026-01-01_120000'

    def test_rejects_path_traversal(self, tmp_path):
        with pytest.raises(_HTTPError) as exc:
            _resolve_session_path(tmp_path, '../etc/passwd')
        assert exc.value.code == 400

    def test_rejects_dotdot_in_parts(self, tmp_path):
        with pytest.raises(_HTTPError) as exc:
            _resolve_session_path(tmp_path, 'fc_dir/../../../etc')
        assert exc.value.code == 400

    def test_rejects_single_part(self, tmp_path):
        with pytest.raises(_HTTPError) as exc:
            _resolve_session_path(tmp_path, 'only_one_part')
        assert exc.value.code == 400


class TestRenderIndex:
    def test_renders_html(self, storage):
        html = _render_index(storage)
        assert 'LogFalcon' in html
        assert 'fc_BTFL_uid-deadbeef' in html
        assert 'Download .bbl' in html

    def test_empty_storage(self, tmp_path):
        html = _render_index(tmp_path)
        assert 'No sessions yet' in html
        assert 'inner OTG port' in html

    def test_nonexistent_storage(self, tmp_path):
        html = _render_index(tmp_path / 'nonexistent')
        assert 'No sessions yet' in html

    def test_settings_link_in_header(self, storage):
        html = _render_index(storage)
        assert '/settings' in html

    def test_low_space_warning(self, storage):
        cfg = Config()
        cfg.min_free_space_mb = 200
        with (
            patch('logfalcon.web.server.load_config', return_value=cfg),
            patch('logfalcon.web.server.used_and_free_gb', return_value=(1.0, 0.1)),
            patch('logfalcon.web.server.free_mb', return_value=100.0),
        ):
            html = _render_index(storage)
        assert 'Oldest sessions may be removed automatically' in html


class _FakeRequest(io.BytesIO):
    """Minimal request object for BaseHTTPRequestHandler."""

    def makefile(self, *args, **kwargs):
        return self


class _FakeWfile(io.BytesIO):
    """Writable file for capturing handler output."""

    pass


def _make_request_handler(storage_path, method, path, body=b'', headers=None):
    """Create a handler instance and invoke the given HTTP method."""
    handler_cls = _make_handler(storage_path)

    # Build raw HTTP request (headers only, without request line — parse_request
    # reads headers from rfile after raw_requestline is already consumed).
    request_line = f'{method} {path} HTTP/1.1\r\n'
    header_lines = f'Host: localhost\r\nContent-Length: {len(body)}\r\n'
    if headers:
        for k, v in headers.items():
            header_lines += f'{k}: {v}\r\n'
    headers_bytes = (header_lines + '\r\n').encode()

    wfile = _FakeWfile()

    # Suppress log output
    with patch.object(handler_cls, 'log_message', lambda *a, **kw: None):
        handler = handler_cls.__new__(handler_cls)
        handler.rfile = io.BufferedReader(io.BytesIO(headers_bytes))
        handler.wfile = wfile
        handler.client_address = ('127.0.0.1', 12345)
        handler.server = type('FakeServer', (), {'server_name': 'localhost', 'server_port': 80})()

        handler.raw_requestline = (request_line.strip() + '\r\n').encode()
        handler.parse_request()

        # Re-wrap rfile with remaining body
        handler.rfile = io.BytesIO(body)

        getattr(handler, f'do_{method}')()

    wfile.seek(0)
    return wfile.read().decode('utf-8', errors='replace')


class TestSettingsPage:
    def test_settings_page_renders(self, tmp_path):
        with patch(
            'logfalcon.web.server._read_hostapd_config',
            return_value={'ssid': 'TestNet', 'wpa_passphrase': 'secret123'},
        ):
            response = _make_request_handler(str(tmp_path), 'GET', '/settings')
        assert '200' in response.split('\r\n')[0]
        assert 'Settings' in response
        assert 'TestNet' in response
        assert _CSRF_TOKEN in response
        assert 'form' in response.lower()

    def test_settings_post_validates_ssid(self, tmp_path):
        body = f'csrf_token={_CSRF_TOKEN}&ssid=&password=validpass1'.encode()
        with patch('logfalcon.web.server._read_hostapd_config', return_value={}):
            response = _make_request_handler(
                str(tmp_path),
                'POST',
                '/settings',
                body=body,
                headers={'Content-Type': 'application/x-www-form-urlencoded'},
            )
        assert '400' in response.split('\r\n')[0]
        assert 'SSID must be' in response

    def test_settings_post_validates_password(self, tmp_path):
        body = f'csrf_token={_CSRF_TOKEN}&ssid=ValidSSID&password=short'.encode()
        with patch('logfalcon.web.server._read_hostapd_config', return_value={}):
            response = _make_request_handler(
                str(tmp_path),
                'POST',
                '/settings',
                body=body,
                headers={'Content-Type': 'application/x-www-form-urlencoded'},
            )
        assert '400' in response.split('\r\n')[0]
        assert 'Password must be' in response

    def test_settings_post_requires_csrf_token(self, tmp_path):
        body = b'ssid=ValidSSID&password=securepass123'
        with patch('logfalcon.web.server._read_hostapd_config', return_value={}):
            response = _make_request_handler(
                str(tmp_path),
                'POST',
                '/settings',
                body=body,
                headers={'Content-Type': 'application/x-www-form-urlencoded'},
            )
        assert '403' in response.split('\r\n')[0]
        assert 'Security check failed' in response

    def test_settings_post_success(self, tmp_path):
        body = f'csrf_token={_CSRF_TOKEN}&ssid=NewNetwork&password=securepass123'.encode()
        with (
            patch('logfalcon.web.server._read_hostapd_config', return_value={}),
            patch('logfalcon.web.server._write_hostapd_config', return_value=True) as mock_hostapd,
            patch('logfalcon.web.server._write_logfalcon_config', return_value=True) as mock_app,
            patch('logfalcon.web.server._write_boot_config', return_value=True) as mock_boot,
            patch(
                'logfalcon.web.server.subprocess.run',
                return_value=type('Result', (), {'returncode': 0})(),
            ) as mock_run,
        ):
            response = _make_request_handler(
                str(tmp_path),
                'POST',
                '/settings',
                body=body,
                headers={'Content-Type': 'application/x-www-form-urlencoded'},
            )
        assert '200' in response.split('\r\n')[0]
        assert 'NewNetwork' in response
        assert 'Settings saved' in response
        mock_hostapd.assert_called_once()
        mock_app.assert_called_once()
        mock_boot.assert_called_once()
        mock_run.assert_called_once()

    def test_delete_requires_csrf_header(self, storage):
        response = _make_request_handler(
            str(storage), 'DELETE', '/sessions/fc_BTFL_uid-deadbeef/2026-02-26_143012'
        )
        assert '403' in response.split('\r\n')[0]

    def test_health_endpoint(self, storage):
        cfg = Config()
        cfg.min_free_space_mb = 200
        with (
            patch(
                'logfalcon.web.server._read_hostapd_config',
                return_value={'ssid': 'TestNet', 'wpa_passphrase': 'secret123'},
            ),
            patch('logfalcon.web.server.load_config', return_value=cfg),
            patch('logfalcon.web.server.used_and_free_gb', return_value=(1.0, 0.1)),
            patch('logfalcon.web.server.free_mb', return_value=100.0),
        ):
            response = _make_request_handler(str(storage), 'GET', '/health')
        assert '200' in response.split('\r\n')[0]
        assert '"session_count": 1' in response
        assert '"low_space": true' in response


class TestStatusEndpoint:
    def test_status_returns_json(self, tmp_path):
        with patch(
            'logfalcon.web.server.get_status',
            return_value={'state': 'idle', 'progress': 0, 'message': ''},
        ):
            response = _make_request_handler(str(tmp_path), 'GET', '/status')
        assert '200' in response.split('\r\n')[0]
        assert 'application/json' in response
        body = response.split('\r\n\r\n', 1)[1]
        data = json.loads(body)
        assert 'state' in data
        assert 'progress' in data
        assert 'message' in data


class TestSSEEndpoint:
    def test_events_content_type_and_first_event(self, tmp_path):
        """SSE endpoint returns text/event-stream and sends at least one event."""
        status_calls = [0]

        def fake_get_status():
            status_calls[0] += 1
            if status_calls[0] >= 2:
                raise BrokenPipeError('simulate disconnect')
            return {'state': 'idle', 'progress': 0, 'message': ''}

        with patch('logfalcon.web.server.get_status', side_effect=fake_get_status), patch(
            'logfalcon.web.server._time.sleep', side_effect=BrokenPipeError
        ):
            response = _make_request_handler(str(tmp_path), 'GET', '/events')
        assert 'text/event-stream' in response
        assert 'data: ' in response


class TestDeleteSession:
    def test_delete_success(self, storage):
        response = _make_request_handler(
            str(storage),
            'DELETE',
            '/sessions/fc_BTFL_uid-deadbeef/2026-02-26_143012',
            headers={'X-CSRF-Token': _CSRF_TOKEN},
        )
        assert '200' in response.split('\r\n')[0]
        assert '"deleted": true' in response

    def test_delete_nonexistent_returns_404(self, storage):
        response = _make_request_handler(
            str(storage),
            'DELETE',
            '/sessions/fc_BTFL_uid-deadbeef/1999-01-01_000000',
            headers={'X-CSRF-Token': _CSRF_TOKEN},
        )
        assert '404' in response.split('\r\n')[0]

    def test_delete_unknown_path_returns_404(self, storage):
        response = _make_request_handler(
            str(storage),
            'DELETE',
            '/unknown',
            headers={'X-CSRF-Token': _CSRF_TOKEN},
        )
        assert '404' in response.split('\r\n')[0]


class TestErrorResponses:
    def test_404_returns_html(self, tmp_path):
        response = _make_request_handler(str(tmp_path), 'GET', '/nonexistent')
        assert '404' in response.split('\r\n')[0]
        assert 'text/html' in response
        assert '<h1>404</h1>' in response

    def test_post_unknown_path_returns_404(self, tmp_path):
        response = _make_request_handler(str(tmp_path), 'POST', '/unknown')
        assert '404' in response.split('\r\n')[0]
        assert 'text/html' in response


class TestRangeRequests:
    def test_valid_range_returns_206(self, storage):
        response = _make_request_handler(
            str(storage),
            'GET',
            '/download/fc_BTFL_uid-deadbeef/2026-02-26_143012/raw_flash.bbl',
            headers={'Range': 'bytes=0-99'},
        )
        assert '206' in response.split('\r\n')[0]
        assert 'Content-Range' in response
        assert 'bytes 0-99/1024' in response

    def test_malformed_range_falls_through_to_200(self, storage):
        response = _make_request_handler(
            str(storage),
            'GET',
            '/download/fc_BTFL_uid-deadbeef/2026-02-26_143012/raw_flash.bbl',
            headers={'Range': 'bytes=abc-def'},
        )
        assert '200' in response.split('\r\n')[0]

    def test_start_beyond_file_size_returns_416(self, storage):
        response = _make_request_handler(
            str(storage),
            'GET',
            '/download/fc_BTFL_uid-deadbeef/2026-02-26_143012/raw_flash.bbl',
            headers={'Range': 'bytes=9999-'},
        )
        assert '416' in response.split('\r\n')[0]

    def test_inverted_range_returns_416(self, storage):
        response = _make_request_handler(
            str(storage),
            'GET',
            '/download/fc_BTFL_uid-deadbeef/2026-02-26_143012/raw_flash.bbl',
            headers={'Range': 'bytes=500-100'},
        )
        assert '416' in response.split('\r\n')[0]
