package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// readInstalledAppVersion reads CFBundleShortVersionString from Diane.app's Info.plist.
// Returns empty string if the app bundle is not installed or cannot be read.
func readInstalledAppVersion() string {
	for _, appPath := range []string{
		"/Applications/Diane.app",
		filepath.Join(os.Getenv("HOME"), "Applications", "Diane.app"),
	} {
		plist := filepath.Join(appPath, "Contents", "Info.plist")
		if _, err := os.Stat(plist); err != nil {
			continue
		}
		out, err := exec.Command(
			"/usr/libexec/PlistBuddy",
			"-c", "Print CFBundleShortVersionString",
			plist,
		).Output()
		if err == nil {
			return strings.TrimSpace(string(out))
		}
		break
	}
	return ""
}

// cmdVersion prints the CLI version and companion app version (if installed).
func cmdVersion() {
	cliVer := Version
	if cliVer == "" {
		cliVer = "dev"
	}
	cliVer = strings.TrimPrefix(cliVer, "v")

	displayVer := "v" + cliVer
	if cliVer == "dev" {
		displayVer = "dev (local build)"
	}

	fmt.Printf("diane CLI: %s\n", displayVer)

	// Check for companion app
	if appVer := readInstalledAppVersion(); appVer != "" {
		fmt.Printf("companion: v%s (%s)\n", strings.TrimPrefix(appVer, "v"), appVer)
	}
}
