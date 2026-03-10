package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/proeugene/logfalcon/internal/config"
	"github.com/proeugene/logfalcon/internal/led"
	lfsync "github.com/proeugene/logfalcon/internal/sync"
	"github.com/proeugene/logfalcon/internal/web"
)

// Build-time variables injected via ldflags.
var (
	Version     = "dev"
	BuildCommit = "unknown"
)

func main() {
	var (
		webMode     bool
		serialPort  string
		configPath  string
		showVersion bool
		dryRun      bool
	)

	flag.BoolVar(&webMode, "web", false, "Run in web server mode")
	flag.StringVar(&serialPort, "port", "", "Serial port path for sync mode (e.g. /dev/ttyACM0)")
	flag.StringVar(&configPath, "config", "", "Path to config file (default: /etc/logfalcon/logfalcon.toml)")
	flag.BoolVar(&showVersion, "version", false, "Print version and exit")
	flag.BoolVar(&dryRun, "dry-run", false, "Sync without erasing FC flash")
	flag.Parse()

	if showVersion {
		fmt.Printf("logfalcon %s (%s)\n", Version, BuildCommit)
		os.Exit(0)
	}

	if !webMode && serialPort == "" {
		fmt.Fprintln(os.Stderr, "error: specify --web or --port <path>")
		flag.Usage()
		os.Exit(1)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		slog.Warn("config load failed, using defaults", "error", err)
		cfg = config.Default()
	}

	ledCtrl := led.New(cfg.LEDBackend, cfg.LEDGPIOPin)
	ledCtrl.Start()
	defer ledCtrl.Stop()

	if webMode {
		slog.Info("starting web server", "port", cfg.WebPort, "version", Version)
		srv := web.NewServer(cfg.StoragePath, cfg)
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
