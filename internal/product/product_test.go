package product

import "testing"

// clearProductEnv removes every environment variable that participates in
// product detection. Detection reads ambient process state, so tests must start
// from a known-empty environment — otherwise running the suite *inside* one of
// the supported CLIs (e.g. `go test` launched from CodeBuddy, which exports
// CODEBUDDY_*) would silently change what is being asserted.
func clearProductEnv(t *testing.T) {
	t.Helper()
	t.Setenv("PLUGIN_ROOT", "")
	for _, name := range codeBuddyEnvVars {
		t.Setenv(name, "")
	}
}

func TestDetect(t *testing.T) {
	t.Run("turn_id means codex", func(t *testing.T) {
		clearProductEnv(t)
		if got := Detect("turn-1", ""); got != Codex {
			t.Errorf("Detect(turnID) = %v, want %v", got, Codex)
		}
	})

	t.Run("model means codex", func(t *testing.T) {
		clearProductEnv(t)
		if got := Detect("", "gpt-5"); got != Codex {
			t.Errorf("Detect(model) = %v, want %v", got, Codex)
		}
	})

	t.Run("PLUGIN_ROOT env means codex", func(t *testing.T) {
		clearProductEnv(t)
		t.Setenv("PLUGIN_ROOT", "/plugins/x")
		if got := Detect("", ""); got != Codex {
			t.Errorf("Detect(PLUGIN_ROOT) = %v, want %v", got, Codex)
		}
	})

	t.Run("no signals means claude", func(t *testing.T) {
		clearProductEnv(t)
		if got := Detect("", ""); got != Claude {
			t.Errorf("Detect() = %v, want %v", got, Claude)
		}
	})

	for _, name := range codeBuddyEnvVars {
		t.Run(name+" means codebuddy", func(t *testing.T) {
			clearProductEnv(t)
			t.Setenv(name, "/some/path")
			if got := Detect("", ""); got != CodeBuddy {
				t.Errorf("Detect(%s) = %v, want %v", name, got, CodeBuddy)
			}
		})
	}

	// CodeBuddy exports CLAUDE_* names as compatibility aliases, so a stale or
	// inherited PLUGIN_ROOT must not win over the CodeBuddy signal.
	t.Run("codebuddy wins over inherited PLUGIN_ROOT", func(t *testing.T) {
		clearProductEnv(t)
		t.Setenv("PLUGIN_ROOT", "/plugins/x")
		t.Setenv("CODEBUDDY_PLUGIN_ROOT", "/plugins/codebuddy")
		if got := Detect("", ""); got != CodeBuddy {
			t.Errorf("Detect() = %v, want %v", got, CodeBuddy)
		}
	})

	// A CodeBuddy payload never carries turn_id/model, but if some future
	// version did, the environment must remain authoritative.
	t.Run("codebuddy wins over turn_id", func(t *testing.T) {
		clearProductEnv(t)
		t.Setenv("CODEBUDDY_SESSION_ID", "sess-1")
		if got := Detect("turn-1", "some-model"); got != CodeBuddy {
			t.Errorf("Detect(turnID, model) = %v, want %v", got, CodeBuddy)
		}
	})

	t.Run("empty codebuddy vars are ignored", func(t *testing.T) {
		clearProductEnv(t)
		if got := Detect("", ""); got != Claude {
			t.Errorf("Detect() = %v, want %v", got, Claude)
		}
	})
}

func TestFromPayload(t *testing.T) {
	t.Run("explicit opencode product", func(t *testing.T) {
		clearProductEnv(t)
		if got := FromPayload(OpenCode, "", ""); got != OpenCode {
			t.Errorf("FromPayload(opencode) = %v, want %v", got, OpenCode)
		}
	})

	t.Run("explicit codex product", func(t *testing.T) {
		clearProductEnv(t)
		if got := FromPayload(Codex, "", ""); got != Codex {
			t.Errorf("FromPayload(codex) = %v, want %v", got, Codex)
		}
	})

	t.Run("explicit codebuddy product", func(t *testing.T) {
		clearProductEnv(t)
		if got := FromPayload(CodeBuddy, "", ""); got != CodeBuddy {
			t.Errorf("FromPayload(codebuddy) = %v, want %v", got, CodeBuddy)
		}
	})

	// An explicit --product claude must survive even when CodeBuddy environment
	// variables leak into the process (e.g. a Claude Code session started from a
	// CodeBuddy shell), otherwise env detection would silently override it.
	t.Run("explicit claude product overrides codebuddy env", func(t *testing.T) {
		clearProductEnv(t)
		t.Setenv("CODEBUDDY_SESSION_ID", "sess-1")
		if got := FromPayload(Claude, "", ""); got != Claude {
			t.Errorf("FromPayload(claude) = %v, want %v", got, Claude)
		}
	})

	t.Run("no explicit product falls back to heuristic", func(t *testing.T) {
		clearProductEnv(t)
		if got := FromPayload("", "turn-1", ""); got != Codex {
			t.Errorf("FromPayload(turn_id) = %v, want %v", got, Codex)
		}
		if got := FromPayload("", "", ""); got != Claude {
			t.Errorf("FromPayload() = %v, want %v", got, Claude)
		}
	})

	t.Run("empty product falls back to codebuddy env", func(t *testing.T) {
		clearProductEnv(t)
		t.Setenv("CODEBUDDY_PROJECT_DIR", "/repo")
		if got := FromPayload("", "", ""); got != CodeBuddy {
			t.Errorf("FromPayload() = %v, want %v", got, CodeBuddy)
		}
	})

	t.Run("unknown explicit product falls back to heuristic", func(t *testing.T) {
		clearProductEnv(t)
		if got := FromPayload("some-other-tool", "turn-1", ""); got != Codex {
			t.Errorf("FromPayload(unknown, turn_id) = %v, want %v", got, Codex)
		}
	})
}
