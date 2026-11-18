package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"context"
	"github.com/proeugene/logfalcon/internal/config"
	"github.com/proeugene/logfalcon/internal/diagnostics"
	"github.com/proeugene/logfalcon/internal/led"
	"github.com/proeugene/logfalcon/internal/settings"
	lfsync "github.com/proeugene/logfalcon/internal/sync"
	"github.com/proeugene/logfalcon/internal/update"
	"github.com/proeugene/logfalcon/internal/web"
	"os/exec"
)

// Build-time variables injected via ldflags.
var (
	Version     = "dev"
	BuildCommit = "unknown"
)

func main() {
	var (
		webMode          bool
		serialPort       string
		configPath       string
		showVersion      bool
		dryRun           bool
		diagnosticAction string
		ledStatus        bool
		applyBoot        bool
		checkUpgrade     bool
		updateWorker     bool
	)

	flag.BoolVar(&webMode, "web", false, "Run in web server mode")
	flag.StringVar(&serialPort, "port", "", "Serial port path for sync mode (e.g. /dev/ttyACM0)")
	flag.StringVar(&configPath, "config", "", "Path to config file (default: /etc/logfalcon/logfalcon.toml)")
	flag.BoolVar(&showVersion, "version", false, "Print version and exit")
	flag.BoolVar(&dryRun, "dry-run", false, "Sync without erasing FC flash")
	flag.StringVar(&diagnosticAction, "diagnostics", "", "Diagnostics action: start, export, stop")
	flag.BoolVar(&ledStatus, "led-status", false, "Drive the Pi LED from shared transfer status")
	flag.BoolVar(&applyBoot, "apply-boot-config", false, "Recover settings and apply boot configuration")
	flag.BoolVar(&updateWorker, "update-worker", false, "Run privileged signed update activation and recovery")
	flag.BoolVar(&checkUpgrade, "check-upgrade-config", false, "Validate existing configuration before bootstrap; no changes")
	flag.Parse()
	if checkUpgrade {
		app := configPath
		if app == "" {
			app = "/etc/logfalcon/logfalcon.toml"
		}
		host := filepath.Join(filepath.Dir(app), "hostapd.conf")
		if _, err := os.Stat(host); os.IsNotExist(err) {
			host = "/etc/hostapd/hostapd.conf"
		}
		if err := update.CheckConfiguration(app, host); err != nil {
			slog.Error("upgrade configuration rejected", "error", err)
			os.Exit(1)
		}
		return
	}
	if updateWorker {
		if err := update.System(Version).Run(); err != nil {
			slog.Error("update failed", "error", err)
			os.Exit(1)
		}
		return
	}

	if showVersion {
		fmt.Printf("logfalcon %s (%s)\n", Version, BuildCommit)
		os.Exit(0)
	}

	if applyBoot {
		bootFile := "/boot/firmware/logfalcon-config.txt"
		if _, err := os.Stat(bootFile); os.IsNotExist(err) {
			bootFile = "/boot/logfalcon-config.txt"
		}
		m := settings.Manager{Hostapd: "/etc/logfalcon/hostapd.conf", Config: "/etc/logfalcon/logfalcon.toml", Restart: func() error {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			return exec.CommandContext(ctx, "systemctl", "restart", "hostapd.service").Run()
		}}
		if err := m.ApplyBoot(bootFile); err != nil {
			slog.Error("boot settings failed", "error", err)
			os.Exit(1)
		}
		if err := m.InitialiseDevice(); err != nil {
			slog.Error("device identity failed", "error", err)
			os.Exit(1)
		}
		return
	}

	if !webMode && serialPort == "" && diagnosticAction == "" && !ledStatus {
		fmt.Fprintln(os.Stderr, "error: specify --web or --port <path>")
		flag.Usage()
		os.Exit(1)
	}

	cfg, err := config.Load(configPath)
	if diagnosticAction != "" {
		if diagnosticAction != "start" && diagnosticAction != "stop" && diagnosticAction != "export" {
			slog.Error("invalid diagnostics action")
			os.Exit(1)
		}
		diagnostics.ConfigureLogging(cfg != nil && cfg.DebugLogging, Version, BuildCommit)
		if err != nil {
			slog.Warn("configuration unavailable for diagnostics", "error", err)
		}
		if diagnosticAction == "start" || diagnosticAction == "stop" {
			if err := diagnostics.Lifecycle("/var/lib/logfalcon", diagnosticAction, diagnostics.BootID()); err != nil {
				slog.Error("diagnostic lifecycle failed", "error", err)
				os.Exit(1)
			}
		}
		// Boot exports are queued by the service, so reading journal history never
		// delays startup or a controller already connected during boot.
		if diagnosticAction == "start" {
			return
		}
		bootDir, err := diagnostics.BootPartition()
		if err == nil {
			err = diagnostics.Export(diagnostics.Options{BootDir: bootDir, StateDir: "/var/lib/logfalcon", Config: cfg, Version: Version, Commit: BuildCommit})
		}
		if err != nil {
			slog.Warn("diagnostic export skipped", "error", err)
			os.Exit(1)
		}
		return
	}
	if err != nil {
		slog.Error("configuration rejected; no transfer started", "error", err)
		if ledStatus {
			runErrorLED()
		}
		os.Exit(1)
	}
	diagnostics.ConfigureLogging(cfg.DebugLogging, Version, BuildCommit)
	lfsync.ConfigureStatus(cfg.StatusPath)
	if ledStatus {
		runStatusLED(cfg)
		return
	}

	// Only the dedicated privileged LED service accesses sysfs. Copy/web processes
	// run unprivileged and communicate their outcome through the status file.
	ledCtrl := led.New("none", cfg.LEDGPIOPin)
	ledCtrl.Start()
	defer ledCtrl.Stop()

	if webMode {
		slog.Info("starting web server", "port", cfg.WebPort, "version", Version)
		srv := web.NewServer(cfg.StoragePath, cfg)
		srv.Version = Version
		srv.BuildCommit = BuildCommit
		addr := fmt.Sprintf("0.0.0.0:%d", cfg.WebPort)
		if err := srv.ListenAndServe(addr); err != nil {
			slog.Error("web server failed", "error", err)
			os.Exit(1)
		}
		return
	}

	slog.Info("starting sync", "port", serialPort, "version", Version)
	ledCtrl.SetState(led.Busy)
	orch := &lfsync.Orchestrator{
		Config: cfg,
		LED:    ledCtrl,
		DryRun: dryRun,
	}
	result := orch.Run(serialPort)
	switch result {
	case lfsync.ResultSuccess:
		slog.Info("sync complete")
	case lfsync.ResultAlreadyEmpty:
		slog.Info("flash already empty")
	case lfsync.ResultDryRun:
		slog.Info("dry run complete")
	case lfsync.ResultError:
		slog.Error("sync failed")
		os.Exit(1)
	}
}

func runStatusLED(cfg *config.Config) {
	c := led.New(cfg.LEDBackend, cfg.LEDGPIOPin)
	c.Start()
	defer c.Stop()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(signals)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	previous := led.Off
	for {
		status := lfsync.GetStatus()
		state := led.Busy
		switch status.State {
		case "idle", "empty":
			state = led.Ready
		case "waiting":
			state = led.Booting
		case "complete":
			state = led.Saved
		case "error", "interrupted", "unavailable":
			state = led.Error
		}
		if state != previous {
			c.SetState(state)
			previous = state
			slog.Info("LED outcome changed", "state", status.State)
		}
		select {
		case <-signals:
			return
		case <-ticker.C:
		}
	}
}

// Invalid configuration must still produce an error signal in the root LED service.
func runErrorLED() {
	c := led.New("sysfs", 17)
	c.Start()
	defer c.Stop()
	c.SetState(led.Error)
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(stop)
	<-stop
}
