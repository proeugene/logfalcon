// Package web provides the LogFalcon HTTP server and HTML templates.
package web

import (
	"fmt"
	"html"
	"strings"
	"time"

	"github.com/proeugene/logfalcon/internal/storage"
)

// IndexParams holds values for the main dashboard page.
type IndexParams struct {
	UsedGB, FreeGB     float64
	Pct                int
	SessionsHTML       string
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

// RenderIndex renders the saved-log browser. Live status is intentionally not
// displayed until the separate sync and web processes share a status source.
func RenderIndex(params IndexParams) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Saved logs — LogFalcon</title>
  <style>
    *, *::before, *::after { box-sizing: border-box; }
    body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; margin: 0; background: #0f0f12; color: #e0e0e8; line-height: 1.5; }
    header { background: #1a1a24; border-bottom: 1px solid #2e2e40; padding: 12px 20px; display: flex; align-items: center; justify-content: space-between; }
    header h1 { margin: 0; font-size: 1.1rem; }
    header a { color: #c0c0d8; padding: 10px 0; text-decoration: none; }
    main { max-width: 700px; margin: 0 auto; padding: 20px 16px; }
    h2 { font-size: 1rem; margin: 0 0 8px; }
    p { margin: 0 0 12px; }
    a { color: #a0c8ff; }
    a:focus-visible, button:focus-visible, summary:focus-visible { outline: 2px solid #60b0ff; outline-offset: 3px; }
    .status-card, .disk-info, .help-card, .warning-card { border: 1px solid #2e2e40; border-radius: 8px; background: #1a1a24; padding: 16px; margin-bottom: 20px; }
    .status-card p, .help-card, .disk-info { color: #b5b5c8; font-size: 0.9rem; }
    .status-card p:last-child { margin-bottom: 0; }
    .status-card { border-color: #665126; }
    .warning-card { background: #3a2a10; color: #ffca80; }
    .logs-header { display: flex; justify-content: space-between; align-items: center; gap: 12px; margin-bottom: 12px; }
    .logs-header h2 { margin: 0; }
    .refresh { padding: 12px 0; font-size: 0.9rem; }
    .fc-group { margin-bottom: 20px; }
    summary { cursor: pointer; min-height: 44px; padding: 10px 0; }
    .fc-group > summary { font-weight: 600; border-bottom: 1px solid #2e2e40; overflow-wrap: anywhere; }
    .session-card { background: #1a1a24; border: 1px solid #2e2e40; border-radius: 8px; padding: 16px; margin-top: 10px; }
    .session-header { display: flex; justify-content: space-between; align-items: center; gap: 12px; flex-wrap: wrap; margin-bottom: 6px; }
    .session-title { font-weight: 500; }
    .session-size { color: #b5b5c8; font-size: 0.85rem; margin-bottom: 12px; }
    .badge { padding: 3px 8px; border-radius: 6px; font-size: 0.75rem; background: #2e2e40; color: #c0c0d8; }
    .badge.erased { background: #1a3a1a; color: #8de08d; }
    .badge.no-erase { background: #2e2e40; color: #c0c0d8; }
    button, a.btn { display: inline-flex; align-items: center; justify-content: center; min-height: 44px; padding: 10px 16px; border-radius: 6px; border: 1px solid transparent; font: inherit; font-size: 0.9rem; text-decoration: none; cursor: pointer; }
    .btn-download { background: #2a4a80; color: #d7e9ff; }
    .btn-manifest { background: #2e2e40; color: #d0d0e0; }
    .btn-delete { background: transparent; border-color: #754141; color: #ffb0b0; }
    button:disabled { opacity: 0.6; cursor: wait; }
    .session-details { margin-top: 8px; font-size: 0.85rem; color: #b5b5c8; }
    .session-meta { display: flex; flex-direction: column; gap: 6px; overflow-wrap: anywhere; margin-bottom: 12px; }
    .session-actions { display: flex; gap: 10px; flex-wrap: wrap; }
    .empty-state { padding: 24px 16px; border: 1px dashed #45455c; border-radius: 8px; margin-bottom: 20px; color: #b5b5c8; }
    .empty-state strong { color: #e0e0e8; }
    .disk-bar-track { background: #2e2e40; border-radius: 4px; height: 6px; margin-top: 10px; overflow: hidden; }
    .disk-bar-fill { background: #607edb; height: 100%%; }
    .help-card ol { padding-left: 22px; }
    .help-card li { margin-bottom: 8px; }
    @media (max-width: 400px) { .session-header { align-items: flex-start; flex-direction: column; } }
  </style>
</head>
<body>
<header><h1>LogFalcon</h1><a href="/settings">Settings</a></header>
<main>
  <section class="status-card" aria-labelledby="transfer-title">
    <h2 id="transfer-title">Transfer status</h2>
    <p><strong>Live transfer status is unavailable in this version.</strong></p>
    <p>This page lists saved sessions. It cannot confirm that copying or erasing has finished. Keep the FC connected until the result is confirmed in the service log.</p>
  </section>
  %s
  <section aria-labelledby="logs-title">
    <div class="logs-header"><h2 id="logs-title">Saved logs</h2><a class="refresh" href="/">Refresh list</a></div>
    %s
  </section>
  <section class="disk-info" aria-labelledby="storage-title">
    <h2 id="storage-title">Pi storage</h2>
    <span><strong>%s GB free</strong> · %s GB used</span>
    <div class="disk-bar-track" aria-hidden="true"><div class="disk-bar-fill" style="width: %d%%"></div></div>
  </section>
  <details class="help-card">
    <summary>Connection and download help</summary>
    <ol>
      <li>Connect to the Pi's Wi-Fi and open <code>http://log.falcon</code>.</li>
      <li>Use the inner USB / OTG data port for the FC; the other port powers the Pi.</li>
      <li>After confirming completion, refresh the list and download the <code>.bbl</code>.</li>
      <li>Open it in Blackbox Explorer. Preserve the FC's original data during evaluation.</li>
    </ol>
    <p>For evaluation, disable automatic erase and storage cleanup in the Pi configuration. LED completion reporting also needs validation.</p>
  </details>
</main>
<script>
  function deleteSession(sessionId, btn) {
    if (!confirm('Delete this saved session from the Pi?\n\nDownload the .bbl first. The FC is not changed.')) return;
    btn.disabled = true;
    btn.textContent = 'Deleting…';
    fetch('/sessions/' + sessionId, {
      method: 'DELETE', headers: { 'X-CSRF-Token': '%s' }
    }).then(r => {
      if (!r.ok) throw new Error('Delete failed');
      return r.json();
    }).then(data => {
      if (!data.deleted) throw new Error('Delete failed');
      location.reload();
    }).catch(() => {
      btn.disabled = false;
      btn.textContent = 'Delete from Pi';
      alert('Could not confirm deletion. Check your Wi-Fi connection and refresh the list.');
    });
  }
</script>
</body>
</html>`, params.StorageWarningHTML, params.SessionsHTML,
		fmt.Sprintf("%.1f", params.FreeGB), fmt.Sprintf("%.1f", params.UsedGB), params.Pct, esc(params.CSRFToken))
}

func controllerLabel(dir string) string {
	variant, uid, ok := strings.Cut(strings.TrimPrefix(dir, "fc_"), "_uid-")
	if !ok {
		return dir
	}
	switch variant {
	case "BTFL":
		variant = "Betaflight"
	case "INAV":
		variant = "iNav"
	}
	return variant + " · controller " + uid
}

// RenderSessions renders session cards HTML fragment.
func RenderSessions(sessions []*storage.Session) string {
	if len(sessions) == 0 {
		return `<div class="empty-state"><p><strong>No saved sessions yet.</strong></p>` +
			`<p>After a completed transfer, refresh this list. Only sessions with a readable manifest appear here.</p></div>`
	}

	var b strings.Builder
	currentFC := ""
	for i, sess := range sessions {
		if sess.FCDir != currentFC {
			if currentFC != "" {
				b.WriteString("</div></details>")
			}
			currentFC = sess.FCDir
			fmt.Fprintf(&b, `<details class="fc-group" open><summary>%s</summary><div>`, esc(controllerLabel(sess.FCDir)))
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
		erasedTxt := "No erase recorded"
		erasedTitle := "The manifest does not record a completed FC erase."
		if erased {
			erasedCls = "erased"
			erasedTxt = "FC erased"
			erasedTitle = "The manifest records a completed FC erase."
		}

		if !erased && sess.Manifest != nil && sess.Manifest.EraseAttempted {
			erasedTxt = "Erase unconfirmed"
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

		if sess.BBLPath == nil {
			bblHTML = `<p>Log file unavailable.</p>`
		}
		title := strings.ReplaceAll(sess.SessionDir, "_", " ")
		if stamp, err := time.Parse("2006-01-02_150405", sess.SessionDir); err == nil {
			title = stamp.Format("2 Jan 2006 · 15:04:05")
		}

		fmt.Fprintf(&b,
			`<div class="session-card">`+
				`<div class="session-header">`+
				`<span class="session-title">%s</span>`+
				`<span class="badge %s" title="%s">%s</span>`+
				`</div>`+
				`<div class="session-size">%s MB</div>`+
				`%s`+
				`<details class="session-details"><summary>Details and actions</summary>`+
				`<div class="session-meta"><span>MSP API %s</span>%s</div>`+
				`<div class="session-actions">`+
				`<a class="btn btn-manifest" href="/download/%s/manifest.json">Manifest</a>`+
				`<button class="btn-delete" onclick="deleteSession('%s', this)">Delete from Pi</button>`+
				`</div></details></div>`,
			esc(title),
			erasedCls, esc(erasedTitle), erasedTxt,
			fileMB,
			bblHTML,
			esc(fcVer),
			shaHTML,
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
