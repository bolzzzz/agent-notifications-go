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

func TestName(t *testing.T) {
	if got := Name(Codex); got != "Codex" {
		t.Errorf("Name(Codex) = %q, want %q", got, "Codex")
	}
	if got := Name(Claude); got != "Claude Code" {
		t.Errorf("Name(Claude) = %q, want %q", got, "Claude Code")
	}
}
