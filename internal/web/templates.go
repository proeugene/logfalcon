// Package web provides the LogFalcon HTTP server and HTML templates.
package web

import (
	"fmt"
	"html"
	"strings"

	"github.com/proeugene/logfalcon/internal/storage"
)

// IndexParams holds values for the main dashboard page.
type IndexParams struct {
	UsedGB, FreeGB     float64
	Pct                int
	SessionsHTML       string
	StatusMessage      string
	StorageWarningHTML string
	CSRFToken          string
}

// SettingsParams holds values for the settings page.
type SettingsParams struct {
	CurrentSSID string
	CurrentPass string
	MsgHTML     string
	WarningHTML string
	CSRFToken   string
}

func esc(s string) string { return html.EscapeString(s) }

// RenderIndex renders the main dashboard page.
func RenderIndex(params IndexParams) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>LogFalcon</title>
  <style>
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
    #status-badge {
      font-size: 0.75rem;
      padding: 4px 10px;
      border-radius: 12px;
      background: #2e2e40;
      color: #a0a0b8;
    }
    #status-badge.syncing  { background: #1a3a5c; color: #60b0ff; }
    #status-badge.erasing  { background: #3a2a10; color: #ffaa40; }
    #status-badge.verifying { background: #2a1a4a; color: #c060ff; }
    #status-badge.error    { background: #3a1a1a; color: #ff6060; }
    main { max-width: 700px; margin: 0 auto; padding: 16px; }
    .disk-info {
      background: #1a1a24;
      border: 1px solid #2e2e40;
      border-radius: 8px;
      padding: 12px 16px;
      margin-bottom: 16px;
      font-size: 0.85rem;
      color: #a0a0b8;
    }
    .disk-bar-track {
      background: #2e2e40;
      border-radius: 4px;
      height: 6px;
      margin-top: 6px;
      overflow: hidden;
    }
    .disk-bar-fill {
      background: #4060d0;
      height: 100%%;
      border-radius: 4px;
      transition: width 0.3s;
    }
    .help-card {
      background: #141c2c;
      border: 1px solid #294263;
      border-radius: 8px;
      padding: 12px 16px;
      margin-bottom: 16px;
      color: #b7d0f5;
      font-size: 0.85rem;
    }
    .help-card strong { color: #ffffff; }
    .help-card ol { margin: 10px 0 0 18px; padding: 0; }
    .help-card li { margin-bottom: 6px; }
    .warning-card {
      background: #3a2a10;
      border: 1px solid #704d15;
      border-radius: 8px;
      padding: 12px 16px;
      margin-bottom: 16px;
      color: #ffca80;
      font-size: 0.85rem;
    }
    #status-detail {
      margin-top: 6px;
      color: #8f90a8;
      font-size: 0.8rem;
    }
    .fc-group { margin-bottom: 20px; }
    .fc-group summary {
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
    }
    .fc-group summary::before { content: "\25b6"; font-size: 0.7rem; transition: transform 0.2s; }
    .fc-group[open] summary::before { transform: rotate(90deg); }
    .session-card {
      background: #1a1a24;
      border: 1px solid #2e2e40;
      border-radius: 8px;
      padding: 12px 14px;
      margin-top: 8px;
      display: flex;
      flex-direction: column;
      gap: 6px;
    }
    .session-header {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 8px;
      flex-wrap: wrap;
    }
    .session-title { font-size: 0.9rem; font-weight: 500; }
    .session-meta { font-size: 0.75rem; color: #808098; display: flex; gap: 10px; flex-wrap: wrap; }
    .badge {
      display: inline-block;
      padding: 2px 8px;
      border-radius: 8px;
      font-size: 0.7rem;
      background: #2e2e40;
      color: #909098;
    }
    .badge.erased { background: #1a3a1a; color: #60d060; }
    .badge.no-erase { background: #3a2a10; color: #c08030; }
    .session-actions { display: flex; gap: 8px; flex-wrap: wrap; }
    button, a.btn {
      display: inline-block;
      padding: 6px 14px;
      border-radius: 6px;
      font-size: 0.8rem;
      cursor: pointer;
      border: none;
      text-decoration: none;
      font-weight: 500;
      transition: opacity 0.15s;
    }
    button:hover, a.btn:hover { opacity: 0.8; }
    .btn-download { background: #2a4a80; color: #a0c8ff; }
    .btn-manifest { background: #2e2e40; color: #a0a0b8; }
    .btn-delete   { background: #4a1a1a; color: #ff8080; }
    .empty-state {
      text-align: center;
      padding: 48px 24px;
      color: #505068;
    }
    .empty-state .icon { font-size: 3rem; margin-bottom: 12px; }
    .empty-state ol {
      display: inline-block;
      margin: 12px auto 0;
      padding-left: 18px;
      text-align: left;
      color: #7f8098;
    }
    .progress-bar-track {
      background: #2e2e40;
      border-radius: 3px;
      height: 4px;
      overflow: hidden;
      display: none;
    }
    .progress-bar-fill {
      background: #60b0ff;
      height: 100%%;
      width: 0%%;
      border-radius: 3px;
      transition: width 0.5s;
    }
  </style>
</head>
<body>

<header>
  <h1>LogFalcon</h1>
  <a href="/settings" style="color:#a0a0b8; text-decoration:none; font-size:1.2rem;" title="Settings">&#9881;</a>
  <span id="status-badge">Idle</span>
</header>

<div id="idle-shutdown-banner" style="display:none; background:#1a1400; border-bottom:1px solid #4a3a00; padding:6px 20px; text-align:center; font-size:0.8rem; color:#d4a017;">
  <span id="idle-shutdown-text"></span>
</div>

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
    <span>Pi SD card: <strong>%s GB used</strong> / %s GB free</span>
    <div class="disk-bar-track">
      <div class="disk-bar-fill" style="width: %d%%"></div>
    </div>
    <div id="status-detail">%s</div>
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

  %s

  %s
</main>

<script>
  function updateStatus() {
    fetch('/status')
      .then(r => r.json())
      .then(data => {
        const badge = document.getElementById('status-badge');
        const detail = document.getElementById('status-detail');
        const state = data.state || 'idle';
        const progress = data.progress || 0;
        const labels = {
          idle: 'Idle', identifying: 'Identifying FC\u2026',
          querying: 'Querying flash\u2026', syncing: 'Syncing\u2026',
          verifying: 'Verifying\u2026', erasing: 'Erasing\u2026', error: 'Error'
        };
        detail.textContent = data.message || 'Ready for the next sync.';
        badge.textContent = (labels[state] || state) +
          (state === 'syncing' && progress > 0 ? ' ' + progress + '%%' : '');
        badge.className = '';
        if (['syncing','identifying','querying'].includes(state)) badge.classList.add('syncing');
        else if (state === 'erasing') badge.classList.add('erasing');
        else if (state === 'verifying') badge.classList.add('verifying');
        else if (state === 'error') badge.classList.add('error');

        const progressContainer = document.getElementById('sync-progress-container');
        const progressFill = document.getElementById('progress-fill');
        const progressLabel = document.getElementById('sync-progress-label');
        if (state === 'syncing') {
          progressContainer.style.display = 'block';
          progressFill.style.width = progress + '%%';
          progressLabel.textContent = 'Syncing flash... ' + progress + '%%';
        } else {
          progressContainer.style.display = 'none';
        }

        const shutBanner = document.getElementById('idle-shutdown-banner');
        const shutText = document.getElementById('idle-shutdown-text');
        const shutMin = data.idle_shutdown_minutes || 0;
        const shutSec = data.idle_shutdown_remaining_sec;
        if (shutMin > 0 && shutSec != null) {
          const m = Math.floor(shutSec / 60);
          const s = shutSec %% 60;
          shutText.textContent = '\u23fb Auto-shutdown in ' + m + ' min ' + s + ' sec \u2014 activity resets timer';
          shutBanner.style.display = 'block';
          if (shutSec < 60) {
            shutBanner.style.background = '#2a0a0a';
            shutBanner.style.borderBottomColor = '#6a1a1a';
            shutText.style.color = '#e04040';
          } else {
            shutBanner.style.background = '#1a1400';
            shutBanner.style.borderBottomColor = '#4a3a00';
            shutText.style.color = '#d4a017';
          }
        } else {
          shutBanner.style.display = 'none';
        }
      })
      .catch(() => {});
  }
  updateStatus();
  setInterval(updateStatus, 3000);

  function deleteSession(sessionId, btn) {
    if (!confirm('Delete this session from the Pi?\n\nMake sure you have downloaded the .bbl file first.')) return;
    btn.disabled = true;
    btn.textContent = 'Deleting\u2026';
    fetch('/sessions/' + sessionId, {
      method: 'DELETE',
      headers: { 'X-CSRF-Token': '%s' }
    })
      .then(r => r.json())
      .then(data => {
        if (data.deleted) {
          const card = btn.closest('.session-card');
          card.style.transition = 'opacity 0.3s';
          card.style.opacity = '0';
          setTimeout(() => { card.remove(); location.reload(); }, 300);
        } else {
          btn.disabled = false;
          btn.textContent = 'Delete from Pi';
          alert('Delete failed.');
        }
      })
      .catch(() => {
        btn.disabled = false;
        btn.textContent = 'Delete from Pi';
        alert('Delete request failed.');
      });
  }
</script>
</body>
</html>`,
		fmt.Sprintf("%.1f", params.UsedGB),
		fmt.Sprintf("%.1f", params.FreeGB),
		params.Pct,
		esc(params.StatusMessage),
		params.StorageWarningHTML,
		params.SessionsHTML,
		esc(params.CSRFToken),
	)
}

// RenderSessions renders session cards HTML fragment.
func RenderSessions(sessions []*storage.Session) string {
	if len(sessions) == 0 {
		return `<div class="empty-state">` +
			`<div class="icon">📭</div>` +
			`<p>No sessions yet.</p>` +
			`<ol>` +
			`<li>Power on the Pi and give the hotspot up to 90 seconds to appear.</li>` +
			`<li>Join the Wi-Fi network, then plug the FC into the Pi&apos;s inner OTG port.</li>` +
			`<li>Make sure the FC is logging to SPI flash, not an FC-side SD card.</li>` +
			`<li>Wait for the LED success pattern, then refresh this page.</li>` +
			`</ol></div>`
	}

	var b strings.Builder
	currentFC := ""
	for i, sess := range sessions {
		if sess.FCDir != currentFC {
			if currentFC != "" {
				b.WriteString("</div></details>")
			}
			currentFC = sess.FCDir
			fmt.Fprintf(&b, `<details class="fc-group" open><summary>%s</summary><div>`, esc(sess.FCDir))
		}

		var (
			fcVer    = "?"
			fileSize int64
			erased   bool
			sha256   string
		)
		if sess.Manifest != nil {
			fcVer = sess.Manifest.FC.APIVersion
			fileSize = sess.Manifest.File.Bytes
			erased = sess.Manifest.EraseCompleted
			sha256 = sess.Manifest.File.SHA256
		}
		fileMB := fmt.Sprintf("%.1f", float64(fileSize)/1048576)

		erasedCls := "no-erase"
		erasedTxt := "Not erased"
		erasedTitle := "The log was copied safely, but the FC flash still needs attention."
		if erased {
			erasedCls = "erased"
			erasedTxt = "Erased"
			erasedTitle = "Flash copy verified and FC erase completed."
		}

		shaHTML := ""
		if sha256 != "" {
			short := sha256
			if len(short) > 12 {
				short = short[:12]
			}
			shaHTML = fmt.Sprintf(`<span title="%s">SHA-256: %s…</span>`, esc(sha256), esc(short))
		}

		bblHTML := ""
		if sess.BBLPath != nil {
			bblHTML = fmt.Sprintf(
				`<a class="btn btn-download" href="/download/%s/raw_flash.bbl">Download .bbl</a>`,
				esc(sess.SessionID),
			)
		}

		title := strings.ReplaceAll(sess.SessionDir, "_", " ")

		fmt.Fprintf(&b,
			`<div class="session-card">`+
				`<div class="session-header">`+
				`<span class="session-title">%s</span>`+
				`<span class="badge %s" title="%s">%s</span>`+
				`</div>`+
				`<div class="session-meta">`+
				`<span>%s MB</span>`+
				`<span>API %s</span>`+
				`%s`+
				`</div>`+
				`<div class="session-actions">`+
				`%s`+
				`<a class="btn btn-manifest" href="/download/%s/manifest.json">Manifest</a>`+
				`<button class="btn-delete" onclick="deleteSession('%s', this)">Delete from Pi</button>`+
				`</div></div>`,
			esc(title),
			erasedCls, esc(erasedTitle), erasedTxt,
			fileMB,
			esc(fcVer),
			shaHTML,
			bblHTML,
			esc(sess.SessionID),
			esc(sess.SessionID),
		)

		if i == len(sessions)-1 {
			b.WriteString("</div></details>")
		}
	}
	return b.String()
}

// RenderSettings renders the settings page.
func RenderSettings(params SettingsParams) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Settings — LogFalcon</title>
  <style>
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
      width: 100%%; padding: 8px 12px;
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
  </style>
</head>
<body>
<header>
  <h1>Settings</h1>
</header>
<main>
  <a class="back-link" href="/">&larr; Back</a>
  %s
  %s
  <div class="current-info">
    <strong>Current SSID:</strong> %s<br>
    <strong>Current Password:</strong> %s
  </div>
  <form method="POST" action="/settings">
    <input type="hidden" name="csrf_token" value="%s">
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
</html>`,
		params.MsgHTML,
		params.WarningHTML,
		esc(params.CurrentSSID),
		esc(params.CurrentPass),
		esc(params.CSRFToken),
	)
}

// RenderError renders an error page.
func RenderError(code int, reason string) string {
	return fmt.Sprintf(
		`<!doctype html><html><head>`+
			`<meta name="viewport" content="width=device-width,initial-scale=1">`+
			`<title>%d</title>`+
			`<style>body{font-family:system-ui,sans-serif;display:flex;justify-content:center;`+
			`align-items:center;min-height:80vh;margin:0;background:#111;color:#eee}`+
			`div{text-align:center}h1{font-size:4rem;margin:0;color:#0ff}`+
			`p{color:#aaa;margin-top:.5rem}</style></head>`+
			`<body><div><h1>%d</h1><p>%s</p>`+
			`<p><a href="/" style="color:#0ff">← Home</a></p>`+
			`</div></body></html>`,
		code, code, esc(reason),
	)
}
