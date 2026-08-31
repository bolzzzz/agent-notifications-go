package product

import (
	"os"
	"strings"
)

// Desktop host classification: which Electron editor app launched (directly or
// via an ancestor) the current process. This is orthogonal to the product
// running the hook — the Cursor CLI may run as a guest inside VS Code's
// integrated terminal, and its hook subprocesses then carry both CURSOR_* (from
// the CLI) and VS Code host markers (from the terminal).
//
// Electron sets CHROME_DESKTOP to the app's own .desktop file and exports it to
// every child process, so integrated-terminal shells — and everything they
// spawn, including AI CLI hook subprocesses — carry a durable per-app marker.
// GIO_LAUNCHED_DESKTOP_FILE names the .desktop file of the app that launched
// the process via GTK/gio and is inherited the same way. VS Code additionally
// points VSCODE_GIT_ASKPASS_NODE/MAIN at files inside its install directory,
// which covers hosts where the desktop-file vars are missing.

// DesktopHost values returned by DesktopHost.
const (
	// DesktopHostCursor marks the Cursor desktop app (or its integrated terminal).
	DesktopHostCursor = "cursor"
	// DesktopHostCode marks the VS Code desktop app (or its integrated terminal).
	DesktopHostCode = "code"
)

// desktopFileHostVars name the launching app's .desktop file directly.
var desktopFileHostVars = []string{"GIO_LAUNCHED_DESKTOP_FILE", "CHROME_DESKTOP"}

// askPassHostVars point at files inside VS Code's / Cursor's install directory.
var askPassHostVars = []string{"VSCODE_GIT_ASKPASS_NODE", "VSCODE_GIT_ASKPASS_MAIN"}

// allHostVars is every variable scanned for a Cursor marker.
var allHostVars = append(append([]string{}, desktopFileHostVars...), askPassHostVars...)

// DesktopHost reports which desktop editor hosts the current process:
// DesktopHostCursor, DesktopHostCode, or "" when the host cannot be determined
// (not editor-hosted, or an editor we do not classify).
//
// Cursor wins over VS Code: a Cursor desktop file or install path never
// contains "code", so the first cursor match is unambiguous, and misreading a
// Cursor host as VS Code would focus the wrong editor.
func DesktopHost() string {
	for _, name := range allHostVars {
		if strings.Contains(strings.ToLower(os.Getenv(name)), "cursor") {
			return DesktopHostCursor
		}
	}
	for _, name := range desktopFileHostVars {
		if strings.Contains(strings.ToLower(os.Getenv(name)), "code") {
			return DesktopHostCode
		}
	}
	// Askpass paths use a substring match on the install directory ("/code/")
	// so "code-oss" installs do not false-positive, but a plain
	// "/usr/share/code" binary still matches via its trailing segment.
	for _, name := range askPassHostVars {
		value := strings.ToLower(os.Getenv(name))
		if strings.Contains(value, "/code/") || strings.HasSuffix(value, "/code") {
			return DesktopHostCode
		}
	}
	return ""
}

// IsCursorDesktopHost reports whether the process was launched from the Cursor
// desktop app, including its integrated terminal.
func IsCursorDesktopHost() bool {
	return DesktopHost() == DesktopHostCursor
}

// IsVSCodeDesktopHost reports whether the process was launched from the VS Code
// desktop app, including its integrated terminal. When true, a Cursor CLI
// session running in that terminal must focus the VS Code window — not the
// (possibly not even running) Cursor IDE.
func IsVSCodeDesktopHost() bool {
	return DesktopHost() == DesktopHostCode
}
