//go:build darwin

package notifier

import (
	"os"
	"path/filepath"
	"testing"
)

// setupAgentNotifierEnv sets CLAUDE_PLUGIN_ROOT to point to the repo root
// so that GetTerminalNotifierPath finds AgentNotifier.app if it's been built.
// Returns a cleanup function and whether AgentNotifier.app was found.
func setupAgentNotifierEnv(t *testing.T) (cleanup func(), found bool) {
	t.Helper()

	original := os.Getenv("CLAUDE_PLUGIN_ROOT")

	// Walk up from test location to find repo root
	// Tests run from internal/notifier/, repo root is 2 levels up
	wd, err := os.Getwd()
	if err != nil {
		return func() { os.Setenv("CLAUDE_PLUGIN_ROOT", original) }, false
	}

	repoRoot := filepath.Dir(filepath.Dir(wd))
	binApp := filepath.Join(repoRoot, "bin", "AgentNotifier.app")
	appBinary := filepath.Join(binApp, "Contents", "MacOS", "terminal-notifier-modern")

	// Also check swift-notifier build output (full .app bundle)
	swiftApp := filepath.Join(repoRoot, "swift-notifier", "AgentNotifier.app")
	swiftAppBinary := filepath.Join(swiftApp, "Contents", "MacOS", "terminal-notifier-modern")

	cleanup = func() {
		os.Setenv("CLAUDE_PLUGIN_ROOT", original)
	}

	// Check if AgentNotifier.app exists in bin/ with full bundle
	if _, err := os.Stat(appBinary); err == nil {
		// Verify it's a complete bundle (has Info.plist)
		if _, err := os.Stat(filepath.Join(binApp, "Contents", "Info.plist")); err == nil {
			os.Setenv("CLAUDE_PLUGIN_ROOT", repoRoot)
			return cleanup, true
		}
	}

	if _, err := os.Stat(swiftAppBinary); err == nil {
		// Symlink the entire .app bundle to bin/ (not just the binary)
		os.Setenv("CLAUDE_PLUGIN_ROOT", repoRoot)
		// Remove any incomplete bin/AgentNotifier.app first
		os.RemoveAll(binApp)
		if err := os.Symlink(swiftApp, binApp); err == nil {
			origCleanup := cleanup
			cleanup = func() {
				os.Remove(binApp) // Remove symlink
				origCleanup()
			}
			return cleanup, true
		}
	}

	return cleanup, false
}
