package main

import (
	"testing"
)

// TestCmdMCPAuthNoServerFlag verifies that the --server flag is required.
func TestCmdMCPAuthNoServerFlag(t *testing.T) {
	origExit := osExit
	defer func() { osExit = origExit }()

	var exitCode int
	osExit = func(code int) {
		exitCode = code
		panic("os.Exit called")
	}

	func() {
		defer func() { recover() }()
		cmdMCPAuth([]string{})
	}()

	if exitCode != 1 {
		t.Errorf("expected exit code 1, got %d", exitCode)
	}
}

// TestCmdMCPAuthUnknownServer verifies that an unknown server name shows an error.
func TestCmdMCPAuthUnknownServer(t *testing.T) {
	origExit := osExit
	defer func() { osExit = origExit }()

	osExit = func(code int) {
		if code != 1 {
			t.Errorf("expected exit code 1, got %d", code)
		}
		panic("os.Exit called")
	}

	func() {
		defer func() { recover() }()
		cmdMCPAuth([]string{"--server", "nonexistent-server"})
	}()
}
