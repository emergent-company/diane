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
	"strings"
)

// companionAppPath is where Diane.app lives on macOS.
const companionAppPath = "/Applications/Diane.app"

// cmdUpgrade checks for a new version and upgrades everything on macOS.
func cmdUpgrade() {
	home, _ := os.UserHomeDir()
	dianeDir := filepath.Join(home, ".diane")
	binDir := filepath.Join(dianeDir, "bin")
	symlinkPath := filepath.Join(binDir, "diane")

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

	resp, err := http.DefaultClient.Do(req)
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

	// Check if the CLI binary needs updating
	needsBinaryUpdate := currentVer != latestVer && currentVer != "dev"

	if !needsBinaryUpdate {
		fmt.Println("✅ CLI binary already up to date!")
	} else {
		// --- Binary update ---
		goos := runtime.GOOS
		goarch := runtime.GOARCH
		if goarch == "aarch64" {
			goarch = "arm64"
		}
		assetName := fmt.Sprintf("diane-%s-%s.tar.gz", goos, goarch)

		var binaryAsset struct {
			Name               string
			BrowserDownloadURL string
		}
		for _, a := range release.Assets {
			if a.Name == assetName {
				binaryAsset = a
				break
			}
		}
		if binaryAsset.Name == "" {
			fmt.Fprintf(os.Stderr, "❌ No asset found for %s\n", assetName)
			fmt.Fprintf(os.Stderr, "   Available: ")
			for _, a := range release.Assets {
				fmt.Fprintf(os.Stderr, "%s ", a.Name)
			}
			fmt.Fprintln(os.Stderr)
			os.Exit(1)
		}

		installBinary(binaryAsset.BrowserDownloadURL, binaryAsset.Name, symlinkPath, binDir)
		fmt.Printf("✅ CLI binary upgraded to v%s\n", latestVer)
	}

	// --- On macOS, also check companion app ---
	if runtime.GOOS == "darwin" {
		checkCompanionApp(release.TagName, release.Assets)
	}
}

// installBinary downloads the tarball, extracts it, and writes the binary
// to the correct location — handling symlinks to the app bundle on macOS.
func installBinary(downloadURL, assetName, symlinkPath, binDir string) {
	fmt.Printf("⬇️  Downloading %s...\n", assetName)

	dlReq, err := http.NewRequest("GET", downloadURL, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ %v\n", err)
		os.Exit(1)
	}
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		dlReq.Header.Set("Authorization", "Bearer "+token)
	}

	dlResp, err := http.DefaultClient.Do(dlReq)
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

	srcBinary := filepath.Join(tmpDir, "diane")
	input, err := os.ReadFile(srcBinary)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Failed to read extracted binary: %v\n", err)
		os.Exit(1)
	}

	// Resolve the actual install target, handling app-bundle symlinks.
	// On macOS, ~/.diane/bin/diane may be a symlink to
	// /Applications/Diane.app/Contents/Resources/diane.
	// We must write through the symlink (NOT replace it with a regular file).
	var realTarget string
	var wasSymlink bool
	if fi, err := os.Lstat(symlinkPath); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		resolved, err := os.Readlink(symlinkPath)
		if err == nil {
			if filepath.IsAbs(resolved) {
				realTarget = resolved
				wasSymlink = true
			}
		}
	}
	if realTarget == "" {
		realTarget = symlinkPath
	}

	// Always write through the resolved path (follows symlinks).
	// NEVER use os.Rename — it replaces the symlink with a regular file.
	os.MkdirAll(filepath.Dir(realTarget), 0755)
	if err := os.WriteFile(realTarget, input, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "❌ Failed to install binary: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("   Binary: %s\n", realTarget)

	// Re-create symlinks if we were in bundle mode
	if wasSymlink && realTarget != symlinkPath {
		os.Remove(symlinkPath)
		os.MkdirAll(binDir, 0755)
		os.Symlink(realTarget, symlinkPath)
		fmt.Printf("   Symlink: %s → %s\n", symlinkPath, realTarget)

		// Also re-create ~/.local/bin/diane if it exists
		home, _ := os.UserHomeDir()
		localLink := filepath.Join(home, ".local", "bin", "diane")
		if _, err := os.Lstat(localLink); err == nil {
			os.Remove(localLink)
			os.MkdirAll(filepath.Dir(localLink), 0755)
			os.Symlink(realTarget, localLink)
			fmt.Printf("   Symlink: %s → %s\n", localLink, realTarget)
		}
	}
}

// checkCompanionApp reads the companion app's version and triggers a DMG
// upgrade if the app is outdated.
func checkCompanionApp(tagName string, assets []struct {
	Name               string
	BrowserDownloadURL string
}) {
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
	var dmgAsset struct {
		Name               string
		BrowserDownloadURL string
	}
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
