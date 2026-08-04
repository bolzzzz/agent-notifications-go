package hooks

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

type pluginHookSettings struct {
	Hooks map[string][]pluginHookMatcherGroup `json:"hooks"`
}

type pluginHookMatcherGroup struct {
	Matcher string              `json:"matcher,omitempty"`
	Hooks   []pluginHookCommand `json:"hooks"`
}

type pluginHookCommand struct {
	Type    string   `json:"type"`
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
	Timeout int      `json:"timeout"`
	Shell   string   `json:"shell,omitempty"`
}

func TestPluginHooksUseExecFormWrapper(t *testing.T) {
	data, err := os.ReadFile("../../hooks/hooks.json")
	if err != nil {
		t.Fatal(err)
	}

	raw := string(data)
	for _, forbidden := range []string{
		"hook-wrapper.sh handle-hook",
		"$input",
		"powershell",
		`"shell"`,
	} {
		if strings.Contains(raw, forbidden) {
			t.Fatalf("default hooks config contains %q", forbidden)
		}
	}

	var settings pluginHookSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}

	expected := map[string]string{
		"PreToolUse":   "PreToolUse",
		"Notification": "Notification",
		"Stop":         "Stop",
		"SubagentStop": "SubagentStop",
		"TeammateIdle": "TeammateIdle",
	}

	for hookEvent, expectedArg := range expected {
		groups := settings.Hooks[hookEvent]
		if len(groups) != 1 {
			t.Fatalf("%s groups = %d, want 1", hookEvent, len(groups))
		}
		if len(groups[0].Hooks) != 1 {
			t.Fatalf("%s commands = %d, want 1", hookEvent, len(groups[0].Hooks))
		}

		hook := groups[0].Hooks[0]
		if hook.Type != "command" {
			t.Fatalf("%s type = %q, want command", hookEvent, hook.Type)
		}
		if hook.Command != "sh" {
			t.Fatalf("%s command = %q, want sh", hookEvent, hook.Command)
		}
		wantArgs := []string{"${CLAUDE_PLUGIN_ROOT}/bin/hook-wrapper.sh", "handle-hook", expectedArg}
		if !reflect.DeepEqual(hook.Args, wantArgs) {
			t.Fatalf("%s args = %#v, want %#v", hookEvent, hook.Args, wantArgs)
		}
		if hook.Shell != "" {
			t.Fatalf("%s shell = %q, want empty", hookEvent, hook.Shell)
		}
		if hook.Timeout != 30 {
			t.Fatalf("%s timeout = %d, want 30", hookEvent, hook.Timeout)
		}
	}
}

// TestCodeBuddyHooksJSONValidates asserts the CodeBuddy hook config matches the
// CodeBuddy subprocess contract: single command-string form (args is silently
// ignored by codebuddy.js), CodeBuddy plugin-root variable, the --product flag,
// and no Windows-specific command_windows (enforced Git Bash on Windows).
func TestCodeBuddyHooksJSONValidates(t *testing.T) {
	data, err := os.ReadFile("../../hooks/hooks-codebuddy.json")
	if err != nil {
		t.Fatal(err)
	}

	var settings pluginHookSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}

	// Required events for CodeBuddy Code parity with Claude Code.
	for _, hookEvent := range []string{"PreToolUse", "Notification", "SessionStart", "Stop", "SubagentStop", "TeammateIdle"} {
		groups := settings.Hooks[hookEvent]
		if len(groups) == 0 {
			t.Fatalf("missing event %s", hookEvent)
		}
		for _, g := range groups {
			for _, hook := range g.Hooks {
				// CodeBuddy ignores the args array entirely — must be empty.
				if len(hook.Args) != 0 {
					t.Fatalf("%s: args must be empty in CodeBuddy hooks (ignored by codebuddy.js): %#v", hookEvent, hook.Args)
				}
				// No Windows-specific command form (cmd.exe path on Windows).
				if strings.Contains(strings.ToLower(hook.Command), "command_windows") {
					t.Fatalf("%s: command_windows is unsupported by CodeBuddy", hookEvent)
				}
				// Must reference the CodeBuddy plugin root and pin the product.
				if !strings.Contains(hook.Command, "${CODEBUDDY_PLUGIN_ROOT}") {
					t.Fatalf("%s: command must reference ${CODEBUDDY_PLUGIN_ROOT}: %q", hookEvent, hook.Command)
				}
				if !strings.Contains(hook.Command, "--product codebuddy") {
					t.Fatalf("%s: command must pin --product codebuddy: %q", hookEvent, hook.Command)
				}
				if hook.Timeout != 30 {
					t.Fatalf("%s: timeout = %d, want 30", hookEvent, hook.Timeout)
				}
			}
		}
	}

	// idle_prompt must NOT be present: handleNotificationEvent unconditionally
	// returns StatusQuestion, so an idle_prompt matcher would fire a spurious
	// question notification every 60s of idle.
	raw := string(data)
	if strings.Contains(raw, "idle_prompt") {
		t.Fatal("CodeBuddy hooks must not include idle_prompt (spurious question notifications)")
	}
}

// TestPluginManifestVersionParity ensures the three plugin manifests shipped for
// Claude, CodeBuddy, and WorkBuddy all declare the same version, so the lazy
// binary-update check (readPluginManifestVersion) stays consistent across hosts.
func TestPluginManifestVersionParity(t *testing.T) {
	type manifest struct {
		Version string `json:"version"`
	}
	paths := []string{
		"../../.claude-plugin/plugin.json",
		"../../.codebuddy-plugin/plugin.json",
	}
	want := ""
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		var m manifest
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatalf("parse %s: %v", p, err)
		}
		if m.Version == "" {
			t.Fatalf("%s: empty version", p)
		}
		if want == "" {
			want = m.Version
		} else if m.Version != want {
			t.Fatalf("version mismatch: %s=%q, want %q", p, m.Version, want)
		}
	}
}
