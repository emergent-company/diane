package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/Emergent-Comapny/diane/internal/mcpproxy"
)

func cmdMCPReload(args []string) {
	fs := flag.NewFlagSet("reload", flag.ExitOnError)
	configPath := fs.String("config", "", "Path to MCP servers config (default: ~/.diane/mcp-servers.json)")
	fs.Parse(args)

	// Try to signal the relay process via PID file
	home, err := os.UserHomeDir()
	if err == nil {
		pidFile := filepath.Join(home, ".diane", "mcp.pid")
		if data, err := os.ReadFile(pidFile); err == nil {
			var pid int
			if _, err := fmt.Sscanf(string(data), "%d", &pid); err == nil && pid > 0 {
				process, err := os.FindProcess(pid)
				if err == nil {
					if err := process.Signal(syscall.SIGUSR1); err == nil {
						fmt.Printf("🔄 Reload signal sent to relay (PID %d)\n", pid)
						return
					}
					fmt.Fprintf(os.Stderr, "⚠️  Could not send signal to PID %d: %v\n", pid, err)
				}
			}
		}
	}

	// Fallback: try to find and signal diane serve process
	// Check serve.pid too
	if home != "" {
		pidFile := filepath.Join(home, ".diane", "serve.pid")
		if data, err := os.ReadFile(pidFile); err == nil {
			var pid int
			if _, err := fmt.Sscanf(string(data), "%d", &pid); err == nil && pid > 0 {
				process, err := os.FindProcess(pid)
				if err == nil {
					if err := process.Signal(syscall.SIGUSR1); err == nil {
						fmt.Printf("🔄 Reload signal sent to diane serve (PID %d)\n", pid)
						return
					}
				}
			}
		}
	}

	// No relay running — try reloading the config in-memory if we're running locally
	// This is a no-op if there's no process to signal
	path := *configPath
	if path == "" {
		path = mcpproxy.GetDefaultConfigPath()
	}

	// Check if config exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "❌ No relay process found and no config at %s\n", path)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "⚠️  No running relay process found.\n")
	fmt.Fprintf(os.Stderr, "   Config exists at %s — start the relay with 'diane serve' or 'diane mcp relay'\n", path)
	os.Exit(1)
}
