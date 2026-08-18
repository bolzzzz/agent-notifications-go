package hooks

import (
	"os"
	"strings"

	"github.com/777genius/agent-notifications-go/internal/analyzer"
	"github.com/777genius/agent-notifications-go/internal/logging"
	"github.com/777genius/agent-notifications-go/internal/platform"
	"github.com/777genius/agent-notifications-go/pkg/jsonl"
)

// normalizeCursorHookData maps the Cursor CLI hook envelope onto the shared
// HookData fields the rest of the pipeline expects.
//
//   - conversation_id → SessionID (Cursor has no session_id on most events)
//   - workspace_roots[0] → CWD (used for git branch / folder name / focus)
//   - CURSOR_TRANSCRIPT_PATH env → TranscriptPath when the payload omits it
//     (the CLI exposes the transcript through the environment, not the payload)
func normalizeCursorHookData(h *HookData) {
	if h.SessionID == "" {
		h.SessionID = h.ConversationID
	}
	if h.CWD == "" && len(h.WorkspaceRoots) > 0 {
		h.CWD = h.WorkspaceRoots[0]
	}
	if h.CWD == "" {
		h.CWD = os.Getenv("CURSOR_PROJECT_DIR")
	}
	if h.TranscriptPath == "" {
		h.TranscriptPath = os.Getenv("CURSOR_TRANSCRIPT_PATH")
	}
}

// cursorStopStatus classifies a Cursor CLI stop / subagentStop event.
//
// The payload always carries a status field:
//   - "aborted": the user interrupted the turn → no notification
//   - "error":   the agent loop errored → API-error notification
//   - "completed" (or anything else): the turn finished
//
// When a transcript is available it is run through the shared analyzer (via the
// Cursor JSONL parser) so the state machine can classify the turn (task
// complete vs. review) and produce a rich action summary from the tool calls.
// When no transcript exists (transcripts disabled → transcript_path null), the
// status alone drives the result and the turn is reported as task_complete.
func (h *Handler) cursorStopStatus(hookData *HookData) (analyzer.Status, []jsonl.Message, error) {
	switch strings.ToLower(strings.TrimSpace(hookData.Status)) {
	case "aborted":
		logging.Debug("Cursor stop aborted, skipping notification")
		return analyzer.StatusUnknown, nil, nil
	case "error":
		logging.Debug("Cursor stop error → api_error")
		return analyzer.StatusAPIError, nil, nil
	}

	if hookData.TranscriptPath != "" && platform.FileExists(hookData.TranscriptPath) {
		status, messages, err := analyzer.AnalyzeTranscriptWithParser(hookData.TranscriptPath, h.cfg, jsonl.ParseCursorFile)
		if err != nil {
			logging.Warn("Cursor transcript parse failed: %v", err)
		} else {
			// The analyzer returns StatusUnknown for turns it cannot classify
			// (e.g. no assistant text after the last user turn). A completed
			// Cursor turn should still notify, so fall back to task_complete.
			if status == analyzer.StatusUnknown {
				status = analyzer.StatusTaskComplete
			}
			logging.Debug("Analyzed status (cursor transcript): %s", status)
			return status, messages, nil
		}
	}

	logging.Debug("Cursor stop completed (no transcript) → task_complete")
	return analyzer.StatusTaskComplete, nil, nil
}
