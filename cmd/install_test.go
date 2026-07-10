package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallUninstallRoundTrip(t *testing.T) {
	home := t.TempDir()
	bin := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"codesign", "launchctl"} {
		path := filepath.Join(bin, name)
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("GOBIN", filepath.Join(home, "bin"))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	if err := install(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{appBinaryPath(), plistPath(), cliPath()} {
		if _, err := os.Lstat(path); err != nil {
			t.Fatalf("installed path %s: %v", path, err)
		}
	}
	info, err := os.ReadFile(filepath.Join(appPath(), "Contents", "Info.plist"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(info), "<key>LSUIElement</key><true/>") {
		t.Fatal("Info.plist does not declare LSUIElement")
	}

	if err := uninstall(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{appPath(), plistPath(), cliPath()} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("path remains after uninstall: %s", path)
		}
	}
}
