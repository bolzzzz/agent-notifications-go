//go:build linux

package daemon

import (
	"reflect"
	"strings"
	"testing"
)

// --- GetFocusMethods tests ---

func TestGetFocusMethods_Order(t *testing.T) {
	methods := GetFocusMethods()

	expectedNames := []string{
		"activate-window-by-title extension",
		"GNOME Shell Eval (by window title)",
		"GNOME Shell Eval (by app)",
		"GNOME Shell FocusApp",
		"wlrctl",
		"KWin script",
		"xdotool",
	}

	if len(methods) != len(expectedNames) {
		t.Fatalf("GetFocusMethods() returned %d methods, want %d", len(methods), len(expectedNames))
	}

	for i, method := range methods {
		if method.Name != expectedNames[i] {
			t.Errorf("GetFocusMethods()[%d].Name = %q, want %q", i, method.Name, expectedNames[i])
		}
		if method.Fn == nil {
			t.Errorf("GetFocusMethods()[%d].Fn is nil", i)
		}
	}
}

func TestGetFocusMethods_NotEmpty(t *testing.T) {
	methods := GetFocusMethods()
	if len(methods) == 0 {
		t.Fatal("GetFocusMethods() returned empty slice")
	}
}

func TestGetFocusMethods_AllHaveFunctions(t *testing.T) {
	for _, m := range GetFocusMethods() {
		if m.Name == "" {
			t.Error("FocusMethod has empty Name")
		}
		if m.Fn == nil {
			t.Errorf("FocusMethod %q has nil Fn", m.Name)
		}
	}
}

func TestTryGnomeShellEval_SkipsCursorWithFolder(t *testing.T) {
	err := TryGnomeShellEval("Cursor", "model-for-maths")
	if err == nil {
		t.Fatal("expected skip error for Cursor with folder")
	}
	if !strings.Contains(err.Error(), "skipping app-level activate") {
		t.Errorf("error = %q, want skipping app-level activate", err)
	}
}

func TestTryGnomeFocusApp_SkipsCursorWithFolder(t *testing.T) {
	err := TryGnomeFocusApp("Cursor", "model-for-maths")
	if err == nil {
		t.Fatal("expected skip error for Cursor with folder")
	}
	if !strings.Contains(err.Error(), "skipping FocusApp") {
		t.Errorf("error = %q, want skipping FocusApp", err)
	}
}

func TestNormalizeX11WindowID_Decimal(t *testing.T) {
	got, err := normalizeX11WindowID("12345")
	if err != nil {
		t.Fatalf("normalizeX11WindowID returned error: %v", err)
	}
	if got != "12345" {
		t.Errorf("normalizeX11WindowID(decimal) = %q, want %q", got, "12345")
	}
}

func TestNormalizeX11WindowID_Hex(t *testing.T) {
	got, err := normalizeX11WindowID("0x3039")
	if err != nil {
		t.Fatalf("normalizeX11WindowID returned error: %v", err)
	}
	if got != "12345" {
		t.Errorf("normalizeX11WindowID(hex) = %q, want %q", got, "12345")
	}
}

func TestNormalizeX11WindowID_Invalid(t *testing.T) {
	if _, err := normalizeX11WindowID("not-a-window"); err == nil {
		t.Fatal("normalizeX11WindowID should reject invalid input")
	}
}

func TestBuildXdotoolSearches_Order(t *testing.T) {
	// With folderName: class searches first, then AND (class+name) searches.
	searches := buildXdotoolSearches("terminator", "project")

	got := make([][]string, 0, len(searches))
	for _, search := range searches {
		got = append(got, search.args)
	}

	want := [][]string{
		{"search", "--onlyvisible", "--class", "Terminator"},
		{"search", "--class", "Terminator"},
		{"search", "--all", "--onlyvisible", "--class", "Terminator", "--name", "project"},
		{"search", "--all", "--class", "Terminator", "--name", "project"},
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildXdotoolSearches() with folder = %#v, want %#v", got, want)
	}
}

func TestBuildXdotoolSearches_NoFolder(t *testing.T) {
	// Without folderName: class searches + generic name fallback.
	searches := buildXdotoolSearches("terminator", "")

	got := make([][]string, 0, len(searches))
	for _, search := range searches {
		got = append(got, search.args)
	}

	want := [][]string{
		{"search", "--onlyvisible", "--class", "Terminator"},
		{"search", "--class", "Terminator"},
		{"search", "--onlyvisible", "--name", "terminator"},
		{"search", "--name", "terminator"},
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildXdotoolSearches() no folder = %#v, want %#v", got, want)
	}
}

func TestSplitWindowIDs_TrimsBlankLines(t *testing.T) {
	got := splitWindowIDs("\n123\n 456 \n\n789\n")
	want := []string{"123", "456", "789"}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("splitWindowIDs() = %#v, want %#v", got, want)
	}
}

func TestTryKonsoleSession_InvalidSessionPath(t *testing.T) {
	err := TryKonsoleSession(":1.511", "/Windows/1", "/Sessions/abc")
	if err == nil {
		t.Fatal("TryKonsoleSession() with non-numeric session path: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "could not parse session id") {
		t.Errorf("TryKonsoleSession() error = %v, want it to mention parse failure", err)
	}
}
