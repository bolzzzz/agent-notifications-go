//go:build linux

// ABOUTME: Window focus methods for Linux desktop environments.
// ABOUTME: Implements a fallback chain to focus windows on GNOME, KDE, Sway, and other compositors.
package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
)

// FocusMethod represents a method for focusing a window
type FocusMethod struct {
	Name string
	Fn   func(terminalName, folderName string) error
}

// GetFocusMethods returns the ordered list of focus methods to try
func GetFocusMethods() []FocusMethod {
	return []FocusMethod{
		{"activate-window-by-title extension", TryActivateWindowByTitle},
		{"GNOME Shell Eval (by window title)", TryGnomeShellEvalByTitle},
		{"GNOME Shell Eval (by app)", TryGnomeShellEval},
		{"GNOME Shell FocusApp", TryGnomeFocusApp},
		{"wlrctl", TryWlrctl},
		{"KWin script", TryKwinScript},
		{"xdotool", TryXdotool},
	}
}

// TryFocus attempts to focus a window using available tools.
// folderName is the project folder name used for title-based window search (may be empty).
// It tries each method in order until one succeeds.
func TryFocus(terminalName, folderName string) error {
	return TryFocusWithHints(terminalName, folderName, "", "", "", "", "", "")
}

// TryFocusWithWindowID preserves the previous API for callers that only have an exact X11 window ID.
func TryFocusWithWindowID(terminalName, folderName, windowID string) error {
	return TryFocusWithHints(terminalName, folderName, "", "", windowID, "", "", "")
}

// TryFocusWithHints attempts exact focus using hook-time hints first, then falls back to
// compositor-specific methods.
// cwdFolderName is the current cwd folder to try if folderName-based search fails.
// workspaceName is the VS Code workspace name to try if folder searches fail.
// wezTermPaneID and wezTermSocket enable tab-level focus for WezTerm.
//
// For WezTerm, window-level focus runs first, then the pane switch runs after a short
// delay. This ordering matters: GNOME's XDG Activation Token is processed asynchronously
// after the window-level call and may restore the previously active tab if the pane
// switch runs first. Running the pane switch last ensures it wins.
// If all window-level methods fail but a pane ID is available, TryWezTermPane is tried
// as a last resort (activate-pane also raises the window on WezTerm).
func TryFocusWithHints(terminalName, folderName, cwdFolderName, workspaceName, windowID, windowTitle, wezTermPaneID, wezTermSocket string) error {
	wezTermPaneID, wezTermSocket = normalizeWezTermFocusHints(terminalName, wezTermPaneID, wezTermSocket)
	windowFocused := false
	var exactErr, lastErr error

	if strings.TrimSpace(windowID) != "" {
		if err := tryX11WindowID(windowID); err == nil {
			windowFocused = true
		} else {
			exactErr = err
		}
	}

	if !windowFocused && strings.TrimSpace(windowTitle) != "" {
		if err := tryWindowTitle(windowTitle); err == nil {
			windowFocused = true
		} else if exactErr != nil {
			exactErr = fmt.Errorf("%v; exact title focus failed: %v", exactErr, err)
		} else {
			exactErr = fmt.Errorf("exact title focus failed: %v", err)
		}
	}

	// When a WezTerm pane ID is available, skip the generic focus loop entirely.
	// The WezTerm-specific block below queries the mux for the exact window title and
	// raises only that window. Running the generic loop first would raise a WezTerm
	// instance via activateByWmClass (all instances share the same WM class), which
	// brings the wrong window to the foreground before the correct one is raised.
	if !windowFocused && wezTermPaneID == "" {
		// Try the session/project folder first (initial cwd), then the current cwd
		// folder, then the workspace name. This covers VS Code sessions that start in
		// folderA and later cd into folderA/subFolderB.
		for _, candidate := range uniqueNonEmpty(folderName, cwdFolderName, workspaceName) {
			for _, method := range GetFocusMethods() {
				if err := method.Fn(terminalName, candidate); err != nil {
					lastErr = err
					continue
				}
				windowFocused = true
				break
			}
			if windowFocused {
				break
			}
		}
	}

	if wezTermPaneID != "" {
		// Query the mux for the window title of the specific WezTerm window containing
		// our pane, then use activateBySubstring to raise exactly that window.
		// This avoids raising the wrong instance when multiple WezTerm windows are open.
		//
		// wezterm's reported window_title can lag behind which tab is actually
		// displayed (e.g. it keeps showing the title of whichever pane most recently
		// pushed an OSC title update, even if a different tab is now focused). When
		// that happens the substring won't match the window's real current title, so
		// activateBySubstring returns false and no window is raised. Fall back to
		// raising any WezTerm window by WM class — the pane switch below still lands
		// on the correct tab, so this only matters for picking the right window when
		// multiple WezTerm windows are open.
		raised := false
		if wt := wezTermWindowTitle(wezTermPaneID, wezTermSocket); wt != "" {
			raised = gnomeActivateWindow("activateBySubstring", wt)
		}
		if !raised {
			gnomeActivateWindow("activateByWmClass", GetGnomeWmClass(terminalName))
		}

		// Sleep briefly so GNOME's XDG Activation Token is processed before switching
		// tabs — otherwise the token may restore the previously active tab and undo
		// the pane switch.
		time.Sleep(150 * time.Millisecond)
		if err := TryWezTermPane(wezTermPaneID, wezTermSocket); err != nil && !windowFocused {
			// Neither window-level focus nor pane switch succeeded.
			if exactErr != nil && lastErr != nil {
				return fmt.Errorf("%v; fallback focus failed, last error: %v; wezterm pane: %v", exactErr, lastErr, err)
			}
			if exactErr != nil {
				return fmt.Errorf("%v; wezterm pane: %v", exactErr, err)
			}
			if lastErr != nil {
				return fmt.Errorf("all focus methods failed, last error: %v; wezterm pane: %v", lastErr, err)
			}
			return fmt.Errorf("wezterm pane focus failed: %v", err)
		}
		// Pane switch succeeded, or window was already raised (pane switch is best-effort).
		return nil
	}

	if !windowFocused {
		if exactErr != nil && lastErr != nil {
			return fmt.Errorf("%v; fallback focus failed, last error: %v", exactErr, lastErr)
		}
		if exactErr != nil {
			return exactErr
		}
		return fmt.Errorf("all focus methods failed, last error: %v", lastErr)
	}
	return nil
}

func uniqueNonEmpty(values ...string) []string {
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

// gnomeActivateWindow calls the activate-window-by-title GNOME extension method
// (activateBySubstring, activateByWmClass, ...) and reports whether it actually
// raised a window. busctl succeeds (exit 0) even when no window matched, so the
// boolean result must be parsed out of its output.
func gnomeActivateWindow(method, arg string) bool {
	cmd := exec.Command("busctl", "--user", "call",
		"org.gnome.Shell",
		"/de/lucaswerkmeister/ActivateWindowByTitle",
		"de.lucaswerkmeister.ActivateWindowByTitle",
		method, "s", arg,
	)
	out, err := cmd.CombinedOutput()
	return err == nil && strings.Contains(strings.TrimSpace(string(out)), "true")
}

// wezTermWindowTitle queries the WezTerm mux for the window title of the window
// containing paneID. Used to raise the exact WezTerm window when multiple instances
// are open (they share the same WM class and can't be distinguished via activateByWmClass).
// Returns empty string on any failure.
func wezTermWindowTitle(paneID, socketPath string) string {
	paneIDInt, err := strconv.Atoi(paneID)
	if err != nil {
		return ""
	}
	cmd := exec.Command("wezterm", "cli", "--no-auto-start", "list", "--format", "json")
	if strings.TrimSpace(socketPath) != "" {
		cmd.Env = append(os.Environ(), "WEZTERM_UNIX_SOCKET="+socketPath)
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	var panes []struct {
		PaneID      int    `json:"pane_id"`
		WindowTitle string `json:"window_title"`
	}
	if err := json.Unmarshal(output, &panes); err != nil {
		return ""
	}
	for _, p := range panes {
		if p.PaneID == paneIDInt {
			return p.WindowTitle
		}
	}
	return ""
}

// TryWezTermPane activates a specific WezTerm pane by ID using the WezTerm CLI.
// This switches to the exact tab/pane where Claude is running.
// socketPath is passed via WEZTERM_UNIX_SOCKET env var (the CLI has no --unix-socket flag).
func TryWezTermPane(paneID, socketPath string) error {
	if _, err := exec.LookPath("wezterm"); err != nil {
		return fmt.Errorf("wezterm not installed")
	}

	// --no-auto-start: fail instead of spawning a new mux server when socket is wrong.
	cmd := exec.Command("wezterm", "cli", "--no-auto-start", "activate-pane", "--pane-id", paneID)
	if strings.TrimSpace(socketPath) != "" {
		cmd.Env = append(os.Environ(), "WEZTERM_UNIX_SOCKET="+socketPath)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("wezterm cli activate-pane failed: %w, output: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func tryX11WindowID(windowID string) error {
	normalizedID, err := normalizeX11WindowID(windowID)
	if err != nil {
		return fmt.Errorf("invalid X11 window id %q: %w", windowID, err)
	}

	var errs []string

	if err := activateWindowIDWithXdotool(normalizedID); err == nil {
		return nil
	} else {
		errs = append(errs, err.Error())
	}

	if err := activateWindowIDWithWmctrl(normalizedID); err == nil {
		return nil
	} else {
		errs = append(errs, err.Error())
	}

	return fmt.Errorf("exact X11 focus failed: %s", strings.Join(errs, "; "))
}

func activateWindowIDWithXdotool(windowID string) error {
	if _, err := exec.LookPath("xdotool"); err != nil {
		return fmt.Errorf("xdotool not installed")
	}

	cmd := exec.Command("xdotool", "windowactivate", "--sync", windowID)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("xdotool windowactivate failed: %w, output: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func activateWindowIDWithWmctrl(windowID string) error {
	if _, err := exec.LookPath("wmctrl"); err != nil {
		return fmt.Errorf("wmctrl not installed")
	}

	cmd := exec.Command("wmctrl", "-i", "-a", windowID)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("wmctrl -i -a failed: %w, output: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func normalizeX11WindowID(windowID string) (string, error) {
	windowID = strings.TrimSpace(windowID)
	if windowID == "" {
		return "", fmt.Errorf("empty window id")
	}

	id, err := strconv.ParseUint(windowID, 0, 64)
	if err != nil {
		return "", err
	}

	return strconv.FormatUint(id, 10), nil
}

func tryWindowTitle(windowTitle string) error {
	windowTitle = strings.TrimSpace(windowTitle)
	if windowTitle == "" {
		return fmt.Errorf("empty window title")
	}

	var errs []string

	if err := activateWindowTitleWithWmctrl(windowTitle); err == nil {
		return nil
	} else {
		errs = append(errs, err.Error())
	}

	if err := activateWindowTitleWithXdotool(windowTitle); err == nil {
		return nil
	} else {
		errs = append(errs, err.Error())
	}

	return fmt.Errorf("exact title focus failed: %s", strings.Join(errs, "; "))
}

func activateWindowTitleWithWmctrl(windowTitle string) error {
	if _, err := exec.LookPath("wmctrl"); err != nil {
		return fmt.Errorf("wmctrl not installed")
	}

	cmd := exec.Command("wmctrl", "-F", "-a", windowTitle)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("wmctrl -F -a failed: %w, output: %s", err, strings.TrimSpace(string(output)))
	}

	return nil
}

func activateWindowTitleWithXdotool(windowTitle string) error {
	if _, err := exec.LookPath("xdotool"); err != nil {
		return fmt.Errorf("xdotool not installed")
	}

	exactRegex := "^" + regexp.QuoteMeta(windowTitle) + "$"
	windowIDs, err := runXdotoolSearch("search", "--name", exactRegex)
	if err != nil {
		return err
	}
	if len(windowIDs) == 0 {
		return fmt.Errorf("no windows found for exact title")
	}

	for i := len(windowIDs) - 1; i >= 0; i-- {
		windowID := windowIDs[i]
		if err := activateWindowIDWithXdotool(windowID); err == nil {
			return nil
		}
	}

	return fmt.Errorf("xdotool could not activate any exact-title match")
}

// TryActivateWindowByTitle uses the activate-window-by-title GNOME extension.
// https://extensions.gnome.org/extension/5021/activate-window-by-title/
// This method does NOT require unsafe_mode and works on GNOME 42+.
//
// Search order:
//  1. activateBySubstring with folder-specific terms, when available — this
//     distinguishes multiple windows of the same app (e.g. two VS Code windows for
//     different projects). Precise terms include the app title suffix
//     ("folder - Visual Studio Code") so they won't match browser tabs that happen
//     to contain the folder name. A "folder - " fallback covers custom titles
//     that insert profileName between the folder and the app name.
//  2. activateByWmClass — app-specific fallback only when no folder name is
//     available. Never used as a fallback after a folder search miss: that would
//     raise an arbitrary window of the same app.
//  3. activateBySubstring with the generic terminal name as a final fallback.
func TryActivateWindowByTitle(terminalName, folderName string) error {
	gnomeActivate := func(method, arg string) bool {
		cmd := exec.Command("busctl", "--user", "call",
			"org.gnome.Shell",
			"/de/lucaswerkmeister/ActivateWindowByTitle",
			"de.lucaswerkmeister.ActivateWindowByTitle",
			method, "s", arg,
		)
		output, err := cmd.CombinedOutput()
		return err == nil && strings.Contains(strings.TrimSpace(string(output)), "true")
	}

	if terms := folderTitleSearchTerms(terminalName, folderName); len(terms) > 0 {
		for _, term := range terms {
			if gnomeActivate("activateBySubstring", term) {
				return nil
			}
		}
		// Folder-specific searches failed. Return an error so subsequent focus
		// methods (TryGnomeShellEvalByTitle, etc.) can attempt their own strategies.
		// Do NOT fall through to activateByWmClass here — that would focus the wrong
		// window when multiple instances of the same app are open.
		return fmt.Errorf("activate-window-by-title: no window matching %q or variants", terms[0])
	}

	// No folder-specific title available: fall back to WM class and generic searches.
	// These are safe when there is only one window of this app, or when any window will do.
	// Electron apps (Cursor/VS Code) report WM_CLASS as either "cursor"/"code" or
	// "Cursor"/"Code"; the extension uses strict equality, so try both.
	for _, wmClass := range gnomeWmClassCandidates(terminalName) {
		if gnomeActivate("activateByWmClass", wmClass) {
			return nil
		}
	}

	// Generic substring fallback.
	genericTerm := GetSearchTerm(terminalName)
	cmd := exec.Command("busctl", "--user", "call",
		"org.gnome.Shell",
		"/de/lucaswerkmeister/ActivateWindowByTitle",
		"de.lucaswerkmeister.ActivateWindowByTitle",
		"activateBySubstring", "s", genericTerm,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("activate-window-by-title extension not available: %w, output: %s", err, string(output))
	}
	// busctl can succeed (exit 0) even when no window was activated; check the boolean.
	outputStr := strings.TrimSpace(string(output))
	if strings.Contains(outputStr, "false") || outputStr == "" {
		return fmt.Errorf("activate-window-by-title: no window activated for %q (output: %s)", genericTerm, outputStr)
	}
	return nil
}

// TryGnomeShellEvalByTitle uses GNOME Shell's Eval to find and focus window by title.
// Requires unsafe_mode or development-tools enabled.
//
// When a folder name is available, searches for windows whose title contains the folder
// name AND whose WM class matches the terminal — equivalent to an AND of two conditions
// without relying on a fixed separator format (e.g. the em dash in VS Code titles).
// When no folder name is available, falls back to the generic app search term.
//
// All terms are filtered by WM class, so browser windows are never matched.
func TryGnomeShellEvalByTitle(terminalName, folderName string) error {
	wmClass := escapeJS(GetGnomeWmClass(terminalName))

	// Use folderName as the title search term when available.
	// The WM class filter already anchors the match to the correct app, so there is
	// no need to construct a compound string like "folder — Visual Studio Code".
	// This handles custom window title formats that insert extra segments.
	searchTerm := folderName
	if searchTerm == "" {
		searchTerm = GetSearchTerm(terminalName)
	}
	termsJS := fmt.Sprintf(`['%s']`, escapeJS(searchTerm))

	// Filter by both title substring and WM class to avoid accidentally focusing
	// browser windows whose tab titles contain the search term (e.g. GitHub pages).
	js := fmt.Sprintf(`
		(function() {
			let start = Date.now();
			let terms = %s;
			let wmClass = '%s';
			for (let i = 0; i < terms.length; i++) {
				let activated = false;
				global.get_window_actors().forEach(function(actor) {
					if (activated) return;
					let win = actor.get_meta_window();
					let title = win.get_title() || '';
					let wm = (win.get_wm_class() || '').toLowerCase();
					if (title.indexOf(terms[i]) !== -1 && wm.indexOf(wmClass) !== -1) {
						win.activate(start);
						activated = true;
					}
				});
				if (activated) return 'activated';
			}
			return 'no matching window';
		})()
	`, termsJS, wmClass)

	cmd := exec.Command("gdbus", "call",
		"--session",
		"--dest", "org.gnome.Shell",
		"--object-path", "/org/gnome/Shell",
		"--method", "org.gnome.Shell.Eval",
		js,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("gdbus Eval failed: %w, output: %s", err, string(output))
	}

	outputStr := string(output)
	if strings.Contains(outputStr, "no matching window") {
		return fmt.Errorf("no window with title containing %q", searchTerm)
	}
	if strings.Contains(outputStr, "false") && !strings.Contains(outputStr, "activated") {
		return fmt.Errorf("Shell.Eval blocked (GNOME 41+ security) - install unsafe-mode-menu extension or activate-window-by-title extension")
	}

	return nil
}

// TryGnomeShellEval uses GNOME Shell's Eval method to raise an existing app
// window. Requires unsafe_mode or development-tools enabled.
//
// When a folder name is set this is skipped: app-level activation cannot pick
// the right window among several projects, and GNOME's App.activate() falls
// through to open_new_window() when it does not associate existing Electron
// windows with the .desktop file. Cursor's desktop file ships a
// new-empty-window action (`cursor --new-window`), so that path spawns a
// duplicate window of the same project instead of restoring the existing one.
func TryGnomeShellEval(terminalName, folderName string) error {
	if strings.TrimSpace(folderName) != "" && (IsVSCodeTerminalName(terminalName) || IsCursorTerminalName(terminalName)) {
		return fmt.Errorf("skipping app-level activate for %s with folder %q (would not restore a specific window)", terminalName, folderName)
	}

	appID := escapeJS(GetAppID(terminalName))

	// Only raise an already-open window. Never call app.activate() — that
	// launches a new instance when GNOME sees the app as STOPPED.
	js := fmt.Sprintf(`
		(function() {
			let app = Shell.AppSystem.get_default().lookup_app('%s');
			if (!app) {
				return 'app not found';
			}
			let windows = app.get_windows();
			if (!windows || windows.length === 0) {
				return 'no windows';
			}
			windows[0].activate(global.get_current_time());
			return 'activated';
		})()
	`, appID)

	cmd := exec.Command("gdbus", "call",
		"--session",
		"--dest", "org.gnome.Shell",
		"--object-path", "/org/gnome/Shell",
		"--method", "org.gnome.Shell.Eval",
		js,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("gdbus Eval failed: %w, output: %s", err, string(output))
	}

	outputStr := string(output)
	if strings.Contains(outputStr, "app not found") {
		return fmt.Errorf("app not found via Shell.Eval")
	}
	if strings.Contains(outputStr, "no windows") {
		return fmt.Errorf("Shell.Eval found no existing windows for %q", appID)
	}
	if strings.Contains(outputStr, "false") && !strings.Contains(outputStr, "activated") {
		return fmt.Errorf("Shell.Eval blocked (GNOME 41+ security) - install unsafe-mode-menu extension or activate-window-by-title extension")
	}

	return nil
}

// TryGnomeFocusApp uses GNOME Shell's FocusApp method (available since GNOME 45).
// On modern GNOME this only highlights the app in the overview — it does not
// raise a window, and on GNOME 50 it is restricted to privileged senders.
// Skip it for multi-window editors when we know which folder to target.
func TryGnomeFocusApp(terminalName, folderName string) error {
	if strings.TrimSpace(folderName) != "" && (IsVSCodeTerminalName(terminalName) || IsCursorTerminalName(terminalName)) {
		return fmt.Errorf("skipping FocusApp for %s with folder %q (overview select, not window restore)", terminalName, folderName)
	}

	appID := GetAppID(terminalName)

	cmd := exec.Command("gdbus", "call",
		"--session",
		"--dest", "org.gnome.Shell",
		"--object-path", "/org/gnome/Shell",
		"--method", "org.gnome.Shell.FocusApp",
		appID,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("gdbus FocusApp failed: %w, output: %s", err, string(output))
	}
	outputStr := strings.TrimSpace(string(output))
	if strings.Contains(outputStr, "false") || outputStr == "" {
		return fmt.Errorf("gdbus FocusApp reported no activation for %q (output: %s)", appID, outputStr)
	}
	return nil
}

// TryWlrctl uses wlrctl for wlroots-based compositors (Sway, etc.).
func TryWlrctl(terminalName, folderName string) error {
	if _, err := exec.LookPath("wlrctl"); err != nil {
		return fmt.Errorf("wlrctl not installed")
	}

	appID := GetWlrctlAppID(terminalName)

	// When a folder name is available, combine app_id and title filters (AND) to
	// distinguish multiple windows of the same app open for different projects.
	// wlrctl supports multiple filter arguments natively.
	if folderName != "" {
		cmd := exec.Command("wlrctl", "toplevel", "focus", "app_id:"+appID, "title:"+folderName)
		if err := cmd.Run(); err == nil {
			return nil
		}
	}

	// Fallback: match by app_id alone (focuses whichever window is on top).
	cmd := exec.Command("wlrctl", "toplevel", "focus", "app_id:"+appID)
	if err := cmd.Run(); err == nil {
		return nil
	}

	// Last resort: title-only search using the generic app name.
	searchTerm := GetSearchTerm(terminalName)
	cmd = exec.Command("wlrctl", "toplevel", "focus", "title:"+searchTerm)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("wlrctl failed: %w, output: %s", err, string(output))
	}
	return nil
}

// kwinScriptCallback receives the "Result"/"Error" callDBus messages emitted by a KWin
// script loaded via TryKwinScript. It is exported on a dedicated session bus connection
// at path "/" with an empty interface name, matching how the generated script's callDBus
// calls address it (no interface header, resolved by member name only).
type kwinScriptCallback struct {
	result chan string
	errMsg chan string
}

func (c *kwinScriptCallback) Result(message string) *dbus.Error {
	select {
	case c.result <- message:
	default:
	}
	return nil
}

func (c *kwinScriptCallback) Error(message string) *dbus.Error {
	select {
	case c.errMsg <- message:
	default:
	}
	return nil
}

// TryKwinScript activates a window by WM class using KWin's own D-Bus scripting service
// (org.kde.KWin /Scripting), the same official mechanism the external kdotool binary
// wraps. This avoids depending on that binary being installed.
//
// KWin's Wayland scripting API (workspace.windowList(), the workspace.activeWindow
// setter) is Plasma 6 only, so this is skipped outright on Plasma 5 rather than
// attempting a script that would fail against a different API shape.
func TryKwinScript(terminalName, folderName string) error {
	if os.Getenv("KDE_SESSION_VERSION") != "6" {
		return fmt.Errorf("KWin scripting focus requires Plasma 6 (KDE_SESSION_VERSION=%q)", os.Getenv("KDE_SESSION_VERSION"))
	}

	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return fmt.Errorf("kwin script: failed to connect to session bus: %w", err)
	}
	defer conn.Close()

	cb := &kwinScriptCallback{result: make(chan string, 1), errMsg: make(chan string, 1)}
	if err := conn.Export(cb, "/", ""); err != nil {
		return fmt.Errorf("kwin script: failed to export callback: %w", err)
	}
	defer conn.Export(nil, "/", "")

	busName := conn.Names()[0]
	wmClass := escapeJS(strings.ToLower(GetKdotoolClass(terminalName)))
	titleTerm := escapeJS(strings.ToLower(folderName))
	// When a folder name is available, require it in the title on the first pass so
	// multiple windows of the same app (e.g. several VS Code projects) don't collide —
	// same reasoning as TryGnomeShellEvalByTitle. Only fall back to a class-only match
	// (whichever matching window comes first) when no title match exists.
	script := fmt.Sprintf(`
		(function() {
			var windows = workspace.windowList();
			var wmClass = '%s';
			var titleTerm = '%s';
			function findMatch(requireTitle) {
				for (var i = 0; i < windows.length; i++) {
					var w = windows[i];
					var cls = (w.resourceClass || '').toLowerCase();
					if (cls.indexOf(wmClass) === -1) {
						continue;
					}
					if (requireTitle && (w.caption || '').toLowerCase().indexOf(titleTerm) === -1) {
						continue;
					}
					return w;
				}
				return null;
			}
			var w = titleTerm !== '' ? findMatch(true) : null;
			if (!w) {
				w = findMatch(false);
			}
			if (w) {
				workspace.activeWindow = w;
				callDBus('%s', '/', '', 'Result', 'activated');
			} else {
				callDBus('%s', '/', '', 'Result', 'no matching window');
			}
		})();
	`, wmClass, titleTerm, busName, busName)

	scriptFile, err := os.CreateTemp("", "agent-notifications-kwin-*.js")
	if err != nil {
		return fmt.Errorf("kwin script: failed to create temp script: %w", err)
	}
	defer os.Remove(scriptFile.Name())
	if _, err := scriptFile.WriteString(script); err != nil {
		scriptFile.Close()
		return fmt.Errorf("kwin script: failed to write temp script: %w", err)
	}
	scriptFile.Close()

	scriptName := fmt.Sprintf("agent-notifications-%d", time.Now().UnixNano())
	scriptingObj := conn.Object("org.kde.KWin", "/Scripting")

	var scriptID int32
	if err := scriptingObj.Call("org.kde.kwin.Scripting.loadScript", 0, scriptFile.Name(), scriptName).Store(&scriptID); err != nil {
		return fmt.Errorf("kwin script: loadScript failed: %w", err)
	}
	if scriptID < 0 {
		return fmt.Errorf("kwin script: loadScript returned invalid id %d", scriptID)
	}
	defer scriptingObj.Call("org.kde.kwin.Scripting.unloadScript", 0, scriptName)

	scriptObj := conn.Object("org.kde.KWin", dbus.ObjectPath(fmt.Sprintf("/Scripting/Script%d", scriptID)))
	if call := scriptObj.Call("org.kde.kwin.Script.run", 0); call.Err != nil {
		return fmt.Errorf("kwin script: run failed: %w", call.Err)
	}

	select {
	case msg := <-cb.result:
		if msg != "activated" {
			return fmt.Errorf("kwin script: %s", msg)
		}
		return nil
	case msg := <-cb.errMsg:
		return fmt.Errorf("kwin script error: %s", msg)
	case <-time.After(3 * time.Second):
		return fmt.Errorf("kwin script: timed out waiting for result")
	}
}

// TryXdotool uses xdotool for X11-based desktop environments
// (XFCE, MATE, Cinnamon, i3, bspwm, and X11 sessions of GNOME/KDE).
func TryXdotool(terminalName, folderName string) error {
	if _, err := exec.LookPath("xdotool"); err != nil {
		return fmt.Errorf("xdotool not installed")
	}

	searches := buildXdotoolSearches(terminalName, folderName)
	seenIDs := make(map[string]struct{})
	foundMatch := false
	var errs []string

	for _, search := range searches {
		windowIDs, err := runXdotoolSearch(search.args...)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", search.label, err))
			continue
		}
		if len(windowIDs) == 0 {
			continue
		}

		foundMatch = true
		windowIDs = prioritizeXdotoolCandidates(windowIDs, search.label, folderName)

		// xdotool returns bottom-most windows first; prefer the top-most candidate.
		for i := len(windowIDs) - 1; i >= 0; i-- {
			windowID := windowIDs[i]
			if _, exists := seenIDs[windowID]; exists {
				continue
			}
			seenIDs[windowID] = struct{}{}

			if err := activateWindowIDWithXdotool(windowID); err == nil {
				return nil
			} else {
				errs = append(errs, fmt.Sprintf("%s (%s): %v", search.label, windowID, err))
			}
		}
	}

	if !foundMatch {
		return fmt.Errorf("no windows found via xdotool")
	}

	return fmt.Errorf("xdotool could not activate any matching window: %s", strings.Join(errs, "; "))
}

type xdotoolSearch struct {
	label string
	args  []string
}

func buildXdotoolSearches(terminalName, folderName string) []xdotoolSearch {
	className := GetXdotoolClass(terminalName)

	searches := []xdotoolSearch{
		{
			label: "visible class search",
			args:  []string{"search", "--onlyvisible", "--class", className},
		},
		{
			label: "class search",
			args:  []string{"search", "--class", className},
		},
	}

	if folderName != "" {
		// AND condition: class + folder name in title independently.
		// Distinguishes multiple windows of the same app open for different projects.
		searches = append(searches,
			xdotoolSearch{
				label: "visible class+name search",
				args:  []string{"search", "--all", "--onlyvisible", "--class", className, "--name", folderName},
			},
			xdotoolSearch{
				label: "class+name search",
				args:  []string{"search", "--all", "--class", className, "--name", folderName},
			},
		)
	} else {
		// No folder name: fall back to generic app name search.
		genericTerm := GetSearchTerm(terminalName)
		if genericTerm != "" {
			searches = append(searches,
				xdotoolSearch{
					label: "visible name search",
					args:  []string{"search", "--onlyvisible", "--name", genericTerm},
				},
				xdotoolSearch{
					label: "name search",
					args:  []string{"search", "--name", genericTerm},
				},
			)
		}
	}

	return searches
}

func runXdotoolSearch(args ...string) ([]string, error) {
	cmd := exec.Command("xdotool", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%w, output: %s", err, strings.TrimSpace(string(output)))
	}

	outputStr := strings.TrimSpace(string(output))
	if outputStr == "" {
		return nil, nil
	}

	return splitWindowIDs(outputStr), nil
}

func splitWindowIDs(output string) []string {
	lines := strings.Split(output, "\n")
	ids := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		ids = append(ids, line)
	}
	return ids
}

func prioritizeXdotoolCandidates(windowIDs []string, searchLabel, folderName string) []string {
	if folderName == "" {
		return windowIDs
	}
	if !strings.Contains(searchLabel, "class search") {
		return windowIDs
	}

	matching := make([]string, 0, len(windowIDs))
	nonMatching := make([]string, 0, len(windowIDs))
	for _, windowID := range windowIDs {
		title := getXdotoolWindowName(windowID)
		if title != "" && strings.Contains(title, folderName) {
			matching = append(matching, windowID)
			continue
		}
		nonMatching = append(nonMatching, windowID)
	}

	if len(matching) == 0 {
		return windowIDs
	}

	return append(matching, nonMatching...)
}

func getXdotoolWindowName(windowID string) string {
	cmd := exec.Command("xdotool", "getwindowname", windowID)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

// DetectFocusTools returns a map of available focus tools.
func DetectFocusTools() map[string]bool {
	tools := map[string]bool{}

	// Check command-line tools
	for _, tool := range []string{"wlrctl", "kdotool", "xdotool", "wmctrl", "gdbus", "busctl"} {
		_, err := exec.LookPath(tool)
		tools[tool] = err == nil
	}

	// Check GNOME activate-window-by-title extension
	cmd := exec.Command("busctl", "--user", "introspect",
		"org.gnome.Shell",
		"/de/lucaswerkmeister/ActivateWindowByTitle",
	)
	output, err := cmd.CombinedOutput()
	tools["activate-window-by-title"] = err == nil && strings.Contains(string(output), "activateBySubstring")

	return tools
}
