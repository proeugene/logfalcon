package web

import (
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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
	"github.com/proeugene/logfalcon/internal/settings"
	"github.com/proeugene/logfalcon/internal/storage"
	lfSync "github.com/proeugene/logfalcon/internal/sync"
	"github.com/proeugene/logfalcon/internal/util"
)

const (
	sessionsCacheTTL       = 10 * time.Second
	sseInterval            = 2 * time.Second
	defaultHotspotPassword = "fpvpilot"
	hostapdConf            = "/etc/logfalcon/hostapd.conf"
	logfalconTOML          = "/etc/logfalcon/logfalcon.toml"
)

var allowedDownloads = map[string]bool{
	"raw_flash.bbl": true,
	"manifest.json": true,
}

// captivePortalPaths maps each OS network probe path to the exact response it
// expects when the network has working internet access. Returning these "success"
// responses prevents iOS/Android/Windows from marking the network as "no internet"
// and routing all app traffic over cellular instead of Wi-Fi. Since dnsmasq
// resolves ALL DNS to 192.168.4.1, every URL the user types in a browser lands
// on our server — the catch-all GET / handler serves the dashboard for anything
// not explicitly registered.
//
// Without this fix: iOS says "internet not available", silently switches to
// cellular data, and no URLs work while connected to the hotspot.
var captivePortalPaths = map[string]struct {
	status int
	body   string
	ctype  string
}{
	// iOS / macOS — expects exact Apple success page
	"/hotspot-detect.html":       {200, "<HTML><HEAD><TITLE>Success</TITLE></HEAD><BODY>Success</BODY></HTML>", "text/html"},
	"/library/test/success.html": {200, "<HTML><HEAD><TITLE>Success</TITLE></HEAD><BODY>Success</BODY></HTML>", "text/html"},
	// Android — expects 204 No Content (empty body)
	"/generate_204": {204, "", "text/plain"},
	"/gen_204":      {204, "", "text/plain"},
	// Windows NCSI
	"/ncsi.txt":        {200, "Microsoft NCSI", "text/plain"},
	"/connecttest.txt": {200, "Microsoft Connect Test", "text/plain"},
}

// Server is the LogFalcon HTTP server.
type Server struct {
	hotspotPath    string
	configPath     string
	restartHotspot func() error
	storagePath    string
	config         *config.Config
	csrfToken      string
	mux            *http.ServeMux
	startedAt      time.Time
	sessionsCache  struct {
		mu   gosync.Mutex
		data []*storage.Session
		ts   time.Time
	}
	activeRequests   int
	shutdown         func() error
	lastTransfer     string
	lastActivity     time.Time
	lastActivityLock gosync.Mutex
	lastThermalLog   time.Time
	thermalLogMu     gosync.Mutex
}

// NewServer creates a configured Server with all routes registered.
func NewServer(storagePath string, cfg *config.Config) *Server {
	token := make([]byte, 18)
	if _, err := rand.Read(token); err != nil {
		slog.Error("failed to generate CSRF token", "error", err)
		return nil
	}

	s := &Server{
		hotspotPath:    hostapdConf,
		configPath:     logfalconTOML,
		restartHotspot: restartHotspot,
		storagePath:    storagePath,
		config:         cfg,
		csrfToken:      hex.EncodeToString(token),
		mux:            http.NewServeMux(),
		startedAt:      time.Now(),
		lastActivity:   time.Now(),
		shutdown: func() error {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			return exec.CommandContext(ctx, "systemctl", "start", "logfalcon-shutdown.service").Run()
		},
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

	// Captive portal probes — respond with OS-specific "internet OK" responses
	// so iOS/Android/Windows keep all traffic on the Wi-Fi interface.
	for path, resp := range captivePortalPaths {
		p, r := path, resp // capture loop variables
		s.mux.HandleFunc("GET "+p, func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("Content-Type", r.ctype)
			w.WriteHeader(r.status)
			if r.body != "" {
				_, _ = w.Write([]byte(r.body))
			}
		})
	}

	return s
}

// ListenAndServe starts the HTTP server. It also starts the idle shutdown
// goroutine when configured.
func (s *Server) ListenAndServe(addr string) error {
	// systemd may restart only the web process after a crash, so boot-time
	// recovery alone is insufficient. Finish any pending rollback before serving.
	if _, err := os.Stat(s.configPath); err == nil {
		m := settings.Manager{Hostapd: s.hotspotPath, Config: s.configPath, Restart: s.restartHotspot}
		if err := m.Recover(); err != nil {
			return fmt.Errorf("settings recovery prevents web startup: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
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

// ServeHTTP records meaningful actions, excluding probes, polling and query strings.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/" || r.URL.Path == "/settings" || strings.HasPrefix(r.URL.Path, "/download/") || (r.Method == "DELETE" && strings.HasPrefix(r.URL.Path, "/sessions/")) {
		s.lastActivityLock.Lock()
		s.activeRequests++
		s.lastActivity = time.Now()
		s.lastActivityLock.Unlock()
		defer func() {
			s.lastActivityLock.Lock()
			s.activeRequests--
			s.lastActivity = time.Now()
			s.lastActivityLock.Unlock()
		}()
		started := time.Now()
		rec := &actionResponse{ResponseWriter: w, status: 200}
		s.mux.ServeHTTP(rec, r)
		slog.Info("web action", "method", r.Method, "path", r.URL.Path, "status", rec.status, "bytes", rec.bytes, "duration_ms", time.Since(started).Milliseconds())
		return
	}
	s.mux.ServeHTTP(w, r)
}

type actionResponse struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *actionResponse) WriteHeader(code int) { w.status = code; w.ResponseWriter.WriteHeader(code) }
func (w *actionResponse) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
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

	var usedGB, freeGB float64
	var freeMB float64
	usedGB, freeGB, err := util.UsedAndFreeGB(s.storagePath)
	if err != nil {
		slog.Warn("disk stats", "error", err)
	}
	freeMB = freeGB * 1024

	totalGB := usedGB + freeGB
	pct := 0
	if totalGB > 0 {
		pct = int(usedGB / totalGB * 100)
	}

	sessionsHTML := RenderSessions(sessions)

	storageWarningHTML := ""
	if freeMB < float64(s.config.MinFreeSpaceMB) {
		cleanupNotice := "Storage cleanup is disabled; free space before another transfer."
		if s.config.StoragePressureCleanup {
			cleanupNotice = "Automatic cleanup is enabled: the next transfer may delete your oldest saved sessions."
		}
		storageWarningHTML = fmt.Sprintf(
			`<div class="warning-card">Low space: %.1f MB free; reserve is %d MB. %s</div>`,
			freeMB, s.config.MinFreeSpaceMB, cleanupNotice,
		)
	}

	body := RenderIndex(IndexParams{
		UsedGB:             usedGB,
		FreeGB:             freeGB,
		Pct:                pct,
		SessionsHTML:       sessionsHTML,
		StorageWarningHTML: storageWarningHTML,
		CSRFToken:          s.csrfToken,
		Status:             lfSync.ReadStatus(s.config.StatusPath),
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
	status := lfSync.ReadStatus(s.config.StatusPath)
	payload := map[string]any{
		"state":        status.State,
		"message":      status.Message,
		"progress":     status.Progress,
		"bytes_copied": status.BytesCopied,
		"total_bytes":  status.TotalBytes,
		"speed_bps":    status.SpeedBPS,
		"eta_sec":      status.ETASec,
		// FC identity — empty strings until handshake completes.
		"fc_variant":          status.FCVariant,
		"fc_firmware_version": status.FCFirmwareVersion,
		"fc_api_version":      status.FCAPIVersion,
		"warning":             status.Warning,
	}
	s.addIdleShutdownInfo(payload)
	s.sendJSON(w, r, http.StatusOK, payload)
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	controller := http.NewResponseController(w)
	ticker := time.NewTicker(sseInterval)
	defer ticker.Stop()
	for {
		if err := controller.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil && !errors.Is(err, http.ErrNotSupported) {
			return
		}
		data, err := json.Marshal(lfSync.ReadStatus(s.config.StatusPath))
		if err != nil {
			return
		}
		if _, err = fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
			return
		}
		if err = controller.Flush(); err != nil {
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	status := lfSync.ReadStatus(s.config.StatusPath)
	sessions := s.getSessions()

	var usedGB, freeGB float64
	usedGB, freeGB, err := util.UsedAndFreeGB(s.storagePath)
	storageErr := err
	if err != nil {
		slog.Warn("disk stats", "error", err)
	}
	freeMB := freeGB * 1024

	hostapd := readHostapdConfigAt(s.hotspotPath)

	var thermalTemp float64
	thermalTemp, err = util.CPUTemperature()
	if err != nil {
		s.logThermalOnce("thermal", "error", err)
	}
	thermalWarning := thermalTemp > util.ThermalWarningThreshold
	if thermalWarning {
		s.logThermalOnce("thermal",
			"temp_c", round1(thermalTemp),
			"threshold_c", util.ThermalWarningThreshold,
			"message", "CPU temperature exceeds 80°C — possible throttling",
		)
	}

	payload := map[string]any{
		"ok":         storageErr == nil && status.State != "error" && status.State != "interrupted" && status.State != "unavailable",
		"uptime_sec": int(time.Since(s.startedAt).Seconds()),
		"status": map[string]any{
			"state":    status.State,
			"message":  status.Message,
			"progress": status.Progress,
		},
		"session_count": len(sessions),
		"storage": map[string]any{
			"used_gb":                  round2(usedGB),
			"free_gb":                  round2(freeGB),
			"free_mb":                  round1(freeMB),
			"reserve_mb":               s.config.MinFreeSpaceMB,
			"storage_pressure_cleanup": s.config.StoragePressureCleanup,
			"low_space":                freeMB < float64(s.config.MinFreeSpaceMB),
		},
		"hotspot": map[string]any{
			"ssid":                    hostapd["ssid"],
			"default_password_in_use": hostapd["wpa_passphrase"] == defaultHotspotPassword,
		},
		"thermal": map[string]any{
			"temp_c":      round1(thermalTemp),
			"warning":     thermalWarning,
			"threshold_c": util.ThermalWarningThreshold,
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

	f, err := storage.OpenSessionFile(s.storagePath, sessionID, filename)
	if err != nil {
		s.sendError(w, r, http.StatusNotFound, "")
		return
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		s.sendError(w, r, http.StatusNotFound, "")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Header().Set("ETag", fmt.Sprintf(`"%x-%x"`, info.ModTime().UnixNano(), info.Size()))
	// ServeContent handles suffix/multipart ranges, HEAD and conditional requests.
	http.ServeContent(w, r, filename, info.ModTime(), f)
}

func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-CSRF-Token") != s.csrfToken {
		s.sendError(w, r, http.StatusForbidden, "Invalid CSRF token")
		return
	}

	sessionID := strings.TrimPrefix(r.URL.Path, "/sessions/")
	if err := storage.DeleteSession(s.storagePath, sessionID); err != nil {
		s.sendError(w, r, http.StatusBadRequest, "")
		return
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

	m := settings.Manager{Hostapd: s.hotspotPath, Config: s.configPath, Restart: s.restartHotspot}
	if err := m.Apply(settings.Values{SSID: ssid, Password: password}); err != nil {
		slog.Warn("settings transaction failed", "error", err)
		s.sendHTML(w, r, http.StatusInternalServerError, s.renderSettingsPage("Settings could not be applied. Recovery will be retried before another change; check diagnostics.", true))
		return
	}
	msg := fmt.Sprintf("Settings saved for hotspot %s. Wi-Fi restarted; reconnect using the new settings.", ssid)
	slog.Info("hotspot settings updated", "ssid", ssid, "client", r.RemoteAddr)
	s.sendHTML(w, r, http.StatusOK, s.renderSettingsPage(msg, false))
}

// ---------- settings helpers ----------

func (s *Server) renderSettingsPage(message string, isError bool) string {
	hostapd := readHostapdConfigAt(s.hotspotPath)
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
		if err := gz.Close(); err != nil {
			slog.Warn("gzip close failed", "error", err)
		}
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
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for now := range ticker.C {
		if s.checkIdleShutdown(now) {
			return
		}
	}
}

// ---------- hostapd config ----------

func readHostapdConfigAt(path string) map[string]string {
	data, err := os.ReadFile(path)
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

func validateHotspotValue(value string, min, max int, label string) string {
	if len(value) < min || len(value) > max {
		return fmt.Sprintf("%s must be %d–%d characters.", label, min, max)
	}
	for _, ch := range value {
		if ch < 32 || ch > 126 {
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

// logThermalOnce rate-limits thermal-related warnings to once per 10 minutes.
func (s *Server) logThermalOnce(msg string, args ...any) {
	s.thermalLogMu.Lock()
	if time.Since(s.lastThermalLog) < 10*time.Minute {
		s.thermalLogMu.Unlock()
		return
	}
	s.lastThermalLog = time.Now()
	s.thermalLogMu.Unlock()
	slog.Warn(msg, args...)
}

// A successful exit confirms the restart job completed, rather than merely queued.
func restartHotspot() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "systemctl", "restart", "hostapd.service").Run()
}

func (s *Server) checkIdleShutdown(now time.Time) bool {
	s.lastActivityLock.Lock()
	defer s.lastActivityLock.Unlock()
	status := lfSync.ReadStatus(s.config.StatusPath)
	if status.Active() || status.State == "unavailable" || s.activeRequests > 0 || s.lastTransfer != status.TransferID {
		s.lastActivity = now
		s.lastTransfer = status.TransferID
		return false
	}
	if s.config.IdleShutdownMinutes <= 0 || now.Sub(s.lastActivity) < time.Duration(s.config.IdleShutdownMinutes)*time.Minute {
		return false
	}
	if err := lfSync.ScheduleShutdown(filepath.Dir(s.config.StatusPath), s.shutdown); err != nil {
		slog.Error("idle shutdown failed", "error", err)
		s.lastActivity = now
		return false
	}
	return true
}
