package hooks

import (
	"strings"
	"testing"

	"github.com/777genius/agent-notifications-go/internal/analyzer"
	"github.com/777genius/agent-notifications-go/internal/config"
)

func TestOpenCodeStopStatus(t *testing.T) {
	tests := []struct {
		name      string
		errorType string
		message   string
		want      analyzer.Status
	}{
		{"no error, empty message is task complete", "", "", analyzer.StatusTaskComplete},
		{"no error, plain text is task complete", "", "All tests pass.", analyzer.StatusTaskComplete},
		{"no error, trailing question mark is question", "", "Which file should I edit?", analyzer.StatusQuestion},
		{"auth error is api error", "ProviderAuthError", "", analyzer.StatusAPIError},
		{"api error is overloaded", "APIError", "", analyzer.StatusAPIErrorOverloaded},
		{"unknown error is overloaded", "UnknownError", "", analyzer.StatusAPIErrorOverloaded},
		{"case-insensitive auth detection", "providerautherror", "", analyzer.StatusAPIError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := opencodeStopStatus(tt.errorType, tt.message); got != tt.want {
				t.Errorf("opencodeStopStatus(%q, %q) = %v, want %v", tt.errorType, tt.message, got, tt.want)
			}
		})
	}
}

func TestHandler_OpenCodeStop_TaskComplete(t *testing.T) {
	cfg := &config.Config{
		Notifications: config.NotificationsConfig{
			Desktop: config.DesktopConfig{Enabled: true},
		},
		Statuses: map[string]config.StatusInfo{
			"task_complete": {Title: "Task Complete"},
		},
	}

	handler, mockNotif, _ := newTestHandler(t, cfg)

	// opencode Stop payload: no transcript parsing, message comes from the payload.
	hookData := buildHookDataJSON(HookData{
		SessionID:            "opencode-session-1",
		CWD:                  "/test",
		Product:              "opencode",
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

func TestHandler_OpenCodeStop_Question(t *testing.T) {
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
		SessionID:            "opencode-session-2",
		CWD:                  "/test",
		Product:              "opencode",
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

func TestHandler_OpenCodeStop_ApiError(t *testing.T) {
	cfg := &config.Config{
		Notifications: config.NotificationsConfig{
			Desktop: config.DesktopConfig{Enabled: true},
		},
		Statuses: map[string]config.StatusInfo{
			"api_error":            {Title: "API Error"},
			"api_error_overloaded": {Title: "API Error"},
		},
	}

	handler, mockNotif, _ := newTestHandler(t, cfg)

	hookData := buildHookDataJSON(HookData{
		SessionID: "opencode-session-3",
		CWD:       "/test",
		Product:   "opencode",
		ErrorType: "ProviderAuthError",
	})

	if err := handler.HandleHook("Stop", hookData); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	call := mockNotif.lastCall()
	if call == nil {
		t.Fatal("expected notification to be sent")
	}
	if call.status != analyzer.StatusAPIError {
		t.Errorf("got status %v, want StatusAPIError", call.status)
	}
}

func TestHandler_OpenCodeNotification_QuestionWithMessage(t *testing.T) {
	cfg := &config.Config{
		Notifications: config.NotificationsConfig{
			Desktop: config.DesktopConfig{Enabled: true},
		},
		Statuses: map[string]config.StatusInfo{
			"question": {Title: "Question"},
		},
	}

	handler, mockNotif, _ := newTestHandler(t, cfg)

	// opencode question.asked / permission.updated events map to the
	// Notification hook; the plugin passes the question/permission text.
	hookData := buildHookDataJSON(HookData{
		SessionID: "opencode-session-4",
		CWD:       "/test",
		Product:   "opencode",
		Message:   "Which dependency manager should I use?",
	})

	if err := handler.HandleHook("Notification", hookData); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	call := mockNotif.lastCall()
	if call == nil {
		t.Fatal("expected notification to be sent")
	}
	if call.status != analyzer.StatusQuestion {
		t.Errorf("got status %v, want StatusQuestion", call.status)
	}
	if !strings.Contains(call.message, "Which dependency manager") {
		t.Errorf("message %q should contain the question text", call.message)
	}
}

func TestHandler_OpenCodeSubagentStop_OptIn(t *testing.T) {
	suppress := false
	cfg := &config.Config{
		Notifications: config.NotificationsConfig{
			Desktop:              config.DesktopConfig{Enabled: true},
			NotifyOnSubagentStop: true,
			SuppressForSubagents: &suppress,
		},
		Statuses: map[string]config.StatusInfo{
			"task_complete": {Title: "Task Complete"},
		},
	}

	handler, mockNotif, _ := newTestHandler(t, cfg)

	hookData := buildHookDataJSON(HookData{
		SessionID:            "opencode-sub-session-1",
		CWD:                  "/test",
		Product:              "opencode",
		LastAssistantMessage: "Subagent finished its analysis.",
	})

	if err := handler.HandleHook("SubagentStop", hookData); err != nil {
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

func TestHandler_OpenCodeSubagentStop_SuppressedByDefault(t *testing.T) {
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
		SessionID:            "opencode-sub-session-2",
		CWD:                  "/test",
		Product:              "opencode",
		LastAssistantMessage: "Subagent finished its analysis.",
	})

	if err := handler.HandleHook("SubagentStop", hookData); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mockNotif.wasCalled() {
		t.Error("expected no notification for opencode subagent stop by default")
	}
}
