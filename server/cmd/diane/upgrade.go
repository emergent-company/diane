package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// companionAppPath is where Diane.app lives on macOS.
const companionAppPath = "/Applications/Diane.app"

// releaseAsset represents a single file asset in a GitHub release.
type releaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// upgradeState tracks binary upgrades for rollback.
type upgradeState struct {
	CurrentVersion string `json:"current_version"`
	PreviousFile   string `json:"previous_file"` // path to diane.prev
	Timestamp      string `json:"timestamp"`
}

// cmdUpgrade checks for a new version and upgrades everything.
// Flags: --check, --json, --auto
// Subcommands: rollback
func cmdUpgrade(args []string) {
	if len(args) > 0 && args[0] == "rollback" {
		cmdUpgradeRollback(args[1:])
		return
	}

	checkOnly := false
	jsonOutput := false
	autoMode := false

	for _, a := range args {
		switch a {
		case "--check":
			checkOnly = true
		case "--json":
			jsonOutput = true
		case "--auto":
			autoMode = true
		}
	}

	home, _ := os.UserHomeDir()
	dianeDir := filepath.Join(home, ".diane")
	binDir := filepath.Join(dianeDir, "bin")
	symlinkPath := filepath.Join(binDir, "diane")

	currentVer := strings.TrimPrefix(Version, "v")
	repo := "emergent-company/diane"
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)

	// Fetch latest release
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		exitErr("Failed to create request: %v", err)
	}
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		exitErr("Failed to fetch latest release: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		exitErr("No releases found — the repo may be private. Set GITHUB_TOKEN env var.")
	}
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		exitErr("GitHub API returned %d: %s", resp.StatusCode, string(body))
	}

	var release struct {
		TagName string         `json:"tag_name"`
		Assets  []releaseAsset `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		exitErr("Failed to parse release: %v", err)
	}

	latestVer := strings.TrimPrefix(release.TagName, "v")
	needsBinaryUpdate := currentVer != latestVer && currentVer != "dev"

	// --json mode: output structured data and exit
	if jsonOutput {
		out := map[string]any{
			"current_version": currentVer,
			"latest_version":  latestVer,
			"update_available": needsBinaryUpdate,
		}
		b, _ := json.Marshal(out)
		fmt.Println(string(b))
		return
	}

	// --check mode: print human-readable and exit
	if checkOnly {
		fmt.Printf("Current version: v%s\n", currentVer)
		fmt.Printf("Latest version:  v%s\n", latestVer)
		if needsBinaryUpdate {
			fmt.Println("Update available!")
		} else {
			fmt.Println("Up to date.")
		}
		return
	}

	// --auto mode: non-interactive upgrade with serve restart
	if autoMode {
		if !needsBinaryUpdate {
			return // silently up to date
		}
		autoUpgrade(release.TagName, release.Assets, symlinkPath, binDir)
		return
	}

	// Default: interactive mode
	fmt.Printf("📦 Current version: v%s\n", currentVer)
	fmt.Printf("📦 Latest version:  v%s\n", latestVer)

	if !needsBinaryUpdate {
		fmt.Println("✅ CLI binary already up to date!")
	} else {
		performBinaryUpdate(release.TagName, release.Assets, symlinkPath, binDir)
	}

	// On macOS, also check companion app
	if runtime.GOOS == "darwin" {
		checkCompanionApp(release.TagName, release.Assets)
	}
}

// cmdUpgradeRollback restores the previous binary and restarts serve.
func cmdUpgradeRollback(_ []string) {
	home, _ := os.UserHomeDir()
	stateFile := filepath.Join(home, ".diane", "upgrade.json")
	prevFile := filepath.Join(home, ".diane", "bin", "diane.prev")
	activeFile := filepath.Join(home, ".diane", "bin", "diane")

	// Read state
	data, err := os.ReadFile(stateFile)
	if err != nil {
		exitErr("No upgrade state found — cannot rollback: %v", err)
	}
	var state upgradeState
	json.Unmarshal(data, &state)

	if _, err := os.Stat(prevFile); os.IsNotExist(err) {
		exitErr("No backup binary at %s", prevFile)
	}

	fmt.Printf("⏪ Rolling back from v%s to v%s...\n", state.CurrentVersion, state.PreviousFile)

	// Get the resolved target (follow symlinks)
	realTarget := resolveBinaryTarget(activeFile)

	input, err := os.ReadFile(prevFile)
	if err != nil {
		exitErr("Failed to read backup binary: %v", err)
	}

	if err := os.WriteFile(realTarget, input, 0755); err != nil {
		exitErr("Failed to write rollback binary: %v", err)
	}

	fmt.Printf("   Binary: %s\n", realTarget)
	os.Remove(prevFile)
	os.Remove(stateFile)

	fmt.Println("✅ Rollback complete. Restarting serve...")
	restartServeProcess()
}

// autoUpgrade performs a non-interactive binary upgrade.
// Downloads, verifies, swaps, and restarts serve.
func autoUpgrade(tagName string, assets []releaseAsset, symlinkPath, binDir string) {
	latestVer := strings.TrimPrefix(tagName, "v")
	fmt.Printf("⬆️  Auto-upgrading to v%s...\n", latestVer)

	goos := runtime.GOOS
	goarch := runtime.GOARCH
	if goarch == "aarch64" {
		goarch = "arm64"
	}
	assetName := fmt.Sprintf("diane-%s-%s.tar.gz", goos, goarch)

	var binaryAsset releaseAsset
	for _, a := range assets {
		if a.Name == assetName {
			binaryAsset = a
			break
		}
	}
	if binaryAsset.Name == "" {
		fmt.Printf("   Skipping binary upgrade — no asset for %s/%s\n", goos, goarch)
		return
	}

	installBinary(binaryAsset.BrowserDownloadURL, binaryAsset.Name, symlinkPath, binDir)
	fmt.Printf("✅ CLI binary upgraded to v%s\n", latestVer)

	// Write state for rollback
	home, _ := os.UserHomeDir()
	state := upgradeState{
		CurrentVersion: latestVer,
		PreviousFile:   filepath.Join(home, ".diane", "bin", "diane.prev"),
		Timestamp:      time.Now().UTC().Format(time.RFC3339),
	}
	stateData, _ := json.Marshal(state)
	os.WriteFile(filepath.Join(home, ".diane", "upgrade.json"), stateData, 0644)

	// Restart serve process so the new binary takes effect
	restartServeProcess()
	fmt.Printf("✅ Diane serve restarted with v%s\n", latestVer)
}

// performBinaryUpdate is the interactive binary download+swap flow.
func performBinaryUpdate(tagName string, assets []releaseAsset, symlinkPath, binDir string) {
	latestVer := strings.TrimPrefix(tagName, "v")
	goos := runtime.GOOS
	goarch := runtime.GOARCH
	if goarch == "aarch64" {
		goarch = "arm64"
	}
	assetName := fmt.Sprintf("diane-%s-%s.tar.gz", goos, goarch)

	var binaryAsset releaseAsset
	for _, a := range assets {
		if a.Name == assetName {
			binaryAsset = a
			break
		}
	}
	if binaryAsset.Name == "" {
		fmt.Fprintf(os.Stderr, "❌ No asset found for %s\n", assetName)
		fmt.Fprintf(os.Stderr, "   Available: ")
		for _, a := range assets {
			fmt.Fprintf(os.Stderr, "%s ", a.Name)
		}
		fmt.Fprintln(os.Stderr)
		os.Exit(1)
	}

	installBinary(binaryAsset.BrowserDownloadURL, binaryAsset.Name, symlinkPath, binDir)
	fmt.Printf("✅ CLI binary upgraded to v%s\n", latestVer)

	// Write state for rollback
	home, _ := os.UserHomeDir()
	state := upgradeState{
		CurrentVersion: latestVer,
		PreviousFile:   filepath.Join(home, ".diane", "bin", "diane.prev"),
		Timestamp:      time.Now().UTC().Format(time.RFC3339),
	}
	stateData, _ := json.Marshal(state)
	os.WriteFile(filepath.Join(home, ".diane", "upgrade.json"), stateData, 0644)

	// Ask about restarting serve
	fmt.Print("\n   Restart diane serve with the new binary? [Y/n]: ")
	var response string
	fmt.Scanln(&response)
	if response != "n" && response != "N" && response != "no" {
		restartServeProcess()
		fmt.Println("✅ diane serve restarted with new binary")
	}

	// Write DMG trigger for macOS companion app
	if runtime.GOOS == "darwin" {
		writeDMGTrigger(tagName)
	}
}

// installBinary downloads the tarball, extracts it, and writes the binary
// to the correct location — handling symlinks to the app bundle on macOS.
func installBinary(downloadURL, assetName, symlinkPath, binDir string) {
	fmt.Printf("⬇️  Downloading %s...\n", assetName)

	dlReq, err := http.NewRequest("GET", downloadURL, nil)
	if err != nil {
		exitErr("%v", err)
	}
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		dlReq.Header.Set("Authorization", "Bearer "+token)
	}

	dlResp, err := http.DefaultClient.Do(dlReq)
	if err != nil {
		exitErr("Download failed: %v", err)
	}
	defer dlResp.Body.Close()

	if dlResp.StatusCode != 200 {
		exitErr("Download returned %d", dlResp.StatusCode)
	}

	tmpDir, err := os.MkdirTemp("", "diane-upgrade")
	if err != nil {
		exitErr("%v", err)
	}
	defer os.RemoveAll(tmpDir)

	tmpTarball := filepath.Join(tmpDir, assetName)
	f, err := os.Create(tmpTarball)
	if err != nil {
		exitErr("%v", err)
	}
	if _, err := io.Copy(f, dlResp.Body); err != nil {
		f.Close()
		exitErr("%v", err)
	}
	f.Close()

	extractCmd := exec.Command("tar", "xzf", tmpTarball, "-C", tmpDir)
	if out, err := extractCmd.CombinedOutput(); err != nil {
		exitErr("Extraction failed: %v\n%s", err, string(out))
	}

	srcBinary := filepath.Join(tmpDir, "diane")
	input, err := os.ReadFile(srcBinary)
	if err != nil {
		exitErr("Failed to read extracted binary: %v", err)
	}

	// Resolve the actual install target, handling app-bundle symlinks.
	realTarget := resolveBinaryTarget(symlinkPath)

	// Back up the current binary before overwriting
	backupPath := filepath.Join(binDir, "diane.prev")
	if _, err := os.Stat(realTarget); err == nil {
		currentData, err := os.ReadFile(realTarget)
		if err == nil {
			os.WriteFile(backupPath, currentData, 0755)
		}
	}

	os.MkdirAll(filepath.Dir(realTarget), 0755)
	if err := os.WriteFile(realTarget, input, 0755); err != nil {
		exitErr("Failed to install binary: %v", err)
	}
	fmt.Printf("   Binary: %s\n", realTarget)

	// On macOS, re-sign to avoid Killed:9
	if runtime.GOOS == "darwin" {
		resignMacOSBinary(realTarget)
	}

	// Re-create symlinks if we were in bundle mode
	home, _ := os.UserHomeDir()
	if strings.Contains(realTarget, "/Applications/Diane.app") && symlinkPath != realTarget {
		os.Remove(symlinkPath)
		os.MkdirAll(binDir, 0755)
		os.Symlink(realTarget, symlinkPath)
		fmt.Printf("   Symlink: %s → %s\n", symlinkPath, realTarget)

		// Also re-create ~/.local/bin/diane if it exists
		localLink := filepath.Join(home, ".local", "bin", "diane")
		if _, err := os.Lstat(localLink); err == nil {
			os.Remove(localLink)
			os.MkdirAll(filepath.Dir(localLink), 0755)
			os.Symlink(realTarget, localLink)
			fmt.Printf("   Symlink: %s → %s\n", localLink, realTarget)
		}
	}
}

// resolveBinaryTarget follows symlinks to find the real file path.
func resolveBinaryTarget(symlinkPath string) string {
	var realTarget string

	if fi, err := os.Lstat(symlinkPath); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		resolved, err := os.Readlink(symlinkPath)
		if err == nil && filepath.IsAbs(resolved) {
			realTarget = resolved
		}
	}
	if realTarget == "" {
		realTarget = symlinkPath
	}
	return realTarget
}

// resignMacOSBinary removes quarantine attributes and re-signs.
func resignMacOSBinary(path string) {
	exec.Command("xattr", "-d", "com.apple.provenance", path).Run()
	exec.Command("codesign", "--force", "-s", "-", path).Run()
}

// restartServeProcess restarts the diane serve process.
// Strategy 1: launchctl kickstart (macOS)
// Strategy 2: systemctl restart (Linux)
// Strategy 3: PID-based kill + re-exec (fallback)
func restartServeProcess() {
	home, _ := os.UserHomeDir()

	if runtime.GOOS == "darwin" {
		// Strategy 1: macOS launchctl kickstart
		label := "com.emergent-company.diane-serve"
		cmd := exec.Command("launchctl", "kickstart", "-kp",
			fmt.Sprintf("gui/%d/%s", os.Getuid(), label))
		if out, err := cmd.CombinedOutput(); err == nil {
			fmt.Println("   Restarted via launchctl kickstart")
			return
		} else {
			log.Printf("[UPGRADE] launchctl kickstart failed: %v\n%s", err, string(out))
		}

		// Strategy 2: Try bootout + bootstrap
		plistPath := filepath.Join(home, "Library", "LaunchAgents", label+".plist")
		if _, err := os.Stat(plistPath); err == nil {
			exec.Command("launchctl", "bootout",
				fmt.Sprintf("gui/%d/%s", os.Getuid(), label)).Run()
			time.Sleep(1 * time.Second)
			exec.Command("launchctl", "bootstrap",
				fmt.Sprintf("gui/%d", os.Getuid()), plistPath).Run()
			time.Sleep(2 * time.Second)
			fmt.Println("   Restarted via launchctl bootstrap")
			return
		}
	}

	if runtime.GOOS == "linux" {
		// Strategy: systemctl restart
		if _, err := exec.LookPath("systemctl"); err == nil {
			cmd := exec.Command("systemctl", "restart", "diane.service")
			if out, err := cmd.CombinedOutput(); err == nil {
				fmt.Println("   Restarted via systemctl restart diane.service")
				return
			} else {
				log.Printf("[UPGRADE] systemctl restart failed: %v\n%s", err, string(out))
			}
		}
	}

	// Fallback: PID-based restart
	pidfile := filepath.Join(home, ".diane", "serve.pid")
	if data, err := os.ReadFile(pidfile); err == nil {
		pid := strings.TrimSpace(string(data))
		exec.Command("kill", "-TERM", pid).Run()
		time.Sleep(2 * time.Second)

		// Find the binary and re-exec
		binaryPath := filepath.Join(home, ".diane", "bin", "diane")
		if _, err := os.Stat(binaryPath); err == nil {
			exec.Command(binaryPath, "serve").Start()
			fmt.Println("   Restarted via PID-based fallback")
			return
		}
	}

	fmt.Println("   ⚠️  Could not restart serve — please restart manually")
}

// writeDMGTrigger writes a trigger file that the companion app reads to perform a DMG update.
func writeDMGTrigger(tagName string) {
	home, _ := os.UserHomeDir()
	trigger := map[string]any{
		"version":      tagName,
		"available":    true,
		"triggered_at": time.Now().UTC().Format(time.RFC3339),
		"triggered_by": Version,
	}
	data, _ := json.Marshal(trigger)
	triggerPath := filepath.Join(home, ".diane", "diane.dmg-trigger")
	os.WriteFile(triggerPath, data, 0644)
	fmt.Printf("   📱 DMG trigger written to %s\n", triggerPath)
}

// checkForUpdate is used by the background auto-upgrade loop in serve.go.
// Returns the latest version tag if an update is available, empty string if not.
func checkForUpdate() string {
	currentVer := strings.TrimPrefix(Version, "v")
	if currentVer == "dev" {
		return "" // dev builds skip upgrade
	}

	repo := "emergent-company/diane"
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return ""
	}
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		return ""
	}
	defer resp.Body.Close()

	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return ""
	}

	latestVer := strings.TrimPrefix(release.TagName, "v")
	if latestVer == currentVer || isOlderVersion(currentVer, latestVer) == false {
		return ""
	}
	return release.TagName
}

// checkCompanionApp reads the companion app's version and triggers a DMG
// upgrade if the app is outdated.
func checkCompanionApp(tagName string, assets []releaseAsset) {
	if _, err := os.Stat(companionAppPath); os.IsNotExist(err) {
		return // No companion app installed
	}

	// Read companion app version from Info.plist
	currentAppVer := readAppVersion(companionAppPath)
	latestVer := strings.TrimPrefix(tagName, "v")

	if !isOlderVersion(currentAppVer, latestVer) {
		fmt.Printf("✅ Companion app at v%s (up to date)\n", currentAppVer)
		return
	}

	fmt.Printf("📱 Companion app version: v%s, latest: v%s\n", currentAppVer, latestVer)

	// Find DMG asset
	var dmgAsset releaseAsset
	for _, a := range assets {
		name := a.Name
		if strings.HasSuffix(name, ".dmg") && strings.Contains(name, "Diane") {
			dmgAsset = a
			break
		}
	}
	if dmgAsset.Name == "" {
		fmt.Printf("   ℹ️  No DMG asset in release — app bundle must be updated manually.\n")
		return
	}

	fmt.Printf("⬇️  Downloading %s...\n", dmgAsset.Name)

	// Download DMG to temp
	tmpDir, err := os.MkdirTemp("", "diane-app-upgrade")
	if err != nil {
		fmt.Fprintf(os.Stderr, "   ❌ %v\n", err)
		return
	}
	defer os.RemoveAll(tmpDir)

	dmgPath := filepath.Join(tmpDir, dmgAsset.Name)
	if err := downloadFile(dmgAsset.BrowserDownloadURL, dmgPath); err != nil {
		fmt.Fprintf(os.Stderr, "   ❌ DMG download failed: %v\n", err)
		return
	}

	// Write post-termination install script
	script := fmt.Sprintf(`#!/bin/bash
# Diane Companion App upgrade — post-termination installer
sleep 2
ATTACHED=$(hdiutil attach -nobrowse -mountpoint /tmp/diane-update-mount "%s" 2>&1 | tail -1 | awk '{print $NF}')
if [ -d "/tmp/diane-update-mount/Diane.app" ]; then
	/usr/bin/ditto "/tmp/diane-update-mount/Diane.app" "%s"
	echo "✅ Companion app updated"
else
	echo "❌ DMG mount failed or no .app found"
fi
hdiutil detach "$ATTACHED" -quiet -force 2>/dev/null || true
rm -rf "%s"
open -n -a "%s"
rm -f /tmp/diane-update-install.sh
`, dmgPath, companionAppPath, tmpDir, companionAppPath)

	scriptPath := "/tmp/diane-update-install.sh"
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		fmt.Fprintf(os.Stderr, "   ❌ Failed to write install script: %v\n", err)
		return
	}

	fmt.Println()
	fmt.Println("📱 Companion app needs updating too!")
	fmt.Println("   The app will close, update via DMG, and relaunch.")
	fmt.Println("   (diane serve restarts automatically with the new app)")
	fmt.Print("\n   Update companion app now? [Y/n]: ")

	var response string
	fmt.Scanln(&response)
	if response == "n" || response == "N" || response == "no" {
		fmt.Println("   Skipped. Run `diane upgrade` again to trigger app update.")
		fmt.Println("   Or use the companion app's menu bar update button.")
		return
	}

	// Kill the companion app (which also kills its diane serve child process)
	exec.Command("pkill", "-x", "Diane").Run()

	// Launch the install script as a detached process
	cmd := exec.Command("/bin/bash", scriptPath)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "   ❌ Failed to launch install script: %v\n", err)
		return
	}

	fmt.Println("✅ Companion app upgrade initiated! The app will relaunch shortly.")
}

// readAppVersion extracts CFBundleShortVersionString from a macOS app bundle.
func readAppVersion(appPath string) string {
	plistPath := filepath.Join(appPath, "Contents", "Info.plist")
	if _, err := os.Stat(plistPath); os.IsNotExist(err) {
		return "0.0.0"
	}
	out, err := exec.Command("/usr/libexec/PlistBuddy", "-c", "Print :CFBundleShortVersionString", plistPath).Output()
	if err != nil {
		return "0.0.0"
	}
	return strings.TrimSpace(string(out))
}

// isOlderVersion returns true if current < latest by semver comparison.
func isOlderVersion(current, latest string) bool {
	current = strings.TrimPrefix(current, "v")
	latest = strings.TrimPrefix(latest, "v")

	curParts := strings.Split(current, ".")
	latParts := strings.Split(latest, ".")

	for len(curParts) < len(latParts) {
		curParts = append(curParts, "0")
	}
	for len(curParts) > len(latParts) {
		latParts = append(latParts, "0")
	}

	for i := 0; i < len(curParts); i++ {
		var c, l int
		fmt.Sscanf(curParts[i], "%d", &c)
		fmt.Sscanf(latParts[i], "%d", &l)
		if c < l {
			return true
		}
		if c > l {
			return false
		}
	}
	return false
}

// downloadFile downloads a URL to a local file path.
func downloadFile(url, dest string) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, resp.Body)
	return err
}

// exitErr prints a formatted error to stderr and exits with code 1.
func exitErr(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "❌ "+format+"\n", args...)
	os.Exit(1)
}
