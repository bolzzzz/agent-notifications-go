package hooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/777genius/agent-notifications-go/internal/analyzer"
	"github.com/777genius/agent-notifications-go/internal/config"
)

// createTempCursorTranscript writes raw Cursor-format JSONL lines to a temp
// file and returns its path.
func createTempCursorTranscript(t *testing.T, lines []string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cursor-transcript.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("failed to write cursor transcript: %v", err)
	}
	return path
}

func TestNormalizeCursorHookData(t *testing.T) {
	t.Run("maps conversation_id and workspace_roots", func(t *testing.T) {
		h := &HookData{
			ConversationID: "conv-123",
			WorkspaceRoots: []string{"/repo/one", "/repo/two"},
		}
		normalizeCursorHookData(h)
		if h.SessionID != "conv-123" {
			t.Errorf("SessionID = %q, want conv-123", h.SessionID)
		}
		if h.CWD != "/repo/one" {
			t.Errorf("CWD = %q, want /repo/one", h.CWD)
		}
	})

	t.Run("does not clobber existing session and cwd", func(t *testing.T) {
		h := &HookData{
			SessionID:      "explicit",
			CWD:            "/explicit",
			ConversationID: "conv-123",
			WorkspaceRoots: []string{"/repo/one"},
		}
		normalizeCursorHookData(h)
		if h.SessionID != "explicit" || h.CWD != "/explicit" {
			t.Errorf("normalize clobbered explicit values: %+v", h)
		}
	})

	t.Run("falls back to CURSOR env vars", func(t *testing.T) {
		t.Setenv("CURSOR_PROJECT_DIR", "/env/project")
		t.Setenv("CURSOR_TRANSCRIPT_PATH", "/env/transcript.jsonl")
		h := &HookData{ConversationID: "conv-9"}
		normalizeCursorHookData(h)
		if h.CWD != "/env/project" {
			t.Errorf("CWD = %q, want /env/project", h.CWD)
		}
		if h.TranscriptPath != "/env/transcript.jsonl" {
			t.Errorf("TranscriptPath = %q, want /env/transcript.jsonl", h.TranscriptPath)
		}
	})
}

func TestCursorStopStatus(t *testing.T) {
	cfg := config.DefaultConfig()
	handler := &Handler{cfg: cfg}

	tests := []struct {
		name   string
		status string
		want   analyzer.Status
	}{
		{name: "aborted is skipped", status: "aborted", want: analyzer.StatusUnknown},
		{name: "error maps to api_error", status: "error", want: analyzer.StatusAPIError},
		{name: "completed without transcript is task_complete", status: "completed", want: analyzer.StatusTaskComplete},
		{name: "empty status without transcript is task_complete", status: "", want: analyzer.StatusTaskComplete},
		{name: "case-insensitive Aborted", status: "Aborted", want: analyzer.StatusUnknown},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			status, _, err := handler.cursorStopStatus(&HookData{Status: tc.status})
			if err != nil {
				t.Fatalf("cursorStopStatus error: %v", err)
			}
			if status != tc.want {
				t.Errorf("cursorStopStatus(%q) = %v, want %v", tc.status, status, tc.want)
			}
		})
	}
}

func TestCursorStopStatus_WithTranscript(t *testing.T) {
	cfg := config.DefaultConfig()
	handler := &Handler{cfg: cfg}

	transcript := createTempCursorTranscript(t, []string{
		`{"role":"user","message":{"content":"edit the file"}}`,
		`{"role":"assistant","message":{"content":[{"type":"tool_use","name":"Edit"},{"type":"text","text":"done"}]}}`,
	})

	status, messages, err := handler.cursorStopStatus(&HookData{
		Status:         "completed",
		TranscriptPath: transcript,
	})
	if err != nil {
		t.Fatalf("cursorStopStatus error: %v", err)
	}
	if status != analyzer.StatusTaskComplete {
		t.Errorf("status = %v, want StatusTaskComplete", status)
	}
	if len(messages) == 0 {
		t.Error("expected parsed messages from transcript")
	}
}

func TestHandler_CursorStop_TaskComplete(t *testing.T) {
	cfg := config.DefaultConfig()
	handler, mockNotif, _ := newTestHandler(t, cfg)
	// Cursor payloads carry a model field; the explicit product marker keeps
	// detection from routing to Codex.
	handler.defaultProduct = ""

	hookData := buildHookDataJSON(HookData{
		Product:        "cursor",
		ConversationID: "cursor-session-1",
		Status:         "completed",
		Model:          "claude-4-sonnet",
		WorkspaceRoots: []string{t.TempDir()},
	})

	if err := handler.HandleHook("Stop", hookData); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !mockNotif.wasCalled() {
		t.Fatal("expected a notification for a completed Cursor turn")
	}
	if call := mockNotif.lastCall(); call.status != analyzer.StatusTaskComplete {
		t.Errorf("status = %v, want StatusTaskComplete", call.status)
	}
}

func TestHandler_CursorStop_AbortedIsSilent(t *testing.T) {
	cfg := config.DefaultConfig()
	handler, mockNotif, _ := newTestHandler(t, cfg)
	handler.defaultProduct = ""

	hookData := buildHookDataJSON(HookData{
		Product:        "cursor",
		ConversationID: "cursor-session-2",
		Status:         "aborted",
		Model:          "claude-4-sonnet",
		WorkspaceRoots: []string{t.TempDir()},
	})

	if err := handler.HandleHook("Stop", hookData); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mockNotif.wasCalled() {
		t.Error("aborted Cursor turn should not notify")
	}
}

// cursor-approval-watch reports an unresolved shell/MCP gate by feeding a
// synthetic Notification payload back through the pipeline, carrying the body
// in the message field. That text names the command Cursor is waiting on, so it
// has to survive into the notification rather than degrading to a generic
// question line.
func TestHandler_CursorApprovalWaitNotification_KeepsCommandText(t *testing.T) {
	cfg := config.DefaultConfig()
	handler, mockNotif, _ := newTestHandler(t, cfg)
	handler.defaultProduct = ""

	hookData := buildHookDataJSON(HookData{
		Product:   "cursor",
		SessionID: "cursor-session-approval",
		CWD:       t.TempDir(),
		Message:   "Waiting for approval: git push --force origin main",
	})

	if err := handler.HandleHook("Notification", hookData); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	call := mockNotif.lastCall()
	if call == nil {
		t.Fatal("expected a notification for an unresolved Cursor approval gate")
	}
	if call.status != analyzer.StatusQuestion {
		t.Errorf("status = %v, want StatusQuestion", call.status)
	}
	if !strings.Contains(call.message, "git push --force origin main") {
		t.Errorf("message = %q, want it to name the command awaiting approval", call.message)
	}
}
