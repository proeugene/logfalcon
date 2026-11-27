package web

import _ "embed"

// Keep the dashboard styled even when the hotspot has no internet connection.
//
//go:embed field-kit.css
var appCSS string

func icon(name string) string {
	paths := map[string]string{
		"download": `<path d="M12 3v12m-5-5 5 5 5-5M4 16v4h16v-4"/>`,
		"arrow":    `<path d="M4 12h16m-6-6 6 6-6 6"/>`,
		"wifi":     `<path d="M2 8a16 16 0 0 1 20 0M5 12a11 11 0 0 1 14 0m-11 4a6 6 0 0 1 8 0"/><circle cx="12" cy="20" r=".7"/>`,
		"shield":   `<path d="m12 3 8 3v6c0 5-8 9-8 9s-8-4-8-9V6l8-3Z"/><path d="m8 12 3 3 5-6"/>`,
		"storage":  `<rect x="4" y="3" width="16" height="18" rx="3"/><path d="M4 14h16m-11 4h.01M9 7h6"/>`,
		"file":     `<path d="M14 3H6a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9l-6-6Z"/><path d="M14 3v6h6M8 13h8m-8 4h5"/>`,
		"upload":   `<path d="M12 16V3m-5 5 5-5 5 5M4 16v5h16v-5"/>`,
		"check":    `<path d="m5 12 4 4L20 5"/>`,
		"eye":      `<path d="M2 12s4-7 10-7 10 7 10 7-4 7-10 7S2 12 2 12Z"/><circle cx="12" cy="12" r="3"/>`,
		"help":     `<circle cx="12" cy="12" r="9"/><path d="M9.5 9a2.5 2.5 0 1 1 4 2c-1 .7-1.5 1-1.5 3m0 3h.01"/>`,
	}
	return `<svg class="icon" aria-hidden="true" viewBox="0 0 24 24">` + paths[name] + `</svg>`
}

func renderShell(title, active, version, body, script string) string {
	nav := func(path, label, key string) string {
		current := ""
		if active == key {
			current = ` aria-current="page"`
		}
		cls := ""
		if key == "software" {
			cls = ` class="nav-update"`
		}
		return `<a href="` + path + `"` + cls + current + `>` + label + `</a>`
	}
	return `<!doctype html><html lang="en"><head><meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="theme-color" content="#1f3028"><link rel="icon" href="/favicon.svg" type="image/svg+xml"><title>` + esc(title) + ` — LogFalcon</title><style>` + appCSS + `</style></head><body><header class="topbar"><div class="topbar-inner"><a class="brand" href="/" aria-label="LogFalcon home"><img class="brand-mark" src="/assets/logfalcon-mark.svg" alt="" width="26" height="26"><span class="brand-name">LogFalcon</span></a><nav class="nav" aria-label="Main navigation">` + nav("/", "Recordings", "dashboard") + nav("/software", "Updates", "software") + nav("/settings", "Settings", "settings") + `</nav></div></header><main>` + body + `</main><footer><span>Installed version: ` + esc(version) + ` · Works offline</span></footer><script>` + script + `</script></body></html>`
}

func pageIntro(kicker, title, description string) string {
	body := `<div class="page-intro"><h1>` + esc(title) + `</h1>`
	if description != "" {
		body += `<p>` + esc(description) + `</p>`
	}
	return body + `</div>`
}
