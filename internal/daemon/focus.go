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
		{"kdotool", TryKdotool},
		{"xdotool", TryXdotool},
	}
}

// TryFocus attempts to focus a window using available tools.
// folderName is the project folder name used for title-based window search (may be empty).
// It tries each method in order until one succeeds.
func TryFocus(terminalName, folderName string) error {
	return TryFocusWithHints(terminalName, folderName, "", "", "", "")
}

// TryFocusWithWindowID preserves the previous API for callers that only have an exact X11 window ID.
func TryFocusWithWindowID(terminalName, folderName, windowID string) error {
	return TryFocusWithHints(terminalName, folderName, windowID, "", "", "")
}

// TryFocusWithHints attempts exact focus using hook-time hints first, then falls back to
// compositor-specific methods.
// wezTermPaneID and wezTermSocket enable tab-level focus for WezTerm.
//
// For WezTerm, window-level focus runs first, then the pane switch runs after a short
// delay. This ordering matters: GNOME's XDG Activation Token is processed asynchronously
// after the window-level call and may restore the previously active tab if the pane
// switch runs first. Running the pane switch last ensures it wins.
// If all window-level methods fail but a pane ID is available, TryWezTermPane is tried
// as a last resort (activate-pane also raises the window on WezTerm).
func TryFocusWithHints(terminalName, folderName, windowID, windowTitle, wezTermPaneID, wezTermSocket string) error {
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

	if !windowFocused {
		for _, method := range GetFocusMethods() {
			if err := method.Fn(terminalName, folderName); err != nil {
				lastErr = err
				continue
			}
			windowFocused = true
			break
		}
	}

	if wezTermPaneID != "" {
		// When multiple WezTerm windows are open, activateByWmClass may have raised
		// the wrong one (both share the same WM class). Query the mux for the window
		// title of the specific WezTerm window containing our pane, then use
		// activateBySubstring to bring exactly that window to the front.
		if wt := wezTermWindowTitle(wezTermPaneID, wezTermSocket); wt != "" {
			cmd := exec.Command("busctl", "--user", "call",
				"org.gnome.Shell",
				"/de/lucaswerkmeister/ActivateWindowByTitle",
				"de.lucaswerkmeister.ActivateWindowByTitle",
				"activateBySubstring", "s", wt,
			)
			cmd.CombinedOutput() //nolint:errcheck // best-effort; non-GNOME systems will fail here
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
//  1. activateBySubstring with the folder-specific term, when available — this
//     distinguishes multiple windows of the same app (e.g. two VS Code windows for
//     different projects). GetSearchTermWithFolder includes the app title suffix for
//     VS Code ("folder — Visual Studio Code") so it won't match browser windows whose
//     tab title happens to contain the folder name.
//  2. activateByWmClass — app-specific fallback for when no folder name is available
//     or the folder-specific search found no match.
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

	// Step 1: folder-specific search (e.g. "project - Visual Studio Code").
	// When the terminal supports folder-specific titles (e.g. VS Code), try the
	// precise search first. If it fails (e.g. VS Code opened as a workspace whose
	// title differs from the project folder name), fall through to WM-class and
	// generic searches rather than aborting — a best-effort focus is better than none.
	folderTerm := GetSearchTermWithFolder(terminalName, folderName)
	if folderTerm != GetSearchTerm(terminalName) {
		if gnomeActivate("activateBySubstring", folderTerm) {
			return nil
		}
		// Folder-specific search missed (e.g. workspace title mismatch); fall through.
	}

	// No folder-specific title available: fall back to WM class and generic searches.
	// These are safe when there is only one window of this app, or when any window will do.
	if wmClass := GetGnomeWmClass(terminalName); wmClass != "" {
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

// TryGnomeShellEval uses GNOME Shell's Eval method to activate an app.
// Requires unsafe_mode or development-tools enabled.
func TryGnomeShellEval(terminalName, folderName string) error {
	appID := escapeJS(GetAppID(terminalName))

	// JavaScript to find and activate the app's windows
	js := fmt.Sprintf(`
		(function() {
			let app = Shell.AppSystem.get_default().lookup_app('%s');
			if (app) {
				app.activate();
				return 'activated';
			}
			return 'app not found';
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
	if strings.Contains(outputStr, "false") && !strings.Contains(outputStr, "activated") {
		return fmt.Errorf("Shell.Eval blocked (GNOME 41+ security) - install unsafe-mode-menu extension or activate-window-by-title extension")
	}

	return nil
}

// TryGnomeFocusApp uses GNOME Shell's FocusApp method (available since GNOME 45).
func TryGnomeFocusApp(terminalName, folderName string) error {
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

// TryKdotool uses kdotool for KDE Plasma.
func TryKdotool(terminalName, folderName string) error {
	if _, err := exec.LookPath("kdotool"); err != nil {
		return fmt.Errorf("kdotool not installed")
	}

	// Search by class
	className := GetKdotoolClass(terminalName)
	searchCmd := exec.Command("kdotool", "search", "--class", className)
	output, err := searchCmd.CombinedOutput()
	outputStr := strings.TrimSpace(string(output))

	if err != nil || outputStr == "" {
		return fmt.Errorf("no windows found via kdotool")
	}

	windowIDs := strings.Split(outputStr, "\n")

	cmd := exec.Command("kdotool", "windowactivate", windowIDs[0])
	if _, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("kdotool windowactivate failed: %w", err)
	}
	return nil
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
