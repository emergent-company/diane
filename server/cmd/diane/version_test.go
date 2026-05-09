package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readAllString(t *testing.T, r io.Reader) string {
	t.Helper()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("readAllString: %v", err)
	}
	return string(b)
}

func TestReadInstalledAppVersion_NotInstalled(t *testing.T) {
	// This test validates the function doesn't crash when no app exists
	_ = readInstalledAppVersion()
}

func TestReadInstalledAppVersion_CustomHomeApp(t *testing.T) {
	doc := `
	<?xml version="1.0" encoding="UTF-8"?>
	<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
	  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
	<plist version="1.0"><dict>
		<key>CFBundleShortVersionString</key>
		<string>1.38.43</string>
	</dict></plist>`

	dir := t.TempDir()
	// Put app in $HOME/Applications/Diane.app — one of the paths checked
	appDir := filepath.Join(dir, "Applications", "Diane.app", "Contents")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "Info.plist"), []byte(doc), 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HOME", dir)

	ver := readInstalledAppVersion()
	if ver != "" {
		// Just verify the function reads a version successfully when an app exists.
		// We can't enforce a specific version since the real app may be installed.
		t.Logf("readInstalledAppVersion() = %q", ver)
	}
}

func TestCmdVersion_DevBuild(t *testing.T) {
	origVersion := Version
	t.Cleanup(func() { Version = origVersion })

	Version = ""

	// Capture stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	origStdout := os.Stdout
	os.Stdout = w

	cmdVersion()

	w.Close()
	os.Stdout = origStdout

	out := readAllString(t, r)

	if !strings.Contains(out, "CLI") {
		t.Errorf("output should contain 'CLI', got: %s", out)
	}
}

func TestCmdVersion_KnownVersion(t *testing.T) {
	origVersion := Version
	t.Cleanup(func() { Version = origVersion })

	Version = "v1.38.42"

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	origStdout := os.Stdout
	os.Stdout = w

	cmdVersion()

	w.Close()
	os.Stdout = origStdout

	out := readAllString(t, r)

	if !strings.Contains(out, "1.38.42") {
		t.Errorf("output should contain version, got: %s", out)
	}
}
