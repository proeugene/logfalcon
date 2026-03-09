"""stdlib http.server web server for blackbox log retrieval.

Routes:
  GET  /                          Main UI page
  GET  /settings                  Settings page (Wi-Fi config)
  POST /settings                  Save Wi-Fi settings
  GET  /sessions                  JSON: all sessions
  GET  /health                    JSON: health and support snapshot
  GET  /download/<session_id>/raw_flash.bbl
  GET  /download/<session_id>/manifest.json
  GET  /status                    JSON: current sync status
  GET  /events                    SSE: real-time sync status stream
  DELETE /sessions/<session_id>   Delete a session from Pi
  GET  /generate_204              Android captive portal probe
  GET  /hotspot-detect.html       iOS/macOS captive portal probe
  GET  /connecttest.txt           Windows captive portal probe
"""

from __future__ import annotations

import email.utils
import gzip
import html
import json
import logging
import os
import secrets
import shutil
import socketserver
import subprocess
import threading
import time as _time
from http.server import BaseHTTPRequestHandler, HTTPServer
from pathlib import Path
from urllib.parse import parse_qs

from logfalcon.config import load_config
from logfalcon.storage.manifest import list_sessions
from logfalcon.sync.orchestrator import get_status
from logfalcon.util.disk_space import free_mb, used_and_free_gb
from logfalcon.web._templates import (
    render_error,
    render_index,
    render_sessions,
    render_settings,
)

log = logging.getLogger(__name__)

_sessions_cache: tuple[float, list] = (0.0, [])
_sessions_cache_lock = threading.Lock()
_SESSIONS_TTL = 10.0  # seconds
_SERVER_STARTED_AT = _time.monotonic()
_CSRF_TOKEN = secrets.token_urlsafe(24)
_DEFAULT_HOTSPOT_PASSWORD = 'fpvpilot'  # noqa: S105  # nosec B105 — well-known factory default, user-changeable
_ALLOWED_DOWNLOADS = frozenset({'raw_flash.bbl', 'manifest.json'})


def _get_sessions(storage: Path) -> list:
    global _sessions_cache
    with _sessions_cache_lock:
        ts, data = _sessions_cache
        if _time.monotonic() - ts > _SESSIONS_TTL:
            data = list_sessions(storage) if storage.exists() else []
            _sessions_cache = (_time.monotonic(), data)
        return data


_CAPTIVE_PATHS = frozenset(
    {
        '/generate_204',
        '/gen_204',
        '/hotspot-detect.html',
        '/library/test/success.html',
        '/connecttest.txt',
        '/ncsi.txt',
    }
)

_CAPTIVE_HTML = (
    '<!DOCTYPE html><html><head>'
    '<meta http-equiv="refresh" content="0; url=/">'
    '<title>LogFalcon</title>'
    '</head><body>'
    '<p>Redirecting to <a href="/">LogFalcon</a>...</p>'
    '</body></html>'
)


_HOSTAPD_CONF = '/etc/hostapd/hostapd.conf'
_LOGFALCON_TOML = '/etc/logfalcon/logfalcon.toml'
_BOOT_CONFIG = '/boot/firmware/logfalcon-config.txt'


def _read_hostapd_config() -> dict[str, str]:
    """Read /etc/hostapd/hostapd.conf and return key=value pairs."""
    try:
        text = Path(_HOSTAPD_CONF).read_text()
        result: dict[str, str] = {}
        for line in text.splitlines():
            line = line.strip()
            if '=' in line and not line.startswith('#'):
                key, _, value = line.partition('=')
                result[key.strip()] = value.strip()
        return result
    except OSError:
        log.warning('Could not read %s', _HOSTAPD_CONF)
        return {}


def _rewrite_prefixed_lines(path: str, replacements: dict[str, str]) -> bool:
    """Replace entire lines whose stripped content starts with a known prefix."""
    try:
        text = Path(path).read_text()
        trailing_newline = text.endswith('\n')
        remaining = dict(replacements)
        updated_lines: list[str] = []
        for line in text.splitlines():
            stripped = line.lstrip()
            replacement = None
            for prefix, new_line in tuple(remaining.items()):
                if stripped.startswith(prefix):
                    indent = line[: len(line) - len(stripped)]
                    replacement = f'{indent}{new_line}'
                    remaining.pop(prefix)
                    break
            updated_lines.append(replacement if replacement is not None else line)
        updated_lines.extend(remaining.values())
        Path(path).write_text('\n'.join(updated_lines) + ('\n' if trailing_newline else ''))
        return True
    except OSError:
        log.warning('Could not update %s', path)
        return False


def _write_hostapd_config(ssid: str, password: str) -> bool:
    return _rewrite_prefixed_lines(
        _HOSTAPD_CONF,
        {
            'ssid=': f'ssid={ssid}',
            'wpa_passphrase=': f'wpa_passphrase={password}',
        },
    )


def _write_logfalcon_config(ssid: str, password: str) -> bool:
    return _rewrite_prefixed_lines(
        _LOGFALCON_TOML,
        {
            'hotspot_ssid =': f'hotspot_ssid = {json.dumps(ssid)}',
            'hotspot_password =': f'hotspot_password = {json.dumps(password)}',
        },
    )


def _write_boot_config(ssid: str, password: str) -> bool:
    return _rewrite_prefixed_lines(
        _BOOT_CONFIG,
        {
            'SSID=': f'SSID={ssid}',
            'PASSWORD=': f'PASSWORD={password}',
        },
    )


def _validate_hotspot_value(value: str, minimum: int, maximum: int, label: str) -> str | None:
    if len(value) < minimum or len(value) > maximum:
        return f'{label} must be {minimum}\u2013{maximum} characters.'
    if any(ord(ch) < 32 or ord(ch) == 127 for ch in value):
        return f'{label} must use printable characters only.'
    return None


def _health_payload(storage: Path) -> dict[str, object]:
    status = get_status()
    cfg = load_config()
    try:
        used_gb, free_gb = used_and_free_gb(storage)
        free_storage_mb = round(free_mb(storage), 1)
    except OSError:
        used_gb, free_gb = 0.0, 0.0
        free_storage_mb = 0.0
    hostapd = _read_hostapd_config()
    return {
        'ok': status.get('state') != 'error',
        'uptime_sec': int(_time.monotonic() - _SERVER_STARTED_AT),
        'status': status,
        'session_count': len(_get_sessions(storage)),
        'storage': {
            'used_gb': round(used_gb, 2),
            'free_gb': round(free_gb, 2),
            'free_mb': free_storage_mb,
            'reserve_mb': cfg.min_free_space_mb,
            'storage_pressure_cleanup': cfg.storage_pressure_cleanup,
            'low_space': free_storage_mb < cfg.min_free_space_mb,
        },
        'hotspot': {
            'ssid': hostapd.get('ssid', ''),
            'default_password_in_use': (
                hostapd.get('wpa_passphrase', '') == _DEFAULT_HOTSPOT_PASSWORD
            ),
        },
    }


class _HTTPError(Exception):
    def __init__(self, code: int) -> None:
        self.code = code


class _ThreadedHTTPServer(socketserver.ThreadingMixIn, HTTPServer):
    daemon_threads = True


def _e(s: object) -> str:
    """HTML-escape a value for safe inline embedding."""
    return html.escape(str(s))


def _render_index(storage: Path) -> str:
    sessions = _get_sessions(storage)
    status = get_status()
    cfg = load_config()
    try:
        used_gb, free_gb = used_and_free_gb(storage)
        free_storage_mb = free_mb(storage)
    except OSError:
        used_gb, free_gb = 0.0, 0.0
        free_storage_mb = 0.0
    total_gb = used_gb + free_gb
    pct = int(used_gb / total_gb * 100) if total_gb > 0 else 0
    sessions_html = render_sessions(sessions)
    status_message = _e(status.get('message', 'Ready for the next sync.'))
    storage_warning_html = ''
    if free_storage_mb < cfg.min_free_space_mb:
        storage_warning_html = (
            '<div class="warning-card">'
            f'Low space: only {_e(round(free_storage_mb, 1))} MB free. '
            'Oldest sessions may be removed automatically during the next sync '
            f'to stay above the {_e(cfg.min_free_space_mb)} MB reserve.'
            '</div>'
        )
    return render_index(
        used_gb=used_gb,
        free_gb=free_gb,
        pct=pct,
        sessions_html=sessions_html,
        status_message=status_message,
        storage_warning_html=storage_warning_html,
        csrf_token=_e(_CSRF_TOKEN),
    )

def _render_settings(message: str = '', error: bool = False) -> str:
    hostapd = _read_hostapd_config()
    current_ssid = _e(hostapd.get('ssid', 'Unknown'))
    current_pass = _e(hostapd.get('wpa_passphrase', 'Unknown'))
    using_default_password = hostapd.get('wpa_passphrase', '') == _DEFAULT_HOTSPOT_PASSWORD
    msg_html = ''
    if message:
        cls = 'msg-error' if error else 'msg-success'
        msg_html = f'<div class="{cls}">{_e(message)}</div>'
    warning_html = ''
    if using_default_password:
        warning_html = (
            '<div class="msg-error">'
            'This Pi is still using the launch-default hotspot password. '
            'Change it before flying at a shared field.'
            '</div>'
        )
    return render_settings(
        current_ssid=current_ssid,
        current_pass=current_pass,
        msg_html=msg_html,
        warning_html=warning_html,
        csrf_token=_e(_CSRF_TOKEN),
    )

def _resolve_session_path(storage: Path, session_id: str) -> Path:
    """Safely resolve a session_id like 'fc_BTFL_uid-abc/2026-02-26_143012'."""
    parts = session_id.split('/')
    if len(parts) != 2:
        raise _HTTPError(400)
    fc_dir, session_dir = parts
    if '..' in fc_dir or '..' in session_dir:
        raise _HTTPError(400)
    try:
        path = storage / fc_dir / session_dir
        path.resolve().relative_to(storage.resolve())
    except ValueError:
        raise _HTTPError(400) from None
    return path


def _resolve_session_file(storage: Path, session_id: str, filename: str) -> Path:
    if filename not in _ALLOWED_DOWNLOADS:
        raise _HTTPError(400)
    session_path = _resolve_session_path(storage, session_id)
    file_path = session_path / filename
    if not file_path.exists():
        raise _HTTPError(404)
    return file_path


def _make_handler(storage_path: str) -> type:
    storage = Path(storage_path)

    class _Handler(BaseHTTPRequestHandler):
        def do_GET(self) -> None:
            path = self.path.split('?')[0]
            try:
                if path in _CAPTIVE_PATHS:
                    self._send_html(_CAPTIVE_HTML)
                elif path == '/':
                    self._send_html(_render_index(storage))
                elif path == '/sessions':
                    self._send_json(_get_sessions(storage))
                elif path == '/status':
                    payload = get_status()
                    payload.update(_idle_shutdown_info())
                    self._send_json(payload)
                elif path == '/events':
                    self._handle_sse()
                elif path == '/health':
                    self._send_json(_health_payload(storage))
                elif path.startswith('/download/'):
                    self._handle_download(path[len('/download/') :])
                elif path == '/settings':
                    self._send_html(_render_settings())
                else:
                    self._send_error_response(404)
            except _HTTPError as exc:
                self._send_error_response(exc.code)
            except (OSError, ValueError, KeyError):
                log.exception('Unhandled error in %s %s', 'GET', path)
                self._send_error_response(500)

        def do_DELETE(self) -> None:
            path = self.path.split('?')[0]
            try:
                if path.startswith('/sessions/'):
                    self._require_csrf_header()
                    self._handle_delete_session(path[len('/sessions/') :])
                else:
                    self._send_error_response(404)
            except _HTTPError as exc:
                self._send_error_response(exc.code)
            except (OSError, ValueError, KeyError):
                log.exception('Unhandled error in %s %s', 'DELETE', path)
                self._send_error_response(500)

        def do_POST(self) -> None:
            path = self.path.split('?')[0]
            try:
                if path == '/settings':
                    self._handle_settings_post()
                else:
                    self._send_error_response(404)
            except _HTTPError as exc:
                self._send_error_response(exc.code)
            except (OSError, ValueError, KeyError):
                log.exception('Unhandled error in %s %s', 'POST', path)
                self._send_error_response(500)

        def _handle_settings_post(self) -> None:
            content_length = int(self.headers.get('Content-Length', 0))
            body = self.rfile.read(content_length).decode('utf-8')
            params = parse_qs(body)
            csrf_token = params.get('csrf_token', [''])[0]
            ssid = params.get('ssid', [''])[0].strip()
            password = params.get('password', [''])[0].strip()
            if csrf_token != _CSRF_TOKEN:
                self._send_html(
                    _render_settings('Security check failed. Reload and try again.', error=True),
                    status=403,
                )
                return

            ssid_error = _validate_hotspot_value(ssid, 1, 32, 'SSID')
            if ssid_error:
                self._send_html(_render_settings(ssid_error, error=True), status=400)
                return
            password_error = _validate_hotspot_value(password, 8, 63, 'Password')
            if password_error:
                self._send_html(_render_settings(password_error, error=True), status=400)
                return

            if not all(
                (
                    _write_hostapd_config(ssid, password),
                    _write_logfalcon_config(ssid, password),
                    _write_boot_config(ssid, password),
                )
            ):
                self._send_html(
                    _render_settings(
                        'Could not save every hotspot setting cleanly. Review the Pi before flying.',
                        error=True,
                    ),
                    status=500,
                )
                return

            try:
                result = subprocess.run(
                    ['systemctl', 'restart', 'hostapd'],
                    capture_output=True,
                    check=False,
                    timeout=10,
                )
            except (OSError, subprocess.TimeoutExpired):
                log.warning('Could not restart hostapd')
                self._send_html(
                    _render_settings(
                        'Settings were saved, but hostapd could not restart cleanly. Reboot the Pi before flying.',
                        error=True,
                    ),
                    status=500,
                )
                return
            if result.returncode != 0:
                log.warning('hostapd restart failed with code %s', result.returncode)
                self._send_html(
                    _render_settings(
                        'Settings were saved, but Wi-Fi restart failed. Reboot the Pi before flying.',
                        error=True,
                    ),
                    status=500,
                )
                return

            msg = (
                f'Settings saved! Wi-Fi hotspot is now: {ssid}. '
                'You may need to reconnect to the new network.'
            )
            log.info('Hotspot settings updated from %s to SSID=%s', self.client_address[0], ssid)
            self._send_html(_render_settings(msg))

        def _handle_sse(self) -> None:
            """Server-Sent Events stream — pushes sync status every 2 seconds."""
            self.send_response(200)
            self.send_header('Content-Type', 'text/event-stream')
            self.send_header('Cache-Control', 'no-cache')
            self.send_header('Connection', 'keep-alive')
            self.send_header('X-Accel-Buffering', 'no')
            self.end_headers()
            prev = None
            try:
                while True:
                    status = get_status()
                    payload = json.dumps(status)
                    if payload != prev:
                        self.wfile.write(f'data: {payload}\n\n'.encode())
                        self.wfile.flush()
                        prev = payload
                    _time.sleep(2)
            except (BrokenPipeError, ConnectionResetError, OSError):
                pass  # Client disconnected

        def _handle_download(self, sub_path: str) -> None:
            # sub_path is "<session_id>/<filename>"
            if sub_path.endswith('/raw_flash.bbl'):
                session_id = sub_path[: -len('/raw_flash.bbl')]
                file_path = _resolve_session_file(storage, session_id, 'raw_flash.bbl')
                self._send_file(file_path, 'raw_flash.bbl')
            elif sub_path.endswith('/manifest.json'):
                session_id = sub_path[: -len('/manifest.json')]
                file_path = _resolve_session_file(storage, session_id, 'manifest.json')
                self._send_file(file_path, 'manifest.json')
            else:
                raise _HTTPError(404)

        def _handle_delete_session(self, session_id: str) -> None:
            session_path = _resolve_session_path(storage, session_id)
            if not session_path.exists():
                raise _HTTPError(404)
            shutil.rmtree(session_path)
            global _sessions_cache
            with _sessions_cache_lock:
                _sessions_cache = (0.0, [])  # invalidate
            log.info('Deleted session from %s: %s', self.client_address[0], session_path)
            self._send_json({'deleted': True, 'session_id': session_id})

        def _require_csrf_header(self) -> None:
            if self.headers.get('X-CSRF-Token', '') != _CSRF_TOKEN:
                raise _HTTPError(403)

        def _send_body(self, body: bytes, content_type: str, status: int = 200) -> None:
            accept_enc = self.headers.get('Accept-Encoding', '')
            if 'gzip' in accept_enc:
                body = gzip.compress(body)
                self.send_response(status)
                self.send_header('Content-Type', content_type)
                self.send_header('Content-Encoding', 'gzip')
                self.send_header('Content-Length', str(len(body)))
                self.end_headers()
                self.wfile.write(body)
            else:
                self.send_response(status)
                self.send_header('Content-Type', content_type)
                self.send_header('Content-Length', str(len(body)))
                self.end_headers()
                self.wfile.write(body)

        def _send_html(self, body: str, status: int = 200) -> None:
            self._send_body(body.encode(), 'text/html; charset=utf-8', status)

        def _send_json(self, data: object, status: int = 200) -> None:
            self._send_body(json.dumps(data).encode(), 'application/json', status)

        def _stream_file_range(self, f, wfile, offset: int, length: int) -> None:
            if hasattr(os, 'sendfile'):
                remaining = length
                while remaining > 0:
                    sent = os.sendfile(
                        wfile.fileno(),
                        f.fileno(),
                        offset,
                        min(1 << 20, remaining),
                    )
                    if sent == 0:
                        break
                    offset += sent
                    remaining -= sent
            else:
                f.seek(offset)
                remaining = length
                while remaining > 0:
                    chunk = f.read(min(1 << 20, remaining))
                    if not chunk:
                        break
                    wfile.write(chunk)
                    remaining -= len(chunk)

        def _send_file(self, path: Path, filename: str) -> None:
            st = path.stat()
            size = st.st_size
            mtime = st.st_mtime
            last_modified = email.utils.formatdate(mtime, usegmt=True)
            etag = f'"{int(mtime)}-{size}"'

            # Check If-None-Match
            if_none_match = self.headers.get('If-None-Match')
            if if_none_match and if_none_match == etag:
                self.send_response(304)
                self.end_headers()
                return

            range_header = self.headers.get('Range')
            if range_header and range_header.startswith('bytes='):
                try:
                    range_spec = range_header[6:]
                    start_str, end_str = range_spec.split('-', 1)
                    start = int(start_str) if start_str else 0
                    end = int(end_str) if end_str else size - 1
                    end = min(end, size - 1)
                    if start > end or start >= size:
                        self.send_response(416)
                        self.send_header('Content-Range', f'bytes */{size}')
                        self.end_headers()
                        return
                    content_length = end - start + 1
                    self.send_response(206)
                    self.send_header('Content-Type', 'application/octet-stream')
                    self.send_header('Content-Disposition', f'attachment; filename="{filename}"')
                    self.send_header('Content-Length', str(content_length))
                    self.send_header('Content-Range', f'bytes {start}-{end}/{size}')
                    self.send_header('Accept-Ranges', 'bytes')
                    self.send_header('Last-Modified', last_modified)
                    self.send_header('ETag', etag)
                    self.end_headers()
                    with open(path, 'rb') as f:
                        self._stream_file_range(f, self.wfile, start, content_length)
                    return
                except (ValueError, IndexError):
                    pass  # Fall through to full response

            self.send_response(200)
            self.send_header('Content-Type', 'application/octet-stream')
            self.send_header('Content-Disposition', f'attachment; filename="{filename}"')
            self.send_header('Content-Length', str(size))
            self.send_header('Accept-Ranges', 'bytes')
            self.send_header('Last-Modified', last_modified)
            self.send_header('ETag', etag)
            self.end_headers()
            with open(path, 'rb') as f:
                self._stream_file_range(f, self.wfile, 0, size)

        def _send_error_response(self, code: int, message: str = '') -> None:
            self.send_response(code)
            self.send_header('Content-Type', 'text/html; charset=utf-8')
            self.end_headers()
            reason = message or self.responses.get(code, ('Error',))[0]
            body = render_error(code, reason)
            self.wfile.write(body.encode())

        def log_message(self, format: str, *args: object) -> None:
            log.debug('%s %s', self.address_string(), format % args)

    return _Handler


# Timestamp of the last sync activity (updated by the orchestrator via get_status)
_last_sync_activity = _time.monotonic()
_idle_shutdown_minutes: int = 0  # set by run_server when configured


def _idle_shutdown_info() -> dict:
    """Return idle-shutdown fields for the /status payload."""
    if _idle_shutdown_minutes <= 0:
        return {'idle_shutdown_minutes': 0, 'idle_shutdown_remaining_sec': 0}
    elapsed = _time.monotonic() - _last_sync_activity
    remaining = max(0, _idle_shutdown_minutes * 60 - elapsed)
    return {
        'idle_shutdown_minutes': _idle_shutdown_minutes,
        'idle_shutdown_remaining_sec': int(remaining),
    }


def _start_idle_shutdown_timer(idle_minutes: int) -> None:
    """Background thread that shuts down the Pi after idle_minutes of no sync."""
    timeout_sec = idle_minutes * 60
    log.info('Idle auto-shutdown enabled: %d minutes', idle_minutes)

    def _monitor() -> None:
        global _last_sync_activity  # noqa: PLW0603
        _last_sync_activity = _time.monotonic()
        while True:
            _time.sleep(60)
            status = get_status()
            if status.get('state') not in ('idle', ''):
                # Sync is active — reset the timer
                _last_sync_activity = _time.monotonic()
                continue
            elapsed = _time.monotonic() - _last_sync_activity
            if elapsed >= timeout_sec:
                log.warning(
                    'No sync activity for %d minutes — shutting down', idle_minutes
                )
                subprocess.run(  # noqa: S603
                    ['/usr/bin/sudo', '/sbin/shutdown', '-h', 'now'],
                    check=False,
                )
                return

    t = threading.Thread(target=_monitor, daemon=True, name='idle-shutdown')
    t.start()


def run_server(storage_path: str = '/mnt/logfalcon-logs', port: int = 80) -> None:
    """Start the HTTP server."""
    handler = _make_handler(storage_path)
    server = _ThreadedHTTPServer(('0.0.0.0', port), handler)
    log.info('Starting web server on 0.0.0.0:%d', port)

    cfg = load_config()
    global _idle_shutdown_minutes  # noqa: PLW0603
    _idle_shutdown_minutes = cfg.idle_shutdown_minutes
    if cfg.idle_shutdown_minutes > 0:
        _start_idle_shutdown_timer(cfg.idle_shutdown_minutes)

    server.serve_forever()
