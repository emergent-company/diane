package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// cmdService manages the MCP relay as a background service (macOS/Linux).
//
// Usage: diane service <start|stop|status|restart> [--instance <name>]
func cmdService(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: diane service <start|stop|status|restart> [--instance <name>]")
		os.Exit(1)
	}

	action := args[0]
	instance := ""
	for i := 1; i < len(args); i++ {
		if args[i] == "--instance" && i+1 < len(args) {
			instance = args[i+1]
			i++
		}
	}
	if instance == "" {
		instance = "diane"
	}

	home, _ := os.UserHomeDir()
	dianeDir := filepath.Join(home, ".diane")
	pidFile := filepath.Join(dianeDir, fmt.Sprintf("%s.pid", instance))
	logFile := filepath.Join(dianeDir, fmt.Sprintf("%s.log", instance))
	binary, _ := os.Executable()

	switch action {
	case "start":
		cmdServiceStart(instance, pidFile, logFile, binary)
	case "stop":
		cmdServiceStop(instance, pidFile)
	case "status":
		cmdServiceStatus(instance, pidFile)
	case "restart":
		cmdServiceStop(instance, pidFile)
		cmdServiceStart(instance, pidFile, logFile, binary)
	default:
		fmt.Fprintf(os.Stderr, "Unknown action: %s\n", action)
		fmt.Println("Usage: diane service <start|stop|status|restart> [--instance <name>]")
		os.Exit(1)
	}
}

func cmdServiceStart(instance, pidFile, logFile, binary string) {
	// Check if already running
	if pid, err := readPID(pidFile); err == nil {
		if isRunning(pid) {
			fmt.Printf("✅ %s is already running (PID %d)\n", instance, pid)
			return
		}
		fmt.Printf("⚠️  Stale PID file found (PID %d not running), starting...\n", pid)
	}

	os.MkdirAll(filepath.Dir(pidFile), 0755)

	// Start the relay in background
	cmd := exec.Command(binary, "mcp", "relay", "--instance", instance)
	logF, err := os.Create(logFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Failed to create log file: %v\n", err)
		os.Exit(1)
	}
	cmd.Stdout = logF
	cmd.Stderr = logF

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "❌ Failed to start %s: %v\n", instance, err)
		os.Exit(1)
	}

	// Write PID
	pid := cmd.Process.Pid
	if err := os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", pid)), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "⚠️  Failed to write PID file: %v\n", err)
	}

	fmt.Printf("✅ %s started (PID %d)\n", instance, pid)
	fmt.Printf("   Log: %s\n", logFile)

	// Detach — don't wait
	go cmd.Wait()
}

func cmdServiceStop(instance, pidFile string) {
	pid, err := readPID(pidFile)
	if err != nil {
		fmt.Printf("⚠️  %s is not running (no PID file)\n", instance)
		return
	}

	if !isRunning(pid) {
		fmt.Printf("⚠️  %s was not running (stale PID %d)\n", instance, pid)
		os.Remove(pidFile)
		return
	}

	// Try graceful shutdown first
	killCmd := exec.Command("kill", "-TERM", strconv.Itoa(pid))
	killCmd.Run()

	// Wait a moment, then force kill if still running
	if isRunning(pid) {
		exec.Command("sleep", "2").Run()
		if isRunning(pid) {
			exec.Command("kill", "-KILL", strconv.Itoa(pid)).Run()
		}
	}

	os.Remove(pidFile)
	fmt.Printf("✅ %s stopped\n", instance)
}

func cmdServiceStatus(instance, pidFile string) {
	pid, err := readPID(pidFile)
	if err != nil {
		fmt.Printf("❌ %s is not running\n", instance)
		return
	}

	if isRunning(pid) {
		fmt.Printf("✅ %s is running (PID %d)\n", instance, pid)
	} else {
		fmt.Printf("❌ %s is not running (stale PID %d)\n", instance, pid)
		os.Remove(pidFile)
	}
}

// readPID reads a PID from a file.
func readPID(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(data)))
}

// isRunning checks if a process with the given PID exists.
func isRunning(pid int) bool {
	cmd := exec.Command("kill", "-0", strconv.Itoa(pid))
	return cmd.Run() == nil
}
