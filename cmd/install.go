package cmd

import (
	"fmt"
	"html"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/shadowfax92/focus/ipc"
)

const (
	plistLabel = "com.focus.daemon"
	bundleID   = "com.shadowfax92.focus"
)

func appPath() string       { return filepath.Join(userHome(), "Applications", "Focus.app") }
func appBinaryPath() string { return filepath.Join(appPath(), "Contents", "MacOS", "focus") }
func plistPath() string {
	return filepath.Join(userHome(), "Library", "LaunchAgents", plistLabel+".plist")
}
func logPath() string { return filepath.Join(userHome(), "Library", "Logs", "focus.log") }
func cliPath() string {
	if gobin := os.Getenv("GOBIN"); gobin != "" {
		return filepath.Join(gobin, "focus")
	}
	return filepath.Join(userHome(), "go", "bin", "focus")
}

func userHome() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return home
}

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install the Focus app and launchd service",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := install(); err != nil {
			return err
		}
		fmt.Println("Installed and started Focus.")
		fmt.Printf("  App:   %s\n", appPath())
		fmt.Printf("  CLI:   %s\n", cliPath())
		fmt.Printf("  Plist: %s\n", plistPath())
		fmt.Printf("  Log:   %s\n", logPath())
		return nil
	},
}

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Stop and remove the Focus app and launchd service",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := uninstall(); err != nil {
			return err
		}
		fmt.Println("Uninstalled Focus. Your config and event history were kept.")
		return nil
	},
}

func install() error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate focus executable: %w", err)
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return fmt.Errorf("resolve focus executable: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(appBinaryPath()), 0o755); err != nil {
		return fmt.Errorf("create app bundle: %w", err)
	}
	if err := copyExecutable(executable, appBinaryPath()); err != nil {
		return err
	}
	if err := writeInfoPlist(); err != nil {
		return err
	}
	if output, err := exec.Command("codesign", "--force", "--sign", "-", appPath()).CombinedOutput(); err != nil {
		return fmt.Errorf("codesign Focus.app: %w: %s", err, output)
	}
	if err := writeLaunchAgent(); err != nil {
		return err
	}
	if err := installCLISymlink(); err != nil {
		return err
	}
	_ = bootout()
	if err := bootstrap(); err != nil {
		return err
	}
	return nil
}

func uninstall() error {
	_ = bootout()
	paths := []string{plistPath(), ipc.SocketPath()}
	for _, path := range paths {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove %s: %w", path, err)
		}
	}
	if target, err := filepath.EvalSymlinks(cliPath()); err == nil {
		appTarget, appErr := filepath.EvalSymlinks(appBinaryPath())
		if appErr == nil && target == appTarget {
			if err := os.Remove(cliPath()); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("remove CLI symlink: %w", err)
			}
		}
	}
	if err := os.RemoveAll(appPath()); err != nil {
		return fmt.Errorf("remove app bundle: %w", err)
	}
	return nil
}

func copyExecutable(source, destination string) error {
	if sourceInfo, err := os.Stat(source); err == nil {
		if destinationInfo, err := os.Stat(destination); err == nil && os.SameFile(sourceInfo, destinationInfo) {
			return nil
		}
	}
	in, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open focus executable: %w", err)
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return fmt.Errorf("create app executable: %w", err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return fmt.Errorf("copy app executable: %w", err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close app executable: %w", err)
	}
	return nil
}

func writeInfoPlist() error {
	contents := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleIdentifier</key><string>%s</string>
  <key>CFBundleName</key><string>Focus</string>
  <key>CFBundleExecutable</key><string>focus</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>LSUIElement</key><true/>
</dict>
</plist>
`, bundleID)
	path := filepath.Join(appPath(), "Contents", "Info.plist")
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		return fmt.Errorf("write app Info.plist: %w", err)
	}
	return nil
}

func writeLaunchAgent() error {
	if err := os.MkdirAll(filepath.Dir(plistPath()), 0o755); err != nil {
		return fmt.Errorf("create LaunchAgents directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(logPath()), 0o755); err != nil {
		return fmt.Errorf("create Logs directory: %w", err)
	}
	contents := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>%s</string>
  <key>ProgramArguments</key>
  <array><string>%s</string><string>daemon</string></array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>ProcessType</key><string>Interactive</string>
  <key>StandardOutPath</key><string>%s</string>
  <key>StandardErrorPath</key><string>%s</string>
</dict>
</plist>
`, plistLabel, html.EscapeString(appBinaryPath()), html.EscapeString(logPath()), html.EscapeString(logPath()))
	if err := os.WriteFile(plistPath(), []byte(contents), 0o644); err != nil {
		return fmt.Errorf("write launchd plist: %w", err)
	}
	return nil
}

func installCLISymlink() error {
	if err := os.MkdirAll(filepath.Dir(cliPath()), 0o755); err != nil {
		return fmt.Errorf("create CLI directory: %w", err)
	}
	if err := os.Remove(cliPath()); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("replace CLI: %w", err)
	}
	if err := os.Symlink(appBinaryPath(), cliPath()); err != nil {
		return fmt.Errorf("link CLI: %w", err)
	}
	return nil
}

func bootstrap() error {
	domain := "gui/" + strconv.Itoa(os.Getuid())
	var lastErr error
	var lastOut []byte
	// bootout is asynchronous: a bootstrap issued while the old job is still
	// draining fails transiently, so retry briefly before falling back.
	for attempt := 0; attempt < 5; attempt++ {
		out, err := exec.Command("launchctl", "bootstrap", domain, plistPath()).CombinedOutput()
		if err == nil {
			return nil
		}
		lastErr, lastOut = err, out
		time.Sleep(300 * time.Millisecond)
	}
	// Deprecated `load` can exit 0 without loading anything; trust only a
	// service that launchctl can actually see afterwards.
	_ = exec.Command("launchctl", "load", plistPath()).Run()
	if exec.Command("launchctl", "print", domain+"/"+plistLabel).Run() == nil {
		return nil
	}
	return fmt.Errorf("launchctl bootstrap failed: %w: %s", lastErr, lastOut)
}

func bootout() error {
	domain := "gui/" + strconv.Itoa(os.Getuid()) + "/" + plistLabel
	if err := exec.Command("launchctl", "bootout", domain).Run(); err == nil {
		return nil
	}
	return exec.Command("launchctl", "unload", plistPath()).Run()
}

func init() { rootCmd.AddCommand(installCmd, uninstallCmd) }
