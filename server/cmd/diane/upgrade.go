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

// cmdUpgrade checks for a new version and replaces the current binary.
func cmdUpgrade() {
	home, _ := os.UserHomeDir()
	dianeDir := filepath.Join(home, ".diane")
	binDir := filepath.Join(dianeDir, "bin")
	binary := filepath.Join(binDir, "diane")

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

	// Move binary to install dir
	os.MkdirAll(binDir, 0755)
	srcBinary := filepath.Join(tmpDir, "diane")
	if err := os.Rename(srcBinary, binary); err != nil {
		// Fall back to copy+delete if cross-device
		input, _ := os.ReadFile(srcBinary)
		if err := os.WriteFile(binary, input, 0755); err != nil {
			fmt.Fprintf(os.Stderr, "❌ Failed to install binary: %v\n", err)
			os.Exit(1)
		}
	}

	fmt.Printf("✅ Upgraded to v%s\n", latestVer)
	fmt.Printf("   Binary: %s\n", binary)
}
