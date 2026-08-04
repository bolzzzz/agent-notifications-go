package hooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/777genius/agent-notifications-go/internal/analyzer"
	"github.com/777genius/agent-notifications-go/internal/config"
)

func TestHandler_CodeBuddyStop_PayloadQuestion(t *testing.T) {
	cfg := &config.Config{
		Notifications: config.NotificationsConfig{
			Desktop: config.DesktopConfig{Enabled: true},
		},
		Statuses: map[string]config.StatusInfo{
			"question": {Title: "Question"},
		},
	}
	handler, mockNotif, _ := newTestHandler(t, cfg)

	hookData := buildHookDataJSON(HookData{
		SessionID:            "codebuddy-session-1",
		CWD:                  "/test",
		Product:              "codebuddy",
		LastAssistantMessage: "Should I delete the legacy code path?",
	})
	if err := handler.HandleHook("Stop", hookData); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	call := mockNotif.lastCall()
	if call == nil {
		t.Fatal("expected notification to be sent")
	}
	if call.status != analyzer.StatusQuestion {
		t.Errorf("got status %v, want StatusQuestion", call.status)
	}
}

func TestHandler_CodeBuddyStop_PayloadTaskComplete(t *testing.T) {
	cfg := &config.Config{
		Notifications: config.NotificationsConfig{
			Desktop: config.DesktopConfig{Enabled: true},
		},
		Statuses: map[string]config.StatusInfo{
			"task_complete": {Title: "Task Complete"},
		},
	}
	handler, mockNotif, _ := newTestHandler(t, cfg)

	hookData := buildHookDataJSON(HookData{
		SessionID:            "codebuddy-session-2",
		CWD:                  "/test",
		Product:              "codebuddy",
		LastAssistantMessage: "Renamed foo to bar and updated the tests.",
	})
	if err := handler.HandleHook("Stop", hookData); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	call := mockNotif.lastCall()
	if call == nil {
		t.Fatal("expected notification to be sent")
	}
	if call.status != analyzer.StatusTaskComplete {
		t.Errorf("got status %v, want StatusTaskComplete", call.status)
	}
}

func TestHandler_CodeBuddyStop_TranscriptRoutedToNormalizer(t *testing.T) {
	cfg := &config.Config{
		Notifications: config.NotificationsConfig{
			Desktop: config.DesktopConfig{Enabled: true},
		},
		Statuses: map[string]config.StatusInfo{
			"task_complete": {Title: "Task Complete"},
		},
	}
	handler, mockNotif, _ := newTestHandler(t, cfg)

	// A CodeBuddy-format transcript whose last turn ends with an Edit tool. The
	// Claude parser would yield nothing from this, so routing must use the
	// CodeBuddy normalizer to produce a task_complete notification.
	dir := t.TempDir()
	transcript := filepath.Join(dir, "transcript.jsonl")
	lines := []string{
		`{"id":"u1","type":"message","role":"user","timestamp":1785757068000,"content":[{"type":"input_text","text":"fix the bug"}]}`,
		`{"id":"a1","type":"message","role":"assistant","timestamp":1785757070000,"content":[{"type":"output_text","text":"ok, editing"}]}`,
		`{"id":"a1","type":"function_call","timestamp":1785757070100,"name":"Edit","arguments":"{\"file_path\":\"/tmp/a\"}"}`,
	}
	if err := os.WriteFile(transcript, []byte(strings.Join(lines, "\n")), 0o600); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	hookData := buildHookDataJSON(HookData{
		SessionID:      "codebuddy-session-3",
		CWD:            "/test",
		Product:        "codebuddy",
		TranscriptPath: transcript,
	})
	if err := handler.HandleHook("Stop", hookData); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	call := mockNotif.lastCall()
	if call == nil {
		t.Fatal("expected notification to be sent from CodeBuddy transcript")
	}
	if call.status != analyzer.StatusTaskComplete {
		t.Errorf("got status %v, want StatusTaskComplete", call.status)
	}
}

func TestHandler_CodeBuddyPreToolUse_EnterPlanMode(t *testing.T) {
	cfg := &config.Config{
		Notifications: config.NotificationsConfig{
			Desktop: config.DesktopConfig{Enabled: true},
		},
	}
	handler, mockNotif, _ := newTestHandler(t, cfg)

	hookData := buildHookDataJSON(HookData{
		SessionID: "codebuddy-session-4",
		CWD:       "/test",
		Product:   "codebuddy",
		ToolName:  "EnterPlanMode",
	})
	if err := handler.HandleHook("PreToolUse", hookData); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mockNotif.wasCalled() {
		t.Error("EnterPlanMode must not produce a notification (plan not ready yet)")
	}
}

func TestHandler_CodeBuddyPreToolUse_AskUserQuestion(t *testing.T) {
	cfg := &config.Config{
		Notifications: config.NotificationsConfig{
			Desktop: config.DesktopConfig{Enabled: true},
		},
		Statuses: map[string]config.StatusInfo{
			"question": {Title: "Question"},
		},
	}
	handler, mockNotif, _ := newTestHandler(t, cfg)

	hookData := buildHookDataJSON(HookData{
		SessionID: "codebuddy-session-5",
		CWD:       "/test",
		Product:   "codebuddy",
		ToolName:  "AskUserQuestion",
	})
	if err := handler.HandleHook("PreToolUse", hookData); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	call := mockNotif.lastCall()
	if call == nil {
		t.Fatal("expected notification to be sent")
	}
	if call.status != analyzer.StatusQuestion {
		t.Errorf("got status %v, want StatusQuestion", call.status)
	}
}

func TestHandler_ClaudeStop_ProductOverrideNotAppliedWithoutFlag(t *testing.T) {
	cfg := &config.Config{
		Notifications: config.NotificationsConfig{
			Desktop: config.DesktopConfig{Enabled: true},
		},
		Statuses: map[string]config.StatusInfo{
			"task_complete": {Title: "Task Complete"},
		},
	}
	handler, mockNotif, _ := newTestHandler(t, cfg)

	// Explicit product=claude with no transcript must follow the Claude path and
	// skip (no notification), even if CODEBUDDY_* env leaked into the process.
	hookData := buildHookDataJSON(HookData{
		SessionID: "claude-session-flag",
		CWD:       "/test",
		Product:   "claude",
	})
	if err := handler.HandleHook("Stop", hookData); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mockNotif.wasCalled() {
		t.Error("expected no notification for Claude Stop without transcript")
	}
}

func TestHandler_CodeBuddy_ProductOverrideBackfilledFromFlag(t *testing.T) {
	// Unit-level check that SetProductOverride feeds the product field used by
	// FromPayload. We can't see the internal field, so we assert behavior:
	// a CodeBuddy-style Stop with only payload data and no Product set, but with
	// the override, must classify as task_complete (not be skipped).
	cfg := &config.Config{
		Notifications: config.NotificationsConfig{
			Desktop: config.DesktopConfig{Enabled: true},
		},
		Statuses: map[string]config.StatusInfo{
			"task_complete": {Title: "Task Complete"},
		},
	}
	handler, mockNotif, _ := newTestHandler(t, cfg)
	handler.SetProductOverride("codebuddy")

	hookData := buildHookDataJSON(HookData{
		SessionID:            "codebuddy-override",
		CWD:                  "/test",
		LastAssistantMessage: "Done.",
	})
	if err := handler.HandleHook("Stop", hookData); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	call := mockNotif.lastCall()
	if call == nil {
		t.Fatal("expected notification after product override backfill")
	}
	if call.status != analyzer.StatusTaskComplete {
		t.Errorf("got status %v, want StatusTaskComplete", call.status)
	}
}
