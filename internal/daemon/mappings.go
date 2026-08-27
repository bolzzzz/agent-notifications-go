package daemon

import (
	"os"
	"os/exec"
	"strings"

	"github.com/777genius/agent-notifications-go/internal/product"
)

const agentNotificationsDesktopEntryID = "agent-notifications"

// escapeJS escapes a string for safe interpolation into JavaScript single-quoted strings.
// Prevents JS injection when values are passed to GNOME Shell.Eval.
func escapeJS(s string) string {
	r := strings.NewReplacer(
		`\`, `\\`,
		`'`, `\'`,
		`"`, `\"`,
		"\n", `\n`,
		"\r", `\r`,
		"\x00", `\x00`,
		"\u2028", `\u2028`,
		"\u2029", `\u2029`,
	)
	return r.Replace(s)
}

// GetAppID returns the .desktop app ID for a terminal name.
func GetAppID(terminalName string) string {
	switch strings.ToLower(terminalName) {
	case "code", "vscode", "visual studio code":
		return "code.desktop"
	case "cursor", "cursor ide":
		return "cursor.desktop"
	case "gnome-terminal":
		return "org.gnome.Terminal.desktop"
	case "konsole":
		return "org.kde.konsole.desktop"
	case "alacritty":
		return "Alacritty.desktop"
	case "kitty":
		return "kitty.desktop"
	case "wezterm":
		return "org.wezfurlong.wezterm.desktop"
	case "tilix":
		return "com.gexperts.Tilix.desktop"
	case "terminator":
		return "terminator.desktop"
	default:
		return strings.ToLower(terminalName) + ".desktop"
	}
}

// GetDesktopEntryID returns the desktop entry ID (without .desktop suffix) for a terminal.
// This is the value expected by the freedesktop "desktop-entry" notification hint.
func GetDesktopEntryID(terminalName string) string {
	return strings.TrimSuffix(GetAppID(terminalName), ".desktop")
}

// GetNotificationDesktopEntryID returns the desktop-entry hint value to use for
// notifications. Notifications are attributed to this plugin so per-app
// settings (history, DND, badges) are not mixed with the terminal/editor.
// Falls back to the terminal desktop entry if the plugin desktop file is
// missing. The dedicated desktop file also uses StartupNotify=false so GNOME
// on Wayland does not leave a loading cursor after click-to-focus.
func GetNotificationDesktopEntryID(terminalName string) string {
	if hasAgentNotificationsDesktopEntry() {
		return agentNotificationsDesktopEntryID
	}
	return GetDesktopEntryID(terminalName)
}

func hasAgentNotificationsDesktopEntry() bool {
	_, err := os.Stat(getAgentNotificationsDesktopEntryPath())
	return err == nil
}

func getAgentNotificationsDesktopEntryPath() string {
	dataHome := strings.TrimSpace(os.Getenv("XDG_DATA_HOME"))
	if dataHome == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil || strings.TrimSpace(homeDir) == "" {
			return ""
		}
		dataHome = homeDir + "/.local/share"
	}

	return dataHome + "/applications/" + agentNotificationsDesktopEntryID + ".desktop"
}

// GetGnomeWmClass returns the WM_CLASS used by the activate-window-by-title
// GNOME Shell extension for activateByWmClass calls.
// For Wayland-native apps this is the app_id in reverse-domain format.
func GetGnomeWmClass(terminalName string) string {
	switch strings.ToLower(terminalName) {
	case "ghostty":
		return "com.mitchellh.ghostty"
	case "wezterm":
		return "org.wezfurlong.wezterm"
	case "code", "vscode", "visual studio code":
		return "code"
	case "cursor", "cursor ide":
		return "cursor"
	case "alacritty":
		return "Alacritty"
	case "kitty":
		return "kitty"
	case "gnome-terminal":
		return "org.gnome.Terminal"
	case "konsole":
		return "org.kde.konsole"
	case "tilix":
		return "com.gexperts.Tilix"
	case "terminator":
		return "Terminator"
	case "xfce4-terminal":
		return "Xfce4-terminal"
	case "mate-terminal":
		return "Mate-terminal"
	default:
		return terminalName
	}
}

// gnomeWmClassCandidates returns WM_CLASS values to try with the
// activate-window-by-title extension, which compares with strict equality.
// Electron apps may report either the lowercase app_id or the StartupWMClass
// from the desktop file ("Cursor", "Code").
func gnomeWmClassCandidates(terminalName string) []string {
	primary := GetGnomeWmClass(terminalName)
	if primary == "" {
		return nil
	}
	switch strings.ToLower(terminalName) {
	case "cursor", "cursor ide":
		return uniqueKeepOrder(primary, "Cursor")
	case "code", "vscode", "visual studio code":
		return uniqueKeepOrder(primary, "Code")
	default:
		return []string{primary}
	}
}

func uniqueKeepOrder(values ...string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

// GetWlrctlAppID returns the wlroots app_id for a terminal name.
func GetWlrctlAppID(terminalName string) string {
	switch strings.ToLower(terminalName) {
	case "ghostty":
		return "com.mitchellh.ghostty"
	case "code", "vscode", "visual studio code":
		return "code"
	case "cursor", "cursor ide":
		return "cursor"
	case "alacritty":
		return "Alacritty"
	case "kitty":
		return "kitty"
	case "wezterm":
		return "org.wezfurlong.wezterm"
	case "gnome-terminal":
		return "org.gnome.Terminal"
	case "konsole":
		return "org.kde.konsole"
	default:
		return strings.ToLower(terminalName)
	}
}

// GetKdotoolClass returns the window class for kdotool search.
func GetKdotoolClass(terminalName string) string {
	switch strings.ToLower(terminalName) {
	case "code", "vscode", "visual studio code":
		return "code"
	case "cursor", "cursor ide":
		return "cursor"
	case "alacritty":
		return "Alacritty"
	case "kitty":
		return "kitty"
	case "wezterm":
		return "org.wezfurlong.wezterm"
	case "gnome-terminal":
		return "gnome-terminal-server"
	case "konsole":
		return "konsole"
	default:
		return strings.ToLower(terminalName)
	}
}

// GetXdotoolClass returns the X11 WM_CLASS for xdotool search.
func GetXdotoolClass(terminalName string) string {
	switch strings.ToLower(terminalName) {
	case "code", "vscode", "visual studio code":
		return "Code"
	case "cursor", "cursor ide":
		return "Cursor"
	case "alacritty":
		return "Alacritty"
	case "kitty":
		return "kitty"
	case "wezterm":
		return "org.wezfurlong.wezterm"
	case "gnome-terminal":
		return "Gnome-terminal"
	case "konsole":
		return "konsole"
	case "xfce4-terminal":
		return "Xfce4-terminal"
	case "mate-terminal":
		return "Mate-terminal"
	case "tilix":
		return "Tilix"
	case "terminator":
		return "Terminator"
	default:
		return terminalName
	}
}

// GetSearchTerm returns a window title search term for a terminal name.
func GetSearchTerm(terminalName string) string {
	switch strings.ToLower(terminalName) {
	case "code", "vscode", "visual studio code":
		return "Visual Studio Code"
	case "cursor", "cursor ide":
		return "Cursor"
	case "gnome-terminal":
		return "Terminal"
	default:
		return terminalName
	}
}

// GetSearchTermWithFolder returns the window title search term, using the project
// folder name for VS Code / Cursor when available. The folder name is combined
// with the app title suffix so the search matches the editor window precisely
// without accidentally matching browser tabs that contain the folder name.
func GetSearchTermWithFolder(terminalName, folderName string) string {
	switch strings.ToLower(terminalName) {
	case "code", "vscode", "visual studio code":
		if folderName != "" {
			// VS Code on Linux uses a regular hyphen as title separator (" - "),
			// not an em dash (" — ") which is macOS-only. The suffix avoids
			// matching browser tabs that contain the folder name.
			return folderName + " - Visual Studio Code"
		}
	case "cursor", "cursor ide":
		if folderName != "" {
			// Cursor is a VS Code fork and uses the same Linux title separator.
			return folderName + " - Cursor"
		}
	}
	return GetSearchTerm(terminalName)
}

// folderTitleSearchTerms returns substring candidates for raising a specific
// VS Code / Cursor window. Precise "folder - App" forms are first; a trailing
// "folder - " fallback covers custom window.title values that insert extra
// segments (profileName, dirty indicator) between the folder and the app name.
//
// When a folder name is available this must stay folder-specific: a bare app
// / WM-class match would raise an arbitrary window of the same editor.
func folderTitleSearchTerms(terminalName, folderName string) []string {
	folderName = strings.TrimSpace(folderName)
	if folderName == "" {
		return nil
	}

	precise := GetSearchTermWithFolder(terminalName, folderName)
	if precise == GetSearchTerm(terminalName) {
		return nil
	}

	terms := []string{precise}
	if workspaceTerm := GetSearchTermWorkspace(terminalName, folderName); workspaceTerm != "" {
		terms = append(terms, workspaceTerm)
	}
	if IsVSCodeTerminalName(terminalName) || IsCursorTerminalName(terminalName) {
		// "myproject - Default - Cursor" does not contain "myproject - Cursor".
		terms = append(terms, folderName+" - ")
	}

	seen := make(map[string]bool, len(terms))
	out := make([]string, 0, len(terms))
	for _, term := range terms {
		if term == "" || seen[term] {
			continue
		}
		seen[term] = true
		out = append(out, term)
	}
	return out
}

// GetSearchTermWorkspace returns the VS Code workspace-mode window title search term.
// When VS Code opens a .code-workspace file, the title becomes "{name} (Workspace) - Visual Studio Code"
// rather than "{folder} - Visual Studio Code". Returns empty string for non-VS Code terminals
// or when no folder name is provided.
func GetSearchTermWorkspace(terminalName, folderName string) string {
	switch strings.ToLower(terminalName) {
	case "code", "vscode", "visual studio code":
		if folderName != "" {
			return folderName + " (Workspace) - Visual Studio Code"
		}
	case "cursor", "cursor ide":
		if folderName != "" {
			return folderName + " (Workspace) - Cursor"
		}
	}
	return ""
}

// GetTerminalName detects the current terminal from environment variables.
func GetTerminalName() string {
	termProg := strings.TrimSpace(os.Getenv("TERM_PROGRAM"))

	// Cursor IDE (and its integrated terminal) injects CURSOR_* and often looks
	// like VS Code (TERM_PROGRAM=vscode / VSCODE_*). Focus the Cursor window
	// instead of searching for Visual Studio Code. Cursor CLI in a real
	// terminal keeps that terminal's TERM_PROGRAM (kitty, ghostty, …).
	//
	// CURSOR_* is only injected into Cursor's own agent-hook subprocesses, not
	// into arbitrary shells running inside Cursor's integrated terminal — so a
	// plain Claude Code session opened there has no CURSOR_* to check. Fall back
	// to isCursorDesktopHost, which reads GTK/Chromium launch-tracking env vars
	// that every child process of the Cursor app inherits.
	if (product.IsCursorEnv() || isCursorDesktopHost()) && cursorIDELike(termProg) {
		return "Cursor"
	}

	// Try TERM_PROGRAM first (set by many terminals)
	if termProg != "" {
		return termProg
	}

	// Check VS Code indicators
	if os.Getenv("VSCODE_INJECTION") != "" || os.Getenv("VSCODE_GIT_IPC_HANDLE") != "" {
		return "Code"
	}

	// Check GNOME Terminal indicators
	if os.Getenv("GNOME_TERMINAL_SCREEN") != "" || os.Getenv("GNOME_TERMINAL_SERVICE") != "" {
		return "gnome-terminal"
	}

	// Check Terminator (does not set TERM_PROGRAM, but always sets TERMINATOR_UUID)
	if os.Getenv("TERMINATOR_UUID") != "" {
		return "terminator"
	}

	// Check Konsole (does not set TERM_PROGRAM, but sets KONSOLE_* vars)
	if os.Getenv("KONSOLE_VERSION") != "" || os.Getenv("KONSOLE_DBUS_SESSION") != "" {
		return "konsole"
	}

	// Fallback to generic terminal
	return "Terminal"
}

// isCursorDesktopHost reports whether the current process was launched (directly
// or via an ancestor) from the Cursor desktop app, using GTK/Chromium
// launch-tracking env vars (GIO_LAUNCHED_DESKTOP_FILE, CHROME_DESKTOP) that name
// the .desktop file of the launching app and are inherited by every child
// process — including a plain shell opened in Cursor's integrated terminal.
func isCursorDesktopHost() bool {
	for _, name := range []string{"GIO_LAUNCHED_DESKTOP_FILE", "CHROME_DESKTOP"} {
		if strings.Contains(strings.ToLower(os.Getenv(name)), "cursor") {
			return true
		}
	}
	return false
}

func cursorIDELike(termProg string) bool {
	if termProg == "" || isVSCodeTermProgram(termProg) {
		return true
	}
	// Editor-hosted hook even if TERM_PROGRAM was inherited from a launcher terminal.
	return os.Getenv("VSCODE_PID") != "" ||
		os.Getenv("VSCODE_INJECTION") != "" ||
		os.Getenv("VSCODE_GIT_IPC_HANDLE") != ""
}

func isVSCodeTermProgram(termProg string) bool {
	switch strings.ToLower(termProg) {
	case "vscode", "code", "visual studio code":
		return true
	default:
		return false
	}
}

// GetX11WindowID returns the current terminal window's X11 window ID when available.
// It is captured in the hook process and later used by the daemon for exact focus on X11.
func GetX11WindowID() string {
	return strings.TrimSpace(os.Getenv("WINDOWID"))
}

// GetExactWindowTitle returns an exact top-level window title for terminals that expose
// a reliable per-terminal identifier. Currently Terminator can provide this via
// TERMINATOR_UUID + remotinator.
func GetExactWindowTitle(terminalName string) string {
	switch strings.ToLower(terminalName) {
	case "terminator":
		return getTerminatorWindowTitle()
	default:
		return ""
	}
}

func getTerminatorWindowTitle() string {
	if os.Getenv("TERMINATOR_UUID") == "" {
		return ""
	}
	if _, err := exec.LookPath("remotinator"); err != nil {
		return ""
	}

	cmd := exec.Command("remotinator", "get_window_title")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(output))
}

// GetWezTermPaneID returns the WezTerm pane ID from the environment.
func GetWezTermPaneID() string {
	return strings.TrimSpace(os.Getenv("WEZTERM_PANE"))
}

// GetWezTermSocketPath returns the WezTerm Unix socket path from the environment.
func GetWezTermSocketPath() string {
	return strings.TrimSpace(os.Getenv("WEZTERM_UNIX_SOCKET"))
}

// IsVSCodeTerminalName reports whether terminalName identifies VS Code.
func IsVSCodeTerminalName(terminalName string) bool {
	switch strings.ToLower(strings.TrimSpace(terminalName)) {
	case "code", "vscode", "visual studio code":
		return true
	default:
		return false
	}
}

// IsCursorTerminalName reports whether terminalName identifies the Cursor IDE.
func IsCursorTerminalName(terminalName string) bool {
	switch strings.ToLower(strings.TrimSpace(terminalName)) {
	case "cursor", "cursor ide":
		return true
	default:
		return false
	}
}

// IsWezTermTerminalName reports whether terminalName identifies WezTerm.
func IsWezTermTerminalName(terminalName string) bool {
	switch strings.ToLower(strings.TrimSpace(terminalName)) {
	case "wezterm", "wezterm-gui", "org.wezfurlong.wezterm", "org.wezfurlong.wezterm.desktop", "com.github.wez.wezterm":
		return true
	default:
		return false
	}
}

// GetWezTermFocusHints returns WezTerm-specific focus hints only when the
// detected focus target is WezTerm. WEZTERM_* variables are often inherited by
// GUI apps launched from WezTerm, so they must not be trusted for other targets.
func GetWezTermFocusHints(terminalName string) (paneID, socketPath string) {
	return normalizeWezTermFocusHints(terminalName, GetWezTermPaneID(), GetWezTermSocketPath())
}

func normalizeWezTermFocusHints(terminalName, paneID, socketPath string) (string, string) {
	if !IsWezTermTerminalName(terminalName) {
		return "", ""
	}
	return strings.TrimSpace(paneID), strings.TrimSpace(socketPath)
}

// IsKonsoleTerminalName reports whether terminalName identifies Konsole.
func IsKonsoleTerminalName(terminalName string) bool {
	return strings.EqualFold(strings.TrimSpace(terminalName), "konsole")
}

// GetKonsoleDBusService returns the D-Bus service name of the Konsole instance
// hosting the current shell ($KONSOLE_DBUS_SERVICE).
func GetKonsoleDBusService() string {
	return strings.TrimSpace(os.Getenv("KONSOLE_DBUS_SERVICE"))
}

// GetKonsoleDBusWindow returns the D-Bus object path of the Konsole window
// hosting the current shell ($KONSOLE_DBUS_WINDOW).
func GetKonsoleDBusWindow() string {
	return strings.TrimSpace(os.Getenv("KONSOLE_DBUS_WINDOW"))
}

// GetKonsoleDBusSession returns the D-Bus object path of the Konsole
// tab/session hosting the current shell ($KONSOLE_DBUS_SESSION), e.g. "/Sessions/2".
func GetKonsoleDBusSession() string {
	return strings.TrimSpace(os.Getenv("KONSOLE_DBUS_SESSION"))
}

// GetKonsoleFocusHints returns Konsole D-Bus tab-focus hints only when the
// detected focus target is Konsole. KONSOLE_DBUS_* variables are inherited by
// every child process of a Konsole tab (including GUI apps launched from it),
// so they must not be trusted for other focus targets.
func GetKonsoleFocusHints(terminalName string) (service, window, session string) {
	return normalizeKonsoleFocusHints(terminalName, GetKonsoleDBusService(), GetKonsoleDBusWindow(), GetKonsoleDBusSession())
}

func normalizeKonsoleFocusHints(terminalName, service, window, session string) (string, string, string) {
	if !IsKonsoleTerminalName(terminalName) {
		return "", "", ""
	}
	return strings.TrimSpace(service), strings.TrimSpace(window), strings.TrimSpace(session)
}
