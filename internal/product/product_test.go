package product

import "testing"

func TestDetect(t *testing.T) {
	t.Run("turn_id means codex", func(t *testing.T) {
		if got := Detect("turn-1", ""); got != Codex {
			t.Errorf("Detect(turnID) = %v, want %v", got, Codex)
		}
	})

	t.Run("model means codex", func(t *testing.T) {
		if got := Detect("", "gpt-5"); got != Codex {
			t.Errorf("Detect(model) = %v, want %v", got, Codex)
		}
	})

	t.Run("PLUGIN_ROOT env means codex", func(t *testing.T) {
		t.Setenv("PLUGIN_ROOT", "/plugins/x")
		if got := Detect("", ""); got != Codex {
			t.Errorf("Detect(PLUGIN_ROOT) = %v, want %v", got, Codex)
		}
	})

	t.Run("no signals means claude", func(t *testing.T) {
		t.Setenv("PLUGIN_ROOT", "")
		if got := Detect("", ""); got != Claude {
			t.Errorf("Detect() = %v, want %v", got, Claude)
		}
	})
}

func TestFromPayload(t *testing.T) {
	t.Run("explicit opencode product", func(t *testing.T) {
		if got := FromPayload(OpenCode, "", ""); got != OpenCode {
			t.Errorf("FromPayload(opencode) = %v, want %v", got, OpenCode)
		}
	})

	t.Run("explicit codex product", func(t *testing.T) {
		if got := FromPayload(Codex, "", ""); got != Codex {
			t.Errorf("FromPayload(codex) = %v, want %v", got, Codex)
		}
	})

	t.Run("no explicit product falls back to heuristic", func(t *testing.T) {
		if got := FromPayload("", "turn-1", ""); got != Codex {
			t.Errorf("FromPayload(turn_id) = %v, want %v", got, Codex)
		}
		if got := FromPayload("", "", ""); got != Claude {
			t.Errorf("FromPayload() = %v, want %v", got, Claude)
		}
	})

	t.Run("unknown explicit product falls back to heuristic", func(t *testing.T) {
		if got := FromPayload("some-other-tool", "turn-1", ""); got != Codex {
			t.Errorf("FromPayload(unknown, turn_id) = %v, want %v", got, Codex)
		}
	})
}
