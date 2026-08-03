package hooks

import (
	"strings"
	"testing"

	"github.com/777genius/agent-notifications-go/internal/analyzer"
	"github.com/777genius/agent-notifications-go/internal/config"
)

func TestCodexStopStatus(t *testing.T) {
	tests := []struct {
		name    string
		message string
		want    analyzer.Status
	}{
		{"empty message is task complete", "", analyzer.StatusTaskComplete},
		{"plain text is task complete", "All tests pass.", analyzer.StatusTaskComplete},
		{"trailing question mark is question", "Which file should I edit?", analyzer.StatusQuestion},
		{"whitespace-trimmed question", "Proceed with the rename?  \n", analyzer.StatusQuestion},
		{"interior question mark is task complete", "Fixed it. Why? Because reasons.", analyzer.StatusTaskComplete},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := codexStopStatus(tt.message); got != tt.want {
				t.Errorf("codexStopStatus(%q) = %v, want %v", tt.message, got, tt.want)
			}
		})
	}
}

func TestHandler_CodexStop_TaskComplete(t *testing.T) {
	cfg := &config.Config{
		Notifications: config.NotificationsConfig{
			Desktop: config.DesktopConfig{Enabled: true},
		},
		Statuses: map[string]config.StatusInfo{
			"task_complete": {Title: "Task Complete"},
		},
	}

	handler, mockNotif, _ := newTestHandler(t, cfg)

	// Codex Stop payload: no transcript parsing, message comes from the payload.
	hookData := buildHookDataJSON(HookData{
		SessionID:            "codex-session-1",
		CWD:                  "/test",
		TurnID:               "turn-1",
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
	if !strings.Contains(call.message, "Renamed foo to bar") {
		t.Errorf("message %q should contain the last assistant message", call.message)
	}
}

func TestHandler_CodexStop_Question(t *testing.T) {
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
		SessionID:            "codex-session-2",
		CWD:                  "/test",
		TurnID:               "turn-2",
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

func TestHandler_CodexStop_EmptyMessageFallsBack(t *testing.T) {
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
		SessionID: "codex-session-3",
		CWD:       "/test",
		TurnID:    "turn-3",
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
	if !strings.Contains(call.message, "Task Complete") {
		t.Errorf("message %q should fall back to the default status title", call.message)
	}
}

func TestHandler_CodexPreToolUse_RequestUserInput(t *testing.T) {
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
		SessionID: "codex-session-4",
		CWD:       "/test",
		TurnID:    "turn-4",
		ToolName:  "request_user_input",
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

func TestHandler_ClaudeStop_UnaffectedByCodexPath(t *testing.T) {
	cfg := &config.Config{
		Notifications: config.NotificationsConfig{
			Desktop: config.DesktopConfig{Enabled: true},
		},
		Statuses: map[string]config.StatusInfo{
			"task_complete": {Title: "Task Complete"},
		},
	}

	handler, mockNotif, _ := newTestHandler(t, cfg)

	// Claude payloads have no turn_id/model, so an empty transcript path still
	// follows the Claude path and is skipped with a warning.
	hookData := buildHookDataJSON(HookData{
		SessionID: "claude-session-1",
		CWD:       "/test",
	})

	if err := handler.HandleHook("Stop", hookData); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mockNotif.wasCalled() {
		t.Error("expected no notification for Claude Stop without transcript")
	}
}
