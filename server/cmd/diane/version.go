package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// readAppVersion reads the CFBundleShortVersionString from Diane.app's Info.plist.
// Returns empty string if the app bundle is not installed or cannot be read.
func readAppVersion() string {
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
	// Strip leading "v" if present so we can format consistently
	cliVer = strings.TrimPrefix(cliVer, "v")

	// Build display string — "v" prefix for semver, plain for "dev"
	displayVer := "v" + cliVer
	jsonVer := displayVer
	if cliVer == "dev" {
		displayVer = "dev (local build)"
		jsonVer = "dev"
	}

	type versionInfo struct {
		CLI     string `json:"cli"`
		App     string `json:"app,omitempty"`
		AppPath string `json:"app_path,omitempty"`
	}

	info := versionInfo{CLI: jsonVer}

	// Check for companion app in standard locations
	if appVer := readAppVersion(); appVer != "" {
		cleaned := strings.TrimPrefix(appVer, "v")
		info.App = "v" + cleaned
		// Find the app path
		for _, appPath := range []string{
			"/Applications/Diane.app",
			filepath.Join(os.Getenv("HOME"), "Applications", "Diane.app"),
		} {
			plist := filepath.Join(appPath, "Contents", "Info.plist")
			if _, err := os.Stat(plist); err == nil {
				info.AppPath = appPath
				break
			}
		}
	}

	if jsonOutput {
		emitJSON("ok", info)
		return
	}

	fmt.Printf("diane CLI   : %s\n", displayVer)
	if info.App != "" {
		fmt.Printf("companion   : %s (%s)\n", info.App, info.AppPath)
	} else {
		fmt.Println("companion   : (not installed)")
	}
}
