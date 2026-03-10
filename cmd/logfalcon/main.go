package main

import (
	"flag"
	"fmt"
	"os"
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
	)

	flag.BoolVar(&webMode, "web", false, "Run in web server mode")
	flag.StringVar(&serialPort, "port", "", "Serial port path for sync mode (e.g. /dev/ttyACM0)")
	flag.StringVar(&configPath, "config", "", "Path to config file (default: /etc/logfalcon/logfalcon.toml)")
	flag.BoolVar(&showVersion, "version", false, "Print version and exit")
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

	if webMode {
		fmt.Println("logfalcon web server starting...")
		// TODO: initialize config, LED, web server
		os.Exit(0)
	}

	fmt.Printf("logfalcon sync on %s...\n", serialPort)
	// TODO: initialize config, LED, orchestrator
}
