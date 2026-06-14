//go:build linux

// ABOUTME: Linux-specific notification handling with click-to-focus support.
// ABOUTME: Uses background daemon for persistent D-Bus connection when click-to-focus is enabled.
package notifier

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/777genius/claude-notifications/internal/config"
	"github.com/777genius/claude-notifications/internal/daemon"
	"github.com/777genius/claude-notifications/internal/logging"
	"github.com/gen2brain/beeep"
)

// macOS stub functions - these are not used on Linux but required for compilation

// GetTerminalBundleID returns empty string on Linux
// as terminal bundle IDs are a macOS-specific concept.
func GetTerminalBundleID(configOverride string) string {
	return ""
}

// GetTerminalNotifierPath returns an error on Linux
// as terminal-notifier is macOS-only.
func GetTerminalNotifierPath() (string, error) {
	return "", fmt.Errorf("terminal-notifier is only available on macOS")
}

// IsTerminalNotifierAvailable returns false on Linux.
func IsTerminalNotifierAvailable() bool {
	return false
}

// EnsureClaudeNotificationsApp is a no-op on Linux.
func EnsureClaudeNotificationsApp() error {
	return nil
}

// sendLinuxNotification sends a notification on Linux.
// When clickToFocus is enabled, uses the daemon for click-to-focus support.
// Falls back to beeep when daemon is unavailable.
// cwd is the current working directory; initialCWD is the first cwd seen for this session.
// Both are used for window-specific focus. Either may be empty.
func sendLinuxNotification(title, body, appIcon string, cfg *config.Config, cwd, initialCWD string) error {
	// If click-to-focus is disabled, use beeep directly
	if !cfg.Notifications.Desktop.ClickToFocus {
		logging.Debug("Click-to-focus disabled, using beeep directly")
		return beeep.Notify(title, body, appIcon)
	}

	// Try to use daemon for click-to-focus
	if err := sendViaDaemon(title, body, cwd, initialCWD); err == nil {
		logging.Debug("Notification sent via daemon with click-to-focus support")
		return nil
	} else {
		logging.Debug("Daemon not available (%v), falling back to beeep", err)
	}

	// Fallback to beeep (no click-to-focus)
	return beeep.Notify(title, body, appIcon)
}

// sendViaDaemon sends a notification via the background daemon.
// Returns an error if daemon is not available or fails.
// cwd is used for the current-folder focus hint; initialCWD is used for the
// session/project-folder focus hint.
func sendViaDaemon(title, body, cwd, initialCWD string) error {
	// Start daemon on-demand (no-op if already running)
	if !daemon.StartDaemonOnDemand() {
		return daemon.ErrDaemonNotAvailable
	}

	// Create client and send notification
	client, err := daemon.NewClient()
	if err != nil {
		return err
	}

	// Send notification with 30 second timeout.
	// Detect focus target in the hook process (not the daemon), since the daemon may
	// have been started from a different environment.
	focusTarget := daemon.GetTerminalName()

	// Extract both the session/project folder and the current cwd folder for
	// title-based focus. Claude Code's hook cwd can change after Bash cd commands;
	// initialCWD preserves the folder where the session started.
	folderName := ""
	cwdFolderName := ""
	workspaceName := ""
	if initialCWD != "" {
		folderName = filepath.Base(initialCWD)
	}
	if cwd != "" {
		cwdFolderName = filepath.Base(cwd)
		if folderName == "" {
			folderName = cwdFolderName
		}
		if daemon.IsVSCodeTerminalName(focusTarget) {
			workspaceName = detectVSCodeWorkspaceName(cwd)
		}
	}
	focusWindowID := daemon.GetX11WindowID()
	focusWindowTitle := daemon.GetExactWindowTitle(focusTarget)
	if sessionType := os.Getenv("XDG_SESSION_TYPE"); sessionType != "" && sessionType != "x11" {
		focusWindowID = ""
	}

	// Capture WezTerm pane info only when the focus target is actually WezTerm.
	wezTermPaneID, wezTermSocket := daemon.GetWezTermFocusHints(focusTarget)

	_, err = client.SendNotification(title, body, focusTarget, folderName, cwdFolderName, workspaceName, focusWindowID, focusWindowTitle, wezTermPaneID, wezTermSocket, 2, 30)
	return err
}

// IsDaemonAvailable checks if the notification daemon is available and running.
// Exported for testing and status checks.
func IsDaemonAvailable() bool {
	return daemon.IsDaemonRunning()
}

// StartDaemon starts the notification daemon on-demand.
// Returns true if daemon started successfully or was already running.
func StartDaemon() bool {
	return daemon.StartDaemonOnDemand()
}

// StopDaemon stops the running notification daemon.
func StopDaemon() error {
	return daemon.StopDaemon()
}

// detectVSCodeWorkspaceName walks parent directories of cwd looking for a
// .code-workspace file. Returns the workspace name (filename without extension)
// if found, or empty string otherwise. Stops at the user's home directory.
func detectVSCodeWorkspaceName(cwd string) string {
	if cwd == "" {
		return ""
	}
	homeDir, _ := os.UserHomeDir()
	dir := cwd
	for i := 0; i < 8; i++ {
		if homeDir != "" && dir == homeDir {
			break
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			break
		}
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".code-workspace") {
				if name := strings.TrimSuffix(entry.Name(), ".code-workspace"); name != "" {
					return name
				}
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}
