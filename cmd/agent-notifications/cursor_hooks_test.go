package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readCursorHooks decodes the events and their command strings from a hooks
// file written by updateCursorHooksFile.
func readCursorHooks(t *testing.T, path string) map[string][]string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read hooks file: %v", err)
	}

	var root struct {
		Version int `json:"version"`
		Hooks   map[string][]struct {
			Command string `json:"command"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("parse hooks file: %v", err)
	}
	if root.Version != cursorHooksVersion {
		t.Errorf("version = %d, want %d", root.Version, cursorHooksVersion)
	}

	out := make(map[string][]string, len(root.Hooks))
	for event, entries := range root.Hooks {
		for _, entry := range entries {
			out[event] = append(out[event], entry.Command)
		}
	}
	return out
}

func TestUpdateCursorHooksFile_RegistersEveryEvent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hooks.json")
	if err := updateCursorHooksFile(path, "/opt/agent-notifications", false); err != nil {
		t.Fatalf("install: %v", err)
	}

	hooks := readCursorHooks(t, path)
	if len(hooks) != len(cursorHookSpecs) {
		t.Errorf("registered %d events, want %d: %v", len(hooks), len(cursorHookSpecs), hooks)
	}

	for _, spec := range cursorHookSpecs {
		commands, ok := hooks[spec.event]
		if !ok {
			t.Errorf("event %q not registered", spec.event)
			continue
		}
		if len(commands) != 1 {
			t.Errorf("event %q has %d entries, want 1", spec.event, len(commands))
			continue
		}
		if !strings.Contains(commands[0], "handle-hook "+spec.hookName) {
			t.Errorf("event %q command = %q, want it to invoke handle-hook %s", spec.event, commands[0], spec.hookName)
		}
		if !strings.Contains(commands[0], "--product cursor") {
			t.Errorf("event %q command = %q, want --product cursor", spec.event, commands[0])
		}
	}
}

// The approval-wait detection only works when both halves of each gate are
// registered: the before* gate starts the watcher and the after* event is the
// signal that cancels it.
func TestUpdateCursorHooksFile_RegistersApprovalGatePairs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hooks.json")
	if err := updateCursorHooksFile(path, "/opt/agent-notifications", false); err != nil {
		t.Fatalf("install: %v", err)
	}

	hooks := readCursorHooks(t, path)
	for _, event := range []string{
		"beforeShellExecution", "afterShellExecution",
		"beforeMCPExecution", "afterMCPExecution",
	} {
		if _, ok := hooks[event]; !ok {
			t.Errorf("approval gate event %q not registered", event)
		}
	}
}

func TestUpdateCursorHooksFile_ReinstallIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hooks.json")
	for i := 0; i < 3; i++ {
		if err := updateCursorHooksFile(path, "/opt/agent-notifications", false); err != nil {
			t.Fatalf("install %d: %v", i, err)
		}
	}

	for event, commands := range readCursorHooks(t, path) {
		if len(commands) != 1 {
			t.Errorf("event %q has %d entries after 3 installs, want 1", event, len(commands))
		}
	}
}

func TestUpdateCursorHooksFile_PreservesUnrelatedContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hooks.json")
	existing := `{
  "version": 1,
  "customField": {"keep": true},
  "hooks": {
    "stop": [{"command": "./my-own-stop.sh"}],
    "afterFileEdit": [{"command": "./format.sh"}]
  }
}`
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatalf("seed hooks file: %v", err)
	}

	if err := updateCursorHooksFile(path, "/opt/agent-notifications", false); err != nil {
		t.Fatalf("install: %v", err)
	}

	hooks := readCursorHooks(t, path)
	if got := hooks["afterFileEdit"]; len(got) != 1 || got[0] != "./format.sh" {
		t.Errorf("afterFileEdit = %v, want the pre-existing ./format.sh entry", got)
	}
	if got := hooks["stop"]; len(got) != 2 {
		t.Errorf("stop has %d entries, want the pre-existing one plus ours: %v", len(got), got)
	}

	var root map[string]json.RawMessage
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read hooks file: %v", err)
	}
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("parse hooks file: %v", err)
	}
	if _, ok := root["customField"]; !ok {
		t.Error("unknown top-level field customField was dropped")
	}

	// Uninstall must take back only our entries.
	if err := updateCursorHooksFile(path, "/opt/agent-notifications", true); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	hooks = readCursorHooks(t, path)
	if got := hooks["stop"]; len(got) != 1 || got[0] != "./my-own-stop.sh" {
		t.Errorf("stop after uninstall = %v, want only the pre-existing entry", got)
	}
	if got := hooks["afterFileEdit"]; len(got) != 1 {
		t.Errorf("afterFileEdit after uninstall = %v, want it untouched", got)
	}
	for _, event := range []string{"beforeShellExecution", "afterShellExecution", "beforeMCPExecution", "afterMCPExecution"} {
		if _, ok := hooks[event]; ok {
			t.Errorf("event %q survived uninstall", event)
		}
	}
}
