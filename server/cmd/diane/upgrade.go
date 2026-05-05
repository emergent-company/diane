package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Upgrade state file: ~/.diane/upgrade.json
type upgradeState struct {
	Current  string `json:"current"`           // current installed version (e.g. "v1.21.3")
	Previous string `json:"previous,omitempty"` // previous version available for rollback
	PrevPath string `json:"prev_path,omitempty"` // path to the backed-up previous binary
}

// upgradeStatePath returns the path to the upgrade state file.
func upgradeStatePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".diane", "upgrade.json")
}

// readUpgradeState reads the upgrade state file, returning defaults if missing.
func readUpgradeState() upgradeState {
	var st upgradeState
	data, err := os.ReadFile(upgradeStatePath())
	if err == nil {
		json.Unmarshal(data, &st)
	}
	return st
}

// writeUpgradeState writes the upgrade state file atomically.
func writeUpgradeState(st upgradeState) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	path := upgradeStatePath()
	os.MkdirAll(filepath.Dir(path), 0755)
	return os.WriteFile(path, data, 0644)
}

// upgradeBinaryPath returns the canonical binary path in ~/.diane/bin/diane.
func upgradeBinaryPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".diane", "bin", "diane")
}

// previousBinaryPath returns the rollback backup path.
func previousBinaryPath() string {
	return upgradeBinaryPath() + ".prev"
}

// stagedBinaryPath returns the path for a newly downloaded binary.
func stagedBinaryPath() string {
	return upgradeBinaryPath() + ".new"
}

// cmdUpgrade handles the `diane upgrade` command and its subcommands.
// Subcommands: --check, --auto, rollback, or no flags for the original interactive mode.
func cmdUpgrade() {
	// Parse subcommand or flags
	args := os.Args[2:] // everything after "diane upgrade"

	if len(args) == 0 {
		// No args = original interactive upgrade
		doInteractiveUpgrade()
		return
	}

	switch args[0] {
	case "--check":
		doCheckUpgrade()
	case "--auto":
		doAutoUpgrade()
	case "rollback":
		doRollback()
	default:
		// If it doesn't match, treat as original mode (backward compat)
		doInteractiveUpgrade()
	}
}

// ── Upgrade State Helpers ──

// backupCurrentBinary copies current binary to diane.prev before upgrading.
func backupCurrentBinary(version string) error {
	src := upgradeBinaryPath()
	dst := previousBinaryPath()

	// If both exist and are the same file, skip
	srcStat, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("current binary not found at %s: %w", src, err)
	}

	dstStat, dstErr := os.Stat(dst)
	if dstErr == nil && os.SameFile(srcStat, dstStat) {
		return nil // already backed up
	}

	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read current binary: %w", err)
	}

	if err := os.WriteFile(dst, data, 0755); err != nil {
		return fmt.Errorf("write backup: %w", err)
	}

	// Update state
	st := readUpgradeState()
	st.Previous = st.Current
	st.PrevPath = dst
	if version != "" {
		st.Previous = version
	}
	writeUpgradeState(st)

	return nil
}

// verifyBinary runs "diane version" on the given binary to ensure it works.
// Returns the version string on success.
func verifyBinary(binPath string) (string, error) {
	cmd := exec.Command(binPath, "version", "--json")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("binary verification failed: %v — %s", err, string(out))
	}

	// Parse JSON output for version
	var result struct {
		CLI string `json:"cli"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		// Fallback: try without --json
		return strings.TrimSpace(string(out)), nil
	}
	ver := result.CLI
	if ver == "dev" {
		return "dev", nil
	}
	if !strings.HasPrefix(ver, "v") {
		ver = "v" + ver
	}
	return ver, nil
}

// swapBinary moves staged/new binary to the active location with a rollback safety net.
func swapBinary(src, dst string) error {
	// Copy (not rename) to avoid cross-device link issues
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read staged binary: %w", err)
	}
	if err := os.WriteFile(dst, data, 0755); err != nil {
		return fmt.Errorf("write binary: %w", err)
	}
	// Re-sign on macOS (ad-hoc) to avoid Killed:9
	if runtime.GOOS == "darwin" {
		if err := resignMacOSBinary(dst); err != nil {
			return fmt.Errorf("re-sign failed: %w", err)
		}
	}
	return nil
}

// resignMacOSBinary applies ad-hoc code signing and removes quarantine xattr.
func resignMacOSBinary(path string) error {
	_ = exec.Command("xattr", "-d", "com.apple.provenance", path).Run()
	return exec.Command("codesign", "--force", "-s", "-", path).Run()
}

// ── --check: Non-interactive check ──

func doCheckUpgrade() {
	latest, current, available := checkVersion()
	if jsonOutput {
		emitJSON("ok", map[string]interface{}{
			"current":         current,
			"latest":          latest,
			"updateAvailable": available,
		})
		return
	}
	fmt.Printf("Current: %s\n", current)
	fmt.Printf("Latest:  %s\n", latest)
	if available {
		fmt.Println("Update available!")
	} else {
		fmt.Println("Up to date.")
	}
}

// checkVersion fetches the latest GitHub release and compares to current version.
// Returns (latestTag, currentTag, updateAvailable).
func checkVersion() (string, string, bool) {
	currentVer := strings.TrimPrefix(Version, "v")
	currentTag := "v" + currentVer

	repo := "emergent-company/diane"
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		if jsonOutput {
			emitJSON("error", map[string]string{"message": fmt.Sprintf("request: %v", err)})
		}
		return "", currentTag, false
	}
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		if jsonOutput {
			emitJSON("error", map[string]string{"message": fmt.Sprintf("fetch: %v", err)})
		}
		return "", currentTag, false
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", currentTag, false
	}

	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", currentTag, false
	}

	latestTag := release.TagName
	available := false
	if currentVer != "dev" {
		available = isNewerVersion(latestTag, currentTag)
	}
	return latestTag, currentTag, available
}

// isNewerVersion returns true if v1 > v2 (strict semver comparison).
func isNewerVersion(v1, v2 string) bool {
	p1 := versionParts(v1)
	p2 := versionParts(v2)
	for i := 0; i < max(len(p1), len(p2)); i++ {
		a, b := 0, 0
		if i < len(p1) {
			a = p1[i]
		}
		if i < len(p2) {
			b = p2[i]
		}
		if a > b {
			return true
		}
		if a < b {
			return false
		}
	}
	return false
}

func versionParts(v string) []int {
	v = strings.TrimPrefix(v, "v")
	// Take only the numeric part before any suffix (e.g. "1.21.3-dev" → "1.21.3")
	if idx := strings.IndexAny(v, "-+"); idx >= 0 {
		v = v[:idx]
	}
	parts := strings.Split(v, ".")
	res := make([]int, len(parts))
	for i, p := range parts {
		n, _ := strconv.Atoi(p)
		res[i] = n
	}
	return res
}

// ── --auto: Non-interactive one-shot auto-upgrade ──

func doAutoUpgrade() {
	latestVer, currentVer, available := checkVersion()
	if !available {
		if jsonOutput {
			emitJSON("ok", map[string]interface{}{
				"message": "Already up to date",
				"current": currentVer,
				"latest":  latestVer,
			})
		} else {
			fmt.Printf("✅ Already up to date (%s)\n", currentVer)
		}
		return
	}

	latest := strings.TrimPrefix(latestVer, "v")

	fmt.Printf("⬇️  Downloading v%s...\n", latest)

	// Download new binary
	err := downloadAndStage(latestVer)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Download failed: %v\n", err)
		if jsonOutput {
			emitJSON("error", map[string]string{"message": fmt.Sprintf("download: %v", err)})
		}
		os.Exit(1)
	}

	// Backup current binary
	fmt.Print("📦 Backing up current binary... ")
	if err := backupCurrentBinary(currentVer); err != nil {
		fmt.Fprintf(os.Stderr, "⚠️  Backup failed: %v (continuing)\n", err)
	} else {
		fmt.Println("✅")
	}

	// Resolve symlinks (companion app bundle)
	targetPath := upgradeBinaryPath()
	var symlinkTarget string
	if fi, err := os.Lstat(targetPath); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		if resolved, err := os.Readlink(targetPath); err == nil && filepath.IsAbs(resolved) {
			symlinkTarget = resolved
			targetPath = resolved
			fmt.Printf("   Following symlink → %s\n", resolved)
		}
	}

	// Swap staged → active
	fmt.Print("🔄 Installing... ")
	staged := stagedBinaryPath()
	if err := swapBinary(staged, targetPath); err != nil {
		fmt.Fprintf(os.Stderr, "\n❌ Install failed: %v\n", err)
		tryRollback()
		os.Exit(1)
	}

	// Re-create symlink
	if symlinkTarget != "" {
		os.Remove(upgradeBinaryPath())
		os.Symlink(targetPath, upgradeBinaryPath())
	}

	// Verify the new binary works
	fmt.Print("verifying... ")
	if _, err := verifyBinary(targetPath); err != nil {
		fmt.Fprintf(os.Stderr, "\n❌ Verification failed: %v\n", err)
		tryRollback()
		os.Exit(1)
	}
	fmt.Println("✅")

	// Update state
	st := readUpgradeState()
	st.Current = latestVer
	st.Previous = currentVer
	st.PrevPath = previousBinaryPath()
	writeUpgradeState(st)

	if jsonOutput {
		emitJSON("ok", map[string]interface{}{
			"message":  fmt.Sprintf("Upgraded to %s", latestVer),
			"current":  currentVer,
			"latest":   latestVer,
			"previous": currentVer,
		})
		return
	}

	fmt.Printf("✅ Upgraded to v%s\n", latest)

	// Restart serve
	fmt.Print("🔄 Restarting diane serve... ")
	if err := restartServeProcess(); err != nil {
		fmt.Fprintf(os.Stderr, "⚠️  %v\n", err)
		fmt.Println("   You may need to restart diane serve manually:")
		fmt.Println("     launchctl kickstart -kp gui/$(id -u)/com.emergent-company.diane-serve")
		fmt.Println("     or: systemctl restart diane")
	} else {
		fmt.Println("✅")
	}
}

// companionAppPath returns the path to the companion app bundle, if installed.
func companionAppPath() string {
	// Check standard locations for Diane.app
	for _, p := range []string{
		"/Applications/Diane.app",
		filepath.Join(os.Getenv("HOME"), "Applications", "Diane.app"),
	} {
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			return p
		}
	}
	return ""
}

// isCompanionInstalled returns true if the companion app bundle exists on this machine.
func isCompanionInstalled() bool {
	return companionAppPath() != ""
}

// dmgTriggerPath returns the path to the auto-upgrade DMG trigger file.
func dmgTriggerPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".diane", "diane.dmg-trigger")
}

// writeDMGTrigger writes a trigger file that the companion app's UpdateChecker watches.
// When the companion sees this file, it performs the DMG-based full app update.
func writeDMGTrigger(version string) error {
	data := map[string]interface{}{
		"version":       version,
		"available":     true,
		"triggered_at":  time.Now().UTC().Format(time.RFC3339),
		"triggered_by":  Version,
	}
	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	os.MkdirAll(filepath.Dir(dmgTriggerPath()), 0755)
	return os.WriteFile(dmgTriggerPath(), raw, 0644)
}

// clearDMGTrigger removes the DMG trigger file.
func clearDMGTrigger() {
	os.Remove(dmgTriggerPath())
}

// ── Auto-Upgrade (Background / --auto) ──

// downloadAndStage downloads the release tarball and extracts the binary to the staged path.
// On macOS with companion app, also writes a DMG trigger for the companion to update.
func downloadAndStage(version string) error {
	repo := "emergent-company/diane"
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetch release info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var release struct {
		Assets []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return fmt.Errorf("parse release: %w", err)
	}

	// Find platform asset
	goos := runtime.GOOS
	goarch := runtime.GOARCH
	if goarch == "aarch64" {
		goarch = "arm64"
	}
	assetName := fmt.Sprintf("diane-%s-%s.tar.gz", goos, goarch)

	var downloadURL string
	for _, a := range release.Assets {
		if a.Name == assetName {
			downloadURL = a.BrowserDownloadURL
			break
		}
	}
	if downloadURL == "" {
		return fmt.Errorf("no asset found for %s", assetName)
	}

	// Download tarball to temp
	tmpDir, err := os.MkdirTemp("", "diane-upgrade")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	dlReq, _ := http.NewRequest("GET", downloadURL, nil)
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		dlReq.Header.Set("Authorization", "Bearer "+token)
	}

	dlResp, err := httpClient.Do(dlReq)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer dlResp.Body.Close()

	if dlResp.StatusCode != 200 {
		return fmt.Errorf("download returned %d", dlResp.StatusCode)
	}

	tmpTarball := filepath.Join(tmpDir, assetName)
	f, err := os.Create(tmpTarball)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	if _, err := io.Copy(f, dlResp.Body); err != nil {
		f.Close()
		return fmt.Errorf("save download: %w", err)
	}
	f.Close()

	// Extract
	extractCmd := exec.Command("tar", "xzf", tmpTarball, "-C", tmpDir)
	if out, err := extractCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("extract: %v — %s", err, string(out))
	}

	// Copy binary to staged path
	srcBinary := filepath.Join(tmpDir, "diane")
	dst := stagedBinaryPath()

	data, err := os.ReadFile(srcBinary)
	if err != nil {
		return fmt.Errorf("read extracted binary: %w", err)
	}

	os.MkdirAll(filepath.Dir(dst), 0755)
	if err := os.WriteFile(dst, data, 0755); err != nil {
		return fmt.Errorf("write staged binary: %w", err)
	}

	return nil
}

// tryRollback attempts to restore the previous binary on upgrade failure.
func tryRollback() {
	fmt.Print("⏪ Attempting rollback... ")
	dst := upgradeBinaryPath()
	src := previousBinaryPath()

	if _, err := os.Stat(src); err != nil {
		fmt.Fprintf(os.Stderr, "no backup available at %s\n", src)
		return
	}

	if err := swapBinary(src, dst); err != nil {
		fmt.Fprintf(os.Stderr, "rollback failed: %v\n", err)
		return
	}

	fmt.Println("✅ Rolled back to previous version")
	fmt.Println("   Run 'diane upgrade rollback' to restore again if needed.")
}

// ── Rollback ──

func doRollback() {
	src := previousBinaryPath()
	dst := upgradeBinaryPath()

	if _, err := os.Stat(src); err != nil {
		fmt.Fprintf(os.Stderr, "❌ No backup binary found at %s\n", src)
		fmt.Println("   Previous versions are kept at ~/.diane/bin/diane.prev")
		os.Exit(1)
	}

	// Backup current first (in case they want to roll forward again)
	currentData, err := os.ReadFile(dst)
	if err != nil {
		// Current binary might not exist yet
		fmt.Print("   (no current binary to back up)\n")
	} else {
		os.WriteFile(src+".tmp", currentData, 0755)
	}

	st := readUpgradeState()
	prevVersion := st.Previous
	if prevVersion == "" {
		prevVersion = "(unknown)"
	}

	fmt.Printf("⏪ Rolling back to %s...\n", prevVersion)

	// Resolve symlinks
	targetPath := dst
	var symlinkTarget string
	if fi, err := os.Lstat(dst); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		if resolved, err := os.Readlink(dst); err == nil && filepath.IsAbs(resolved) {
			symlinkTarget = resolved
			targetPath = resolved
		}
	}

	if err := swapBinary(src, targetPath); err != nil {
		fmt.Fprintf(os.Stderr, "❌ Rollback failed: %v\n", err)
		os.Exit(1)
	}

	if symlinkTarget != "" {
		os.Remove(dst)
		os.Symlink(targetPath, dst)
	}

	// Clean up tmp backup
	os.Remove(src + ".tmp")

	// Update state
	st.Current = prevVersion
	st.Previous = ""
	st.PrevPath = ""
	writeUpgradeState(st)

	fmt.Printf("✅ Rolled back to %s\n", prevVersion)
	fmt.Print("🔄 Restarting diane serve... ")
	if err := restartServeProcess(); err != nil {
		fmt.Fprintf(os.Stderr, "⚠️  %v\n", err)
	} else {
		fmt.Println("✅")
	}
}

// ── Original Interactive Upgrade ──

func doInteractiveUpgrade() {
	home, _ := os.UserHomeDir()
	dianeDir := filepath.Join(home, ".diane")
	binDir := filepath.Join(dianeDir, "bin")
	targetPath := filepath.Join(binDir, "diane")

	currentVer := strings.TrimPrefix(Version, "v")
	fmt.Printf("📦 Current version: v%s\n", currentVer)

	// Fetch latest release from GitHub
	repo := "emergent-company/diane"
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Failed to create request: %v\n", err)
		os.Exit(1)
	}
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Failed to fetch latest release: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		fmt.Fprintf(os.Stderr, "❌ No releases found — the repo may be private.\n")
		fmt.Fprintf(os.Stderr, "   Set GITHUB_TOKEN env var for private repos.\n")
		os.Exit(1)
	}
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(os.Stderr, "❌ GitHub API returned %d: %s\n", resp.StatusCode, string(body))
		os.Exit(1)
	}

	var release struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		fmt.Fprintf(os.Stderr, "❌ Failed to parse release: %v\n", err)
		os.Exit(1)
	}

	latestVer := strings.TrimPrefix(release.TagName, "v")
	fmt.Printf("📦 Latest version:  v%s\n", latestVer)

	if currentVer == latestVer && currentVer != "dev" {
		fmt.Println("✅ Already up to date!")
		return
	}

	// Determine platform asset name
	goos := runtime.GOOS
	goarch := runtime.GOARCH
	if goarch == "aarch64" {
		goarch = "arm64"
	}
	assetName := fmt.Sprintf("diane-%s-%s.tar.gz", goos, goarch)

	var downloadURL string
	for _, a := range release.Assets {
		if a.Name == assetName {
			downloadURL = a.BrowserDownloadURL
			break
		}
	}
	if downloadURL == "" {
		fmt.Fprintf(os.Stderr, "❌ No asset found for %s\n", assetName)
		fmt.Fprintf(os.Stderr, "   Available: ")
		for _, a := range release.Assets {
			fmt.Fprintf(os.Stderr, "%s ", a.Name)
		}
		fmt.Fprintln(os.Stderr)
		os.Exit(1)
	}

	fmt.Printf("⬇️  Downloading %s...\n", assetName)

	dlReq, err := http.NewRequest("GET", downloadURL, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ %v\n", err)
		os.Exit(1)
	}
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		dlReq.Header.Set("Authorization", "Bearer "+token)
	}

	dlResp, err := httpClient.Do(dlReq)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Download failed: %v\n", err)
		os.Exit(1)
	}
	defer dlResp.Body.Close()

	if dlResp.StatusCode != 200 {
		fmt.Fprintf(os.Stderr, "❌ Download returned %d\n", dlResp.StatusCode)
		os.Exit(1)
	}

	tmpDir, err := os.MkdirTemp("", "diane-upgrade")
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmpDir)

	tmpTarball := filepath.Join(tmpDir, assetName)
	f, err := os.Create(tmpTarball)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ %v\n", err)
		os.Exit(1)
	}
	if _, err := io.Copy(f, dlResp.Body); err != nil {
		f.Close()
		fmt.Fprintf(os.Stderr, "❌ %v\n", err)
		os.Exit(1)
	}
	f.Close()

	extractCmd := exec.Command("tar", "xzf", tmpTarball, "-C", tmpDir)
	if out, err := extractCmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "❌ Extraction failed: %v\n%s", err, string(out))
		os.Exit(1)
	}

	installTarget := targetPath
	var symlinkTarget string
	if fi, err := os.Lstat(targetPath); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		if resolved, err := os.Readlink(targetPath); err == nil {
			if filepath.IsAbs(resolved) {
				symlinkTarget = resolved
				installTarget = resolved
				fmt.Printf("   Following symlink → %s\n", resolved)
			}
		}
	}

	// Backup current before replacing (original upgrade didn't do this)
	fmt.Print("📦 Backing up current binary... ")
	backupCurrentBinary(currentVer)
	fmt.Println("✅")

	srcBinary := filepath.Join(tmpDir, "diane")
	if err := swapBinary(srcBinary, installTarget); err != nil {
		fmt.Fprintf(os.Stderr, "❌ Install failed: %v\n", err)
		tryRollback()
		os.Exit(1)
	}

	if symlinkTarget != "" {
		os.Remove(targetPath)
		if err := os.Symlink(installTarget, targetPath); err != nil {
			fmt.Fprintf(os.Stderr, "⚠️  Failed to re-create symlink: %v\n", err)
		}
	}

	// Update state
	st := readUpgradeState()
	st.Current = "v" + latestVer
	st.Previous = "v" + currentVer
	st.PrevPath = previousBinaryPath()
	writeUpgradeState(st)

	fmt.Printf("✅ Upgraded to v%s\n", latestVer)
	fmt.Printf("   Binary: %s\n", installTarget)
	if symlinkTarget != "" {
		fmt.Printf("   Symlink: %s → %s\n", targetPath, installTarget)
	}

	fmt.Print("\n🔄 Restarting diane serve... ")
	if err := restartServeProcess(); err != nil {
		fmt.Fprintf(os.Stderr, "⚠️  %v\n", err)
		fmt.Println("   You may need to restart diane serve manually:")
		fmt.Println("     launchctl kickstart -kp gui/$(id -u)/com.emergent-company.diane-serve")
	} else {
		fmt.Println("✅")
	}
}

// restartServeProcess restarts diane serve after upgrade.
// Tries launchd kickstart first; falls back to PID-based restart.
func restartServeProcess() error {
	home, _ := os.UserHomeDir()

	// Strategy 1: launchd — kickstart with kill-first + wait
	label := "com.emergent-company.diane-serve"
	uid := os.Getuid()
	kickstart := exec.Command("launchctl", "kickstart", "-kp", fmt.Sprintf("gui/%d/%s", uid, label))
	if _, err := kickstart.CombinedOutput(); err == nil {
		// Success — wait for serve to be reachable
		time.Sleep(2 * time.Second)
		return nil
	} else {
		// launchctl might have exited non-zero even if kickstart worked.
		// Check if the service is loaded by trying bootout first (which would fail if not loaded)
		check := exec.Command("launchctl", "print", fmt.Sprintf("gui/%d/%s", uid, label))
		if check.Run() != nil {
			// launchd not managing this service — try PID-based restart
			return restartServeByPID(home)
		}
		// launchd IS managing it, just not responding to kickstart — try bootout + bootstrap
		_ = exec.Command("launchctl", "bootout", fmt.Sprintf("gui/%d/%s", uid, label)).Run()
		time.Sleep(1 * time.Second)
		plistPath := filepath.Join(home, "Library", "LaunchAgents", label+".plist")
		if out2, err2 := exec.Command("launchctl", "bootstrap", fmt.Sprintf("gui/%d", uid), plistPath).CombinedOutput(); err2 != nil {
			return fmt.Errorf("launchctl re-bootstrap failed: %v — %s", err2, string(out2))
		}
		time.Sleep(2 * time.Second)
	}

	return nil
}

// restartServeByPID reads the serve PID file and restarts the process.
func restartServeByPID(home string) error {
	pidFile := filepath.Join(home, ".diane", "serve.pid")
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return fmt.Errorf("no PID file at %s (serve was not running or not managed by launchd)", pidFile)
	}

	pidStr := strings.TrimSpace(string(data))
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return fmt.Errorf("invalid PID in %s: %s", pidFile, pidStr)
	}

	// Send SIGTERM for graceful shutdown
	if err := exec.Command("kill", "-TERM", pidStr).Run(); err != nil {
		return fmt.Errorf("failed to signal PID %d: %v", pid, err)
	}

	// Wait for process to exit
	time.Sleep(2 * time.Second)

	// Start fresh serve process
	binary, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot determine own binary path: %v", err)
	}

	serve := exec.Command(binary, "serve")
	if err := serve.Start(); err != nil {
		return fmt.Errorf("failed to start serve: %v", err)
	}

	time.Sleep(2 * time.Second)
	return nil
}
