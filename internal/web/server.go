package web

import (
	"compress/gzip"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	gosync "sync"
	"time"

	"github.com/proeugene/logfalcon/internal/config"
	"github.com/proeugene/logfalcon/internal/storage"
	lfSync "github.com/proeugene/logfalcon/internal/sync"
	"github.com/proeugene/logfalcon/internal/util"
)

const (
	sessionsCacheTTL       = 10 * time.Second
	sseInterval            = 2 * time.Second
	fileChunkSize          = 1 << 20 // 1 MB
	defaultHotspotPassword = "fpvpilot"
	hostapdConf            = "/etc/hostapd/hostapd.conf"
	logfalconTOML          = "/etc/logfalcon/logfalcon.toml"
)

var allowedDownloads = map[string]bool{
	"raw_flash.bbl": true,
	"manifest.json": true,
}

var captivePaths = map[string]bool{
	"/generate_204":              true,
	"/gen_204":                   true,
	"/hotspot-detect.html":       true,
	"/library/test/success.html": true,
	"/connecttest.txt":           true,
	"/ncsi.txt":                  true,
}

const captiveHTML = `<!DOCTYPE html><html><head>` +
	`<meta http-equiv="refresh" content="0; url=/">` +
	`<title>LogFalcon</title>` +
	`</head><body>` +
	`<p>Redirecting to <a href="/">LogFalcon</a>...</p>` +
	`</body></html>`

// Server is the LogFalcon HTTP server.
type Server struct {
	storagePath   string
	config        *config.Config
	csrfToken     string
	mux           *http.ServeMux
	startedAt     time.Time
	sessionsCache struct {
		mu   gosync.Mutex
		data []*storage.Session
		ts   time.Time
	}
	lastActivity     time.Time
	lastActivityLock gosync.Mutex
}

// NewServer creates a configured Server with all routes registered.
func NewServer(storagePath string, cfg *config.Config) *Server {
	token := make([]byte, 18)
	if _, err := rand.Read(token); err != nil {
		slog.Error("failed to generate CSRF token", "error", err)
		return nil
	}

	s := &Server{
		storagePath: storagePath,
		config:      cfg,
		csrfToken:   hex.EncodeToString(token),
		mux:         http.NewServeMux(),
		startedAt:   time.Now(),
		lastActivity: time.Now(),
	}

	s.mux.HandleFunc("GET /", s.handleIndex)
	s.mux.HandleFunc("GET /sessions", s.handleSessions)
	s.mux.HandleFunc("GET /status", s.handleStatus)
	s.mux.HandleFunc("GET /events", s.handleSSE)
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("GET /download/", s.handleDownload)
	s.mux.HandleFunc("DELETE /sessions/", s.handleDeleteSession)
	s.mux.HandleFunc("GET /settings", s.handleSettingsGet)
	s.mux.HandleFunc("POST /settings", s.handleSettingsPost)

	// Captive portal probes
	for path := range captivePaths {
		p := path // capture
		s.mux.HandleFunc("GET "+p, func(w http.ResponseWriter, r *http.Request) {
			s.sendHTML(w, r, http.StatusOK, captiveHTML)
		})
	}

	return s
}

// ListenAndServe starts the HTTP server. It also starts the idle shutdown
// goroutine when configured.
func (s *Server) ListenAndServe(addr string) error {
	if s.config.IdleShutdownMinutes > 0 {
		go s.idleShutdownMonitor()
	}
	slog.Info("starting web server", "addr", addr)
	srv := &http.Server{
		Addr:           addr,
		Handler:        s,
		ReadTimeout:    15 * time.Second,
		WriteTimeout:   60 * time.Second,
		IdleTimeout:    120 * time.Second,
		MaxHeaderBytes: 1 << 20, // 1 MB
	}
	return srv.ListenAndServe()
}

// ServeHTTP implements http.Handler, suppressing per-request log noise.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// ---------- session cache ----------

func (s *Server) getSessions() []*storage.Session {
	s.sessionsCache.mu.Lock()
	defer s.sessionsCache.mu.Unlock()

	if time.Since(s.sessionsCache.ts) > sessionsCacheTTL {
		sessions, err := storage.ListSessions(s.storagePath)
		if err != nil {
			slog.Warn("listing sessions", "error", err)
			sessions = nil
		}
		s.sessionsCache.data = sessions
		s.sessionsCache.ts = time.Now()
	}
	return s.sessionsCache.data
}

func (s *Server) invalidateSessionsCache() {
	s.sessionsCache.mu.Lock()
	defer s.sessionsCache.mu.Unlock()
	s.sessionsCache.ts = time.Time{}
}

// ---------- route handlers ----------

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		s.sendError(w, r, http.StatusNotFound, "")
		return
	}

	sessions := s.getSessions()
	status := lfSync.GetStatus()

	var usedGB, freeGB float64
	var freeMB float64
	usedGB, freeGB, err := util.UsedAndFreeGB(s.storagePath)
	if err != nil {
		slog.Warn("disk stats", "error", err)
	}
	freeMB, _ = util.FreeMB(s.storagePath)

	totalGB := usedGB + freeGB
	pct := 0
	if totalGB > 0 {
		pct = int(usedGB / totalGB * 100)
	}

	sessionsHTML := RenderSessions(sessions)
	statusMessage := status.Message
	if statusMessage == "" {
		statusMessage = "Ready for the next sync."
	}

	storageWarningHTML := ""
	if freeMB < float64(s.config.MinFreeSpaceMB) {
		storageWarningHTML = fmt.Sprintf(
			`<div class="warning-card">Low space: only %.1f MB free. `+
				`Oldest sessions may be removed automatically during the next sync `+
				`to stay above the %d MB reserve.</div>`,
			freeMB, s.config.MinFreeSpaceMB,
		)
	}

	body := RenderIndex(IndexParams{
		UsedGB:             usedGB,
		FreeGB:             freeGB,
		Pct:                pct,
		SessionsHTML:       sessionsHTML,
		StatusMessage:      statusMessage,
		StorageWarningHTML: storageWarningHTML,
		CSRFToken:          s.csrfToken,
	})
	s.sendHTML(w, r, http.StatusOK, body)
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	sessions := s.getSessions()
	if sessions == nil {
		sessions = []*storage.Session{}
	}
	s.sendJSON(w, r, http.StatusOK, sessions)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	status := lfSync.GetStatus()
	payload := map[string]any{
		"state":    status.State,
		"message":  status.Message,
		"progress": status.Progress,
	}
	s.addIdleShutdownInfo(payload)
	s.sendJSON(w, r, http.StatusOK, payload)
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	var prev string
	ticker := time.NewTicker(sseInterval)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			status := lfSync.GetStatus()
			data, err := json.Marshal(status)
			if err != nil {
				slog.Warn("failed to marshal SSE status", "error", err)
				continue
			}
			payload := string(data)
			if payload != prev {
				fmt.Fprintf(w, "data: %s\n\n", payload)
				flusher.Flush()
				prev = payload
			}
		}
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	status := lfSync.GetStatus()
	sessions := s.getSessions()

	var usedGB, freeGB, freeMB float64
	usedGB, freeGB, err := util.UsedAndFreeGB(s.storagePath)
	if err != nil {
		slog.Warn("disk stats", "error", err)
	}
	freeMB, _ = util.FreeMB(s.storagePath)

	hostapd := readHostapdConfig()

	payload := map[string]any{
		"ok":         status.State != "error",
		"uptime_sec": int(time.Since(s.startedAt).Seconds()),
		"status": map[string]any{
			"state":    status.State,
			"message":  status.Message,
			"progress": status.Progress,
		},
		"session_count": len(sessions),
		"storage": map[string]any{
			"used_gb":                   round2(usedGB),
			"free_gb":                   round2(freeGB),
			"free_mb":                   round1(freeMB),
			"reserve_mb":               s.config.MinFreeSpaceMB,
			"storage_pressure_cleanup": s.config.StoragePressureCleanup,
			"low_space":                freeMB < float64(s.config.MinFreeSpaceMB),
		},
		"hotspot": map[string]any{
			"ssid":                      hostapd["ssid"],
			"default_password_in_use":   hostapd["wpa_passphrase"] == defaultHotspotPassword,
		},
	}
	s.sendJSON(w, r, http.StatusOK, payload)
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	// Path: /download/{fc_dir}/{session_dir}/{filename}
	sub := strings.TrimPrefix(r.URL.Path, "/download/")

	var sessionID, filename string
	if strings.HasSuffix(sub, "/raw_flash.bbl") {
		sessionID = sub[:len(sub)-len("/raw_flash.bbl")]
		filename = "raw_flash.bbl"
	} else if strings.HasSuffix(sub, "/manifest.json") {
		sessionID = sub[:len(sub)-len("/manifest.json")]
		filename = "manifest.json"
	} else {
		s.sendError(w, r, http.StatusNotFound, "")
		return
	}

	if !allowedDownloads[filename] {
		s.sendError(w, r, http.StatusBadRequest, "")
		return
	}

	filePath, err := s.resolveSessionFile(sessionID, filename)
	if err != nil {
		s.sendError(w, r, http.StatusNotFound, "")
		return
	}

	s.sendFile(w, r, filePath, filename)
}

func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-CSRF-Token") != s.csrfToken {
		s.sendError(w, r, http.StatusForbidden, "Invalid CSRF token")
		return
	}

	sessionID := strings.TrimPrefix(r.URL.Path, "/sessions/")
	sessionPath, err := s.resolveSessionPath(sessionID)
	if err != nil {
		s.sendError(w, r, http.StatusBadRequest, "")
		return
	}

	if _, err := os.Stat(sessionPath); os.IsNotExist(err) {
		s.sendError(w, r, http.StatusNotFound, "")
		return
	}

	if err := os.RemoveAll(sessionPath); err != nil {
		slog.Error("deleting session", "path", sessionPath, "error", err)
		s.sendError(w, r, http.StatusInternalServerError, "")
		return
	}

	// Remove empty FC parent directory.
	parent := filepath.Dir(sessionPath)
	remaining, _ := os.ReadDir(parent)
	if len(remaining) == 0 {
		_ = os.Remove(parent)
	}

	s.invalidateSessionsCache()
	slog.Info("deleted session", "session_id", sessionID, "client", r.RemoteAddr)
	s.sendJSON(w, r, http.StatusOK, map[string]any{"deleted": true, "session_id": sessionID})
}

func (s *Server) handleSettingsGet(w http.ResponseWriter, r *http.Request) {
	body := s.renderSettingsPage("", false)
	s.sendHTML(w, r, http.StatusOK, body)
}

func (s *Server) handleSettingsPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.sendHTML(w, r, http.StatusBadRequest, s.renderSettingsPage("Invalid form data.", true))
		return
	}

	csrfToken := r.FormValue("csrf_token")
	ssid := strings.TrimSpace(r.FormValue("ssid"))
	password := strings.TrimSpace(r.FormValue("password"))

	if csrfToken != s.csrfToken {
		s.sendHTML(w, r, http.StatusForbidden,
			s.renderSettingsPage("Security check failed. Reload and try again.", true))
		return
	}

	if err := validateHotspotValue(ssid, 1, 32, "SSID"); err != "" {
		s.sendHTML(w, r, http.StatusBadRequest, s.renderSettingsPage(err, true))
		return
	}
	if err := validateHotspotValue(password, 8, 63, "Password"); err != "" {
		s.sendHTML(w, r, http.StatusBadRequest, s.renderSettingsPage(err, true))
		return
	}

	// Write to hostapd.conf
	if !rewritePrefixedLines(hostapdConf, map[string]string{
		"ssid=":           "ssid=" + ssid,
		"wpa_passphrase=": "wpa_passphrase=" + password,
	}) {
		s.sendHTML(w, r, http.StatusInternalServerError,
			s.renderSettingsPage("Could not save hotspot settings. Review the Pi before flying.", true))
		return
	}

	// Write to logfalcon.toml
	rewritePrefixedLines(logfalconTOML, map[string]string{
		"hotspot_ssid =":     fmt.Sprintf(`hotspot_ssid = %q`, ssid),
		"hotspot_password =": fmt.Sprintf(`hotspot_password = %q`, password),
	})

	// Restart hostapd
	cmd := exec.Command("systemctl", "restart", "hostapd")
	if err := cmd.Run(); err != nil {
		slog.Warn("hostapd restart failed", "error", err)
		s.sendHTML(w, r, http.StatusInternalServerError,
			s.renderSettingsPage("Settings saved, but Wi-Fi restart failed. Reboot the Pi.", true))
		return
	}

	msg := fmt.Sprintf("Settings saved! Wi-Fi hotspot is now: %s. You may need to reconnect.", ssid)
	slog.Info("hotspot settings updated", "ssid", ssid, "client", r.RemoteAddr)
	s.sendHTML(w, r, http.StatusOK, s.renderSettingsPage(msg, false))
}

// ---------- settings helpers ----------

func (s *Server) renderSettingsPage(message string, isError bool) string {
	hostapd := readHostapdConfig()
	currentSSID := hostapd["ssid"]
	if currentSSID == "" {
		currentSSID = "Unknown"
	}
	currentPass := hostapd["wpa_passphrase"]
	if currentPass == "" {
		currentPass = "Unknown"
	}

	msgHTML := ""
	if message != "" {
		cls := "msg-success"
		if isError {
			cls = "msg-error"
		}
		msgHTML = fmt.Sprintf(`<div class="%s">%s</div>`, cls, esc(message))
	}

	warningHTML := ""
	if hostapd["wpa_passphrase"] == defaultHotspotPassword {
		warningHTML = `<div class="msg-error">` +
			`This Pi is still using the launch-default hotspot password. ` +
			`Change it before flying at a shared field.</div>`
	}

	return RenderSettings(SettingsParams{
		CurrentSSID: currentSSID,
		CurrentPass: currentPass,
		MsgHTML:     msgHTML,
		WarningHTML: warningHTML,
		CSRFToken:   s.csrfToken,
	})
}

// ---------- file serving ----------

func (s *Server) resolveSessionPath(sessionID string) (string, error) {
	parts := strings.Split(sessionID, "/")
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid session_id")
	}
	fcDir, sessDir := parts[0], parts[1]
	if strings.Contains(fcDir, "..") || strings.Contains(sessDir, "..") {
		return "", fmt.Errorf("path traversal")
	}

	absStorage, err := filepath.Abs(s.storagePath)
	if err != nil {
		return "", err
	}
	// Resolve symlinks on the storage root for consistent prefix checking
	// (e.g. macOS /var → /private/var).
	resolvedStorage, err := filepath.EvalSymlinks(absStorage)
	if err != nil {
		resolvedStorage = absStorage
	}
	candidate := filepath.Join(absStorage, fcDir, sessDir)
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		resolved = filepath.Clean(candidate)
	}
	if !strings.HasPrefix(resolved, resolvedStorage) {
		return "", fmt.Errorf("path outside storage root")
	}
	return candidate, nil
}

func (s *Server) resolveSessionFile(sessionID, filename string) (string, error) {
	if !allowedDownloads[filename] {
		return "", fmt.Errorf("file not allowed")
	}
	sessPath, err := s.resolveSessionPath(sessionID)
	if err != nil {
		return "", err
	}
	filePath := filepath.Join(sessPath, filename)
	if _, err := os.Stat(filePath); err != nil {
		return "", err
	}
	return filePath, nil
}

func (s *Server) sendFile(w http.ResponseWriter, r *http.Request, path, filename string) {
	info, err := os.Stat(path)
	if err != nil {
		s.sendError(w, r, http.StatusNotFound, "")
		return
	}
	size := info.Size()
	mtime := info.ModTime()
	etag := fmt.Sprintf(`"%d-%d"`, mtime.Unix(), size)

	// ETag: If-None-Match
	if inm := r.Header.Get("If-None-Match"); inm == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	f, err := os.Open(path)
	if err != nil {
		s.sendError(w, r, http.StatusInternalServerError, "")
		return
	}
	defer f.Close()

	lastModified := mtime.UTC().Format(http.TimeFormat)

	// Range request
	rangeHeader := r.Header.Get("Range")
	if rangeHeader != "" && strings.HasPrefix(rangeHeader, "bytes=") {
		spec := rangeHeader[6:]
		parts := strings.SplitN(spec, "-", 2)
		if len(parts) == 2 {
			var start, end int64
			if parts[0] != "" {
				var err error
				start, err = strconv.ParseInt(parts[0], 10, 64)
				if err != nil {
					w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", size))
					w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
					return
				}
			}
			if parts[1] != "" {
				var err error
				end, err = strconv.ParseInt(parts[1], 10, 64)
				if err != nil {
					w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", size))
					w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
					return
				}
			} else {
				end = size - 1
			}
			if end >= size {
				end = size - 1
			}
			if start > end || start >= size {
				w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", size))
				w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
				return
			}
			contentLength := end - start + 1
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
			w.Header().Set("Content-Length", strconv.FormatInt(contentLength, 10))
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, size))
			w.Header().Set("Accept-Ranges", "bytes")
			w.Header().Set("Last-Modified", lastModified)
			w.Header().Set("ETag", etag)
			w.WriteHeader(http.StatusPartialContent)
			if _, err := f.Seek(start, io.SeekStart); err != nil {
				return
			}
			streamChunked(w, f, contentLength)
			return
		}
	}

	// Full response
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Last-Modified", lastModified)
	w.Header().Set("ETag", etag)
	w.WriteHeader(http.StatusOK)
	streamChunked(w, f, size)
}

func streamChunked(w io.Writer, r io.Reader, length int64) {
	remaining := length
	buf := make([]byte, fileChunkSize)
	for remaining > 0 {
		toRead := int64(len(buf))
		if toRead > remaining {
			toRead = remaining
		}
		n, err := r.Read(buf[:toRead])
		if n > 0 {
			if _, wErr := w.Write(buf[:n]); wErr != nil {
				return
			}
			remaining -= int64(n)
		}
		if err != nil {
			break
		}
	}
}

// ---------- response helpers ----------

func (s *Server) sendBody(w http.ResponseWriter, r *http.Request, status int, contentType string, body []byte) {
	if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Content-Encoding", "gzip")
		w.WriteHeader(status)
		gz := gzip.NewWriter(w)
		if _, err := gz.Write(body); err != nil {
			slog.Warn("gzip write error", "error", err)
		}
		gz.Close()
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	if _, err := w.Write(body); err != nil {
		slog.Warn("response write error", "error", err)
	}
}

func (s *Server) sendHTML(w http.ResponseWriter, r *http.Request, status int, body string) {
	s.sendBody(w, r, status, "text/html; charset=utf-8", []byte(body))
}

func (s *Server) sendJSON(w http.ResponseWriter, r *http.Request, status int, data any) {
	b, err := json.Marshal(data)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.sendBody(w, r, status, "application/json", b)
}

func (s *Server) sendError(w http.ResponseWriter, r *http.Request, code int, message string) {
	if message == "" {
		message = http.StatusText(code)
	}
	body := RenderError(code, message)
	s.sendHTML(w, r, code, body)
}

// ---------- idle shutdown ----------

func (s *Server) addIdleShutdownInfo(payload map[string]any) {
	if s.config.IdleShutdownMinutes <= 0 {
		payload["idle_shutdown_minutes"] = 0
		payload["idle_shutdown_remaining_sec"] = 0
		return
	}
	s.lastActivityLock.Lock()
	elapsed := time.Since(s.lastActivity)
	s.lastActivityLock.Unlock()

	remaining := time.Duration(s.config.IdleShutdownMinutes)*time.Minute - elapsed
	if remaining < 0 {
		remaining = 0
	}
	payload["idle_shutdown_minutes"] = s.config.IdleShutdownMinutes
	payload["idle_shutdown_remaining_sec"] = int(remaining.Seconds())
}

func (s *Server) idleShutdownMonitor() {
	timeout := time.Duration(s.config.IdleShutdownMinutes) * time.Minute
	slog.Info("idle auto-shutdown enabled", "minutes", s.config.IdleShutdownMinutes)

	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		status := lfSync.GetStatus()
		if status.State != "idle" && status.State != "" {
			// Sync active — reset timer
			s.lastActivityLock.Lock()
			s.lastActivity = time.Now()
			s.lastActivityLock.Unlock()
			continue
		}

		s.lastActivityLock.Lock()
		elapsed := time.Since(s.lastActivity)
		s.lastActivityLock.Unlock()

		if elapsed >= timeout {
			slog.Warn("no sync activity — shutting down", "idle_minutes", s.config.IdleShutdownMinutes)
			cmd := exec.Command("/usr/bin/sudo", "/sbin/shutdown", "-h", "now")
			_ = cmd.Run()
			return
		}
	}
}

// ---------- hostapd config ----------

func readHostapdConfig() map[string]string {
	data, err := os.ReadFile(hostapdConf)
	if err != nil {
		return map[string]string{}
	}
	result := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		k, v, _ := strings.Cut(line, "=")
		result[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return result
}

func rewritePrefixedLines(path string, replacements map[string]string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		slog.Warn("could not read file", "path", path, "error", err)
		return false
	}
	text := string(data)
	trailingNL := strings.HasSuffix(text, "\n")
	lines := strings.Split(text, "\n")
	remaining := make(map[string]string)
	for k, v := range replacements {
		remaining[k] = v
	}

	var updated []string
	for _, line := range lines {
		stripped := strings.TrimLeft(line, " \t")
		replaced := false
		for prefix, newLine := range remaining {
			if strings.HasPrefix(stripped, prefix) {
				indent := line[:len(line)-len(stripped)]
				updated = append(updated, indent+newLine)
				delete(remaining, prefix)
				replaced = true
				break
			}
		}
		if !replaced {
			updated = append(updated, line)
		}
	}
	for _, v := range remaining {
		updated = append(updated, v)
	}
	out := strings.Join(updated, "\n")
	if trailingNL && !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
		slog.Warn("could not write file", "path", path, "error", err)
		return false
	}
	return true
}

func validateHotspotValue(value string, min, max int, label string) string {
	if len(value) < min || len(value) > max {
		return fmt.Sprintf("%s must be %d–%d characters.", label, min, max)
	}
	for _, ch := range value {
		if ch < 32 || ch == 127 {
			return fmt.Sprintf("%s must use printable characters only.", label)
		}
	}
	return ""
}

// ---------- helpers ----------

func round1(f float64) float64 {
	return float64(int(f*10+0.5)) / 10
}

func round2(f float64) float64 {
	return float64(int(f*100+0.5)) / 100
}


