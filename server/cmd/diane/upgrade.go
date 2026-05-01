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

// cmdUpgrade checks for a new version and replaces the current binary.
func cmdUpgrade() {
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

	// Use GITHUB_TOKEN if available (private repo)
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

	// Find matching asset
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

	// Download tarball
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

	// Extract to temp dir
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

	// Extract
	extractCmd := exec.Command("tar", "xzf", tmpTarball, "-C", tmpDir)
	if out, err := extractCmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "❌ Extraction failed: %v\n%s", err, string(out))
		os.Exit(1)
	}

	// Resolve symlinks: if the target is a symlink to the companion app bundle,
	// write to the real file inside the bundle and re-symlink.
	installTarget := targetPath
	var symlinkTarget string // non-empty if we detected a symlink to re-create
	if fi, err := os.Lstat(targetPath); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		if resolved, err := os.Readlink(targetPath); err == nil {
			if filepath.IsAbs(resolved) {
				symlinkTarget = resolved
				installTarget = resolved
				fmt.Printf("   Following symlink → %s\n", resolved)
			}
		}
	}

	srcBinary := filepath.Join(tmpDir, "diane")
	newBinaryData, err := os.ReadFile(srcBinary)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Failed to read extracted binary: %v\n", err)
		os.Exit(1)
	}

	// Write to the (possibly resolved) install target
	if err := os.WriteFile(installTarget, newBinaryData, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "❌ Failed to install binary: %v\n", err)
		os.Exit(1)
	}

	// Re-create symlink if we followed one
	if symlinkTarget != "" {
		os.Remove(targetPath)
		if err := os.Symlink(installTarget, targetPath); err != nil {
			fmt.Fprintf(os.Stderr, "⚠️  Failed to re-create symlink: %v\n", err)
		}
	}

	fmt.Printf("✅ Upgraded to v%s\n", latestVer)
	fmt.Printf("   Binary: %s\n", installTarget)
	if symlinkTarget != "" {
		fmt.Printf("   Symlink: %s → %s\n", targetPath, installTarget)
	}

	// ── Restart diane serve so it picks up the new binary ──
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
	term := exec.Command("kill", "-TERM", pidStr)
	if err := term.Run(); err != nil {
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
