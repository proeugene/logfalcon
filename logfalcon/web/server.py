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

log = logging.getLogger(__name__)

_sessions_cache: tuple[float, list] = (0.0, [])
_SESSIONS_TTL = 10.0  # seconds
_SERVER_STARTED_AT = _time.monotonic()
_CSRF_TOKEN = secrets.token_urlsafe(24)
_DEFAULT_HOTSPOT_PASSWORD = 'fpvpilot'  # noqa: S105  # nosec B105 — well-known factory default, user-changeable
_ALLOWED_DOWNLOADS = frozenset({'raw_flash.bbl', 'manifest.json'})


def _get_sessions(storage: Path) -> list:
    global _sessions_cache
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


def _render_sessions(sessions: list) -> str:
    if not sessions:
        return (
            '<div class="empty-state">'
            '<div class="icon">📭</div>'
            '<p>No sessions yet.</p>'
            '<ol>'
            '<li>Power on the Pi and give the hotspot up to 90 seconds to appear.</li>'
            '<li>Join the Wi-Fi network, then plug the FC into the Pi&apos;s inner OTG port.</li>'
            '<li>Make sure the FC is logging to SPI flash, not an FC-side SD card.</li>'
            '<li>Wait for the LED success pattern, then refresh this page.</li>'
            '</ol>'
            '</div>'
        )
    parts: list[str] = []
    current_fc: str | None = None
    for i, session in enumerate(sessions):
        fc_dir = session['fc_dir']
        if fc_dir != current_fc:
            if current_fc is not None:
                parts.append('</div></details>')
            current_fc = fc_dir
            parts.append(f'<details class="fc-group" open>\n<summary>{_e(fc_dir)}</summary>\n<div>')

        m = session.get('manifest') or {}
        fc = m.get('fc') or {}
        file_info = m.get('file') or {}
        fc_ver = fc.get('api_version', '?')
        file_size = file_info.get('bytes', 0)
        file_mb = round(file_size / 1048576, 1)
        erased = m.get('erase_completed', False)
        sha256 = file_info.get('sha256', '')
        session_id = session['session_id']
        bbl_path = session.get('bbl_path')

        erased_cls = 'erased' if erased else 'no-erase'
        erased_txt = 'Erased' if erased else 'Not erased'
        erased_title = (
            'Flash copy verified and FC erase completed.'
            if erased
            else 'The log was copied safely, but the FC flash still needs attention.'
        )
        sha_html = (
            f'<span title="{_e(sha256)}">SHA-256: {_e(sha256[:12])}…</span>' if sha256 else ''
        )
        bbl_html = (
            f'<a class="btn btn-download" href="/download/{_e(session_id)}/raw_flash.bbl">'
            f'Download .bbl</a>'
            if bbl_path
            else ''
        )
        parts.append(
            f'<div class="session-card">'
            f'<div class="session-header">'
            f'<span class="session-title">{_e(session["session_dir"].replace("_", " "))}</span>'
            f'<span class="badge {erased_cls}" title="{_e(erased_title)}">{erased_txt}</span>'
            f'</div>'
            f'<div class="session-meta">'
            f'<span>{file_mb} MB</span>'
            f'<span>API {_e(fc_ver)}</span>'
            f'{sha_html}'
            f'</div>'
            f'<div class="session-actions">'
            f'{bbl_html}'
            f'<a class="btn btn-manifest" href="/download/{_e(session_id)}/manifest.json">Manifest</a>'
            f'<button class="btn-delete" onclick="deleteSession(\'{_e(session_id)}\', this)">'
            f'Delete from Pi</button>'
            f'</div></div>'
        )

        if i == len(sessions) - 1:
            parts.append('</div></details>')

    return '\n'.join(parts)


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
    sessions_html = _render_sessions(sessions)
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
    return f"""<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>LogFalcon</title>
  <style>
    *, *::before, *::after {{ box-sizing: border-box; }}
    body {{
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
      margin: 0; padding: 0;
      background: #0f0f12;
      color: #e0e0e8;
      min-height: 100vh;
    }}
    header {{
      background: #1a1a24;
      border-bottom: 1px solid #2e2e40;
      padding: 14px 20px;
      display: flex;
      align-items: center;
      justify-content: space-between;
      position: sticky; top: 0; z-index: 100;
    }}
    header h1 {{ margin: 0; font-size: 1.1rem; font-weight: 600; }}
    #status-badge {{
      font-size: 0.75rem;
      padding: 4px 10px;
      border-radius: 12px;
      background: #2e2e40;
      color: #a0a0b8;
    }}
    #status-badge.syncing  {{ background: #1a3a5c; color: #60b0ff; }}
    #status-badge.erasing  {{ background: #3a2a10; color: #ffaa40; }}
    #status-badge.verifying {{ background: #2a1a4a; color: #c060ff; }}
    #status-badge.error    {{ background: #3a1a1a; color: #ff6060; }}
    main {{ max-width: 700px; margin: 0 auto; padding: 16px; }}
    .disk-info {{
      background: #1a1a24;
      border: 1px solid #2e2e40;
      border-radius: 8px;
      padding: 12px 16px;
      margin-bottom: 16px;
      font-size: 0.85rem;
      color: #a0a0b8;
    }}
    .disk-bar-track {{
      background: #2e2e40;
      border-radius: 4px;
      height: 6px;
      margin-top: 6px;
      overflow: hidden;
    }}
    .disk-bar-fill {{
      background: #4060d0;
      height: 100%;
      border-radius: 4px;
      transition: width 0.3s;
    }}
    .help-card {{
      background: #141c2c;
      border: 1px solid #294263;
      border-radius: 8px;
      padding: 12px 16px;
      margin-bottom: 16px;
      color: #b7d0f5;
      font-size: 0.85rem;
    }}
    .help-card strong {{ color: #ffffff; }}
    .help-card ol {{ margin: 10px 0 0 18px; padding: 0; }}
    .help-card li {{ margin-bottom: 6px; }}
    .warning-card {{
      background: #3a2a10;
      border: 1px solid #704d15;
      border-radius: 8px;
      padding: 12px 16px;
      margin-bottom: 16px;
      color: #ffca80;
      font-size: 0.85rem;
    }}
    #status-detail {{
      margin-top: 6px;
      color: #8f90a8;
      font-size: 0.8rem;
    }}
    .fc-group {{ margin-bottom: 20px; }}
    .fc-group summary {{
      cursor: pointer;
      font-size: 0.9rem;
      font-weight: 600;
      color: #c0c0d8;
      padding: 8px 0;
      list-style: none;
      display: flex;
      align-items: center;
      gap: 8px;
      border-bottom: 1px solid #2e2e40;
      user-select: none;
    }}
    .fc-group summary::before {{ content: "▶"; font-size: 0.7rem; transition: transform 0.2s; }}
    .fc-group[open] summary::before {{ transform: rotate(90deg); }}
    .session-card {{
      background: #1a1a24;
      border: 1px solid #2e2e40;
      border-radius: 8px;
      padding: 12px 14px;
      margin-top: 8px;
      display: flex;
      flex-direction: column;
      gap: 6px;
    }}
    .session-header {{
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 8px;
      flex-wrap: wrap;
    }}
    .session-title {{ font-size: 0.9rem; font-weight: 500; }}
    .session-meta {{ font-size: 0.75rem; color: #808098; display: flex; gap: 10px; flex-wrap: wrap; }}
    .badge {{
      display: inline-block;
      padding: 2px 8px;
      border-radius: 8px;
      font-size: 0.7rem;
      background: #2e2e40;
      color: #909098;
    }}
    .badge.erased {{ background: #1a3a1a; color: #60d060; }}
    .badge.no-erase {{ background: #3a2a10; color: #c08030; }}
    .session-actions {{ display: flex; gap: 8px; flex-wrap: wrap; }}
    button, a.btn {{
      display: inline-block;
      padding: 6px 14px;
      border-radius: 6px;
      font-size: 0.8rem;
      cursor: pointer;
      border: none;
      text-decoration: none;
      font-weight: 500;
      transition: opacity 0.15s;
    }}
    button:hover, a.btn:hover {{ opacity: 0.8; }}
    .btn-download {{ background: #2a4a80; color: #a0c8ff; }}
    .btn-manifest {{ background: #2e2e40; color: #a0a0b8; }}
    .btn-delete   {{ background: #4a1a1a; color: #ff8080; }}
    .empty-state {{
      text-align: center;
      padding: 48px 24px;
      color: #505068;
    }}
    .empty-state .icon {{ font-size: 3rem; margin-bottom: 12px; }}
    .empty-state ol {{
      display: inline-block;
      margin: 12px auto 0;
      padding-left: 18px;
      text-align: left;
      color: #7f8098;
    }}
    .progress-bar-track {{
      background: #2e2e40;
      border-radius: 3px;
      height: 4px;
      overflow: hidden;
      display: none;
    }}
    .progress-bar-fill {{
      background: #60b0ff;
      height: 100%;
      width: 0%;
      border-radius: 3px;
      transition: width 0.5s;
    }}
  </style>
</head>
<body>

<header>
  <h1>LogFalcon</h1>
  <a href="/settings" style="color:#a0a0b8; text-decoration:none; font-size:1.2rem;" title="Settings">&#9881;</a>
  <span id="status-badge">Idle</span>
</header>

<div id="sync-progress-container" style="background:#1a2a3a; padding:0 20px; display:none;">
  <div style="max-width:700px; margin:0 auto; padding:8px 0; font-size:0.8rem; color:#60b0ff;">
    <span id="sync-progress-label">Syncing...</span>
    <div class="progress-bar-track" id="progress-track" style="display:block; margin-top:4px;">
      <div class="progress-bar-fill" id="progress-fill"></div>
    </div>
  </div>
</div>

<main>
  <div class="disk-info">
    <span>Pi SD card: <strong>{used_gb:.1f} GB used</strong> / {free_gb:.1f} GB free</span>
    <div class="disk-bar-track">
      <div class="disk-bar-fill" style="width: {pct}%"></div>
    </div>
    <div id="status-detail">{status_message}</div>
  </div>

  <div class="help-card">
    <strong>Field quick start</strong>
    <ol>
      <li>Power on the Pi and wait up to 90 seconds for Wi-Fi to appear.</li>
      <li>Join the hotspot, then plug the FC into the Pi's inner OTG USB port.</li>
      <li>Wait for the LED success pattern before unplugging the FC.</li>
      <li>Download the <code>.bbl</code> later from this page and open it in Blackbox Explorer.</li>
    </ol>
  </div>

  {storage_warning_html}

  {sessions_html}
</main>

<script>
  // Poll sync status every 3 seconds
  function updateStatus() {{
    fetch('/status')
      .then(r => r.json())
      .then(data => {{
        const badge = document.getElementById('status-badge');
        const detail = document.getElementById('status-detail');
        const state = data.state || 'idle';
        const progress = data.progress || 0;
        const labels = {{
          idle: 'Idle', identifying: 'Identifying FC\u2026',
          querying: 'Querying flash\u2026', syncing: 'Syncing\u2026',
          verifying: 'Verifying\u2026', erasing: 'Erasing\u2026', error: 'Error'
        }};
        detail.textContent = data.message || 'Ready for the next sync.';
        badge.textContent = (labels[state] || state) +
          (state === 'syncing' && progress > 0 ? ` ${{progress}}%` : '');
        badge.className = '';
        if (['syncing','identifying','querying'].includes(state)) badge.classList.add('syncing');
        else if (state === 'erasing') badge.classList.add('erasing');
        else if (state === 'verifying') badge.classList.add('verifying');
        else if (state === 'error') badge.classList.add('error');

        const progressContainer = document.getElementById('sync-progress-container');
        const progressFill = document.getElementById('progress-fill');
        const progressLabel = document.getElementById('sync-progress-label');
        if (state === 'syncing') {{
          progressContainer.style.display = 'block';
          progressFill.style.width = progress + '%';
          progressLabel.textContent = `Syncing flash... ${{progress}}%`;
        }} else {{
          progressContainer.style.display = 'none';
        }}
      }})
      .catch(() => {{}});
  }}
  updateStatus();
  setInterval(updateStatus, 3000);

  // Delete session
  function deleteSession(sessionId, btn) {{
    if (!confirm('Delete this session from the Pi?\\n\\nMake sure you have downloaded the .bbl file first.')) return;
    btn.disabled = true;
    btn.textContent = 'Deleting\u2026';
    fetch('/sessions/' + sessionId, {{
      method: 'DELETE',
      headers: {{ 'X-CSRF-Token': '{_e(_CSRF_TOKEN)}' }}
    }})
      .then(r => r.json())
      .then(data => {{
        if (data.deleted) {{
          const card = btn.closest('.session-card');
          card.style.transition = 'opacity 0.3s';
          card.style.opacity = '0';
          setTimeout(() => {{ card.remove(); location.reload(); }}, 300);
        }} else {{
          btn.disabled = false;
          btn.textContent = 'Delete from Pi';
          alert('Delete failed.');
        }}
      }})
      .catch(() => {{
        btn.disabled = false;
        btn.textContent = 'Delete from Pi';
        alert('Delete request failed.');
      }});
  }}
</script>
</body>
</html>"""


_SETTINGS_CSS = """\
    *, *::before, *::after { box-sizing: border-box; }
    body {
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
      margin: 0; padding: 0;
      background: #0f0f12;
      color: #e0e0e8;
      min-height: 100vh;
    }
    header {
      background: #1a1a24;
      border-bottom: 1px solid #2e2e40;
      padding: 14px 20px;
      display: flex;
      align-items: center;
      justify-content: space-between;
      position: sticky; top: 0; z-index: 100;
    }
    header h1 { margin: 0; font-size: 1.1rem; font-weight: 600; }
    main { max-width: 700px; margin: 0 auto; padding: 16px; }
    .form-group { margin-bottom: 16px; }
    label { display: block; font-size: 0.85rem; color: #a0a0b8; margin-bottom: 4px; }
    input[type="text"], input[type="password"] {
      width: 100%; padding: 8px 12px;
      background: #1a1a24; border: 1px solid #2e2e40;
      border-radius: 6px; color: #e0e0e8; font-size: 0.9rem;
    }
    .btn-save {
      display: inline-block; padding: 8px 20px;
      background: #2a4a80; color: #a0c8ff;
      border: none; border-radius: 6px;
      font-size: 0.9rem; cursor: pointer; font-weight: 500;
    }
    .btn-save:hover { opacity: 0.8; }
    .back-link { color: #a0a0b8; text-decoration: none; font-size: 0.85rem; }
    .back-link:hover { color: #e0e0e8; }
    .msg-error { background: #3a1a1a; color: #ff6060; padding: 10px 14px;
      border-radius: 6px; margin-bottom: 16px; font-size: 0.85rem; }
    .msg-success { background: #1a3a1a; color: #60d060; padding: 10px 14px;
      border-radius: 6px; margin-bottom: 16px; font-size: 0.85rem; }
    .current-info { background: #1a1a24; border: 1px solid #2e2e40;
      border-radius: 8px; padding: 12px 16px; margin-bottom: 16px;
      font-size: 0.85rem; color: #a0a0b8; }
"""


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
    return f"""<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Settings — LogFalcon</title>
  <style>{_SETTINGS_CSS}</style>
</head>
<body>
<header>
  <h1>Settings</h1>
</header>
<main>
  <a class="back-link" href="/">&larr; Back</a>
  {msg_html}
  {warning_html}
  <div class="current-info">
    <strong>Current SSID:</strong> {current_ssid}<br>
    <strong>Current Password:</strong> {current_pass}
  </div>
  <form method="POST" action="/settings">
    <input type="hidden" name="csrf_token" value="{_e(_CSRF_TOKEN)}">
    <div class="form-group">
      <label for="ssid">New SSID (1–32 characters)</label>
      <input type="text" id="ssid" name="ssid" required minlength="1" maxlength="32">
    </div>
    <div class="form-group">
      <label for="password">New Password (8–63 characters)</label>
      <input type="password" id="password" name="password" required minlength="8" maxlength="63">
    </div>
    <p style="font-size:0.8rem; color:#a0a0b8;">
      Use printable characters only. Avoid copy/pasting hidden line breaks from password managers.
    </p>
    <button type="submit" class="btn-save">Save</button>
  </form>
</main>
</body>
</html>"""


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
                    self._send_json(get_status())
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
            except Exception:
                log.exception('Unhandled error in GET %s', path)
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
            except Exception:
                log.exception('Unhandled error in DELETE %s', path)
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
            except Exception:
                log.exception('Unhandled error in POST %s', path)
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

        def _send_error_response(self, code: int) -> None:
            self.send_response(code)
            self.send_header('Content-Type', 'text/plain')
            self.end_headers()
            self.wfile.write(f'{code} Error\n'.encode())

        def log_message(self, format: str, *args: object) -> None:
            log.debug('%s %s', self.address_string(), format % args)

    return _Handler


# Timestamp of the last sync activity (updated by the orchestrator via get_status)
_last_sync_activity = _time.monotonic()


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
    if cfg.idle_shutdown_minutes > 0:
        _start_idle_shutdown_timer(cfg.idle_shutdown_minutes)

    server.serve_forever()
