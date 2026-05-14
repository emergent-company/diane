package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// lockFile is kept open for the lifetime of the process so the flock
// is automatically released on process death (even SIGKILL).
var lockFile *os.File

// acquirePIDLock acquires an exclusive file lock on the given path.
// Uses flock(LOCK_EX|LOCK_NB) for atomic acquisition — if another instance
// holds the lock, this exits with a fatal message.
//
// The lock is automatically released when the process exits (even on crash
// or SIGKILL), so systemd restarts work seamlessly.
//
// If path is empty, the lock is skipped (for testing / force-start).
func acquirePIDLock(path string) {
	if path == "" {
		return
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Fatalf("[LOCK] Cannot create lock directory %s: %v", dir, err)
	}

	var err error
	lockFile, err = os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		log.Fatalf("[LOCK] Cannot open lock file %s: %v", path, err)
	}

	// Try to acquire exclusive lock, non-blocking
	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		// Another instance holds the lock — but verify the PID is actually alive
		lockFile.Close()
		lockFile = nil

		// Read the PID for a helpful error message
		existing := "unknown"
		if data, readErr := os.ReadFile(path); readErr == nil {
			existing = strings.TrimSpace(string(data))
		}

		// Check if the owning PID is actually alive (prevents stale locks after crash)
		if existing != "unknown" {
			existingPID := 0
			if _, scanErr := fmt.Sscanf(existing, "%d", &existingPID); scanErr == nil && existingPID > 0 {
				// Try signaling PID 0 to check if alive
				if proc, findErr := os.FindProcess(existingPID); findErr == nil {
					if signalErr := proc.Signal(syscall.Signal(0)); signalErr != nil {
						// Process is dead — remove stale lock and retry
						log.Printf("[LOCK] Stale lock from dead PID %d, removing and retrying...", existingPID)
						os.Remove(path)
						lockFile.Close() // close the fd we opened
						lockFile = nil
						acquirePIDLock(path) // recursive retry
						return
					}
				}
			}
		}

		log.Fatalf(
			"\n[LOCK] Another instance is already running (PID %s).\n"+
				"       Stop it first:\n"+
				"         systemctl stop diane   (if managed by systemd)\n"+
				"         kill %s                (to force kill)\n"+
				"       To bypass this guard, start with --pidfile \"\"\n",
			existing, existing,
		)
	}

	// Write our PID to the file (truncate first)
	if err := lockFile.Truncate(0); err != nil {
		log.Printf("[LOCK] Warning: failed to truncate lock file: %v", err)
	}
	if _, err := lockFile.WriteString(fmt.Sprintf("%d", os.Getpid())); err != nil {
		log.Printf("[LOCK] Warning: failed to write PID: %v", err)
	}
	lockFile.Sync()

	log.Printf("[LOCK] PID lock acquired: %d → %s", os.Getpid(), path)
}

// releasePIDLock explicitly releases the lock. This is called on clean exit
// but is not strictly necessary — the OS releases the lock when the process dies.
func releasePIDLock() {
	if lockFile != nil {
		syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
		lockFile.Close()
		lockFile = nil
	}
}
