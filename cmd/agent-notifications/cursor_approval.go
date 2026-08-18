package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/777genius/agent-notifications-go/internal/approval"
	"github.com/777genius/agent-notifications-go/internal/config"
	"github.com/777genius/agent-notifications-go/internal/hooks"
	"github.com/777genius/agent-notifications-go/internal/logging"
	"github.com/777genius/agent-notifications-go/internal/platform"
	"github.com/777genius/agent-notifications-go/internal/product"
)

// cursorApprovalWatchCommand is the internal subcommand the before* gate spawns
// to decide, after the grace period, whether the call really needs the user.
const cursorApprovalWatchCommand = "cursor-approval-watch"

// cursorApprovalCleanupAgeSeconds discards approval state from gates nobody
// resolved, e.g. when Cursor exits mid-turn.
const cursorApprovalCleanupAgeSeconds = 3600

// cursorApprovalGateKind maps a Cursor permission-gate event onto the kind of
// call it guards.
func cursorApprovalGateKind(hookEvent string) (approval.Kind, bool) {
	switch hookEvent {
	case "beforeShellExecution", "BeforeShellExecution":
		return approval.KindShell, true
	case "beforeMCPExecution", "BeforeMCPExecution":
		return approval.KindMCP, true
	default:
		return "", false
	}
}

// isCursorExecutionDoneEvent reports whether the event marks a shell/MCP call
// finishing, which cancels the watcher its before* sibling started.
func isCursorExecutionDoneEvent(hookEvent string) bool {
	switch hookEvent {
	case "afterShellExecution", "AfterShellExecution",
		"afterMCPExecution", "AfterMCPExecution":
		return true
	default:
		return false
	}
}

// runCursorApprovalGate records a pending shell/MCP call and hands it to a
// detached watcher. It never notifies inline: Cursor fires this gate before
// deciding whether the call needs approval, so an auto-approved call is
// indistinguishable from one that stops for the user.
func runCursorApprovalGate(pluginRoot string, kind approval.Kind, input io.Reader) {
	cfg, err := config.LoadFromPluginRoot(pluginRoot, product.Cursor)
	if err != nil {
		logging.Warn("Cursor approval gate: failed to load config: %v", err)
		return
	}
	if !cfg.ShouldNotifyOnCursorApprovalWait() {
		logging.Debug("Cursor approval gate: disabled (config: cursor.notifyOnApprovalWait)")
		return
	}
	if !cfg.IsAnyNotificationEnabled() {
		logging.Debug("Cursor approval gate: all notifications disabled, skipping")
		return
	}

	payload, err := approval.ParseGatePayload(input)
	if err != nil {
		logging.Warn("Cursor approval gate: %v", err)
		return
	}

	rec := payload.Record(kind)
	if rec.Key == "" || rec.Target() == "" {
		logging.Debug("Cursor approval gate: payload carries no command or tool, skipping")
		return
	}

	store := approval.NewStore()
	if err := store.Cleanup(cursorApprovalCleanupAgeSeconds); err != nil {
		logging.Warn("Cursor approval gate: cleanup failed: %v", err)
	}
	if err := store.WritePending(rec); err != nil {
		logging.Warn("Cursor approval gate: failed to record pending call: %v", err)
		return
	}

	if err := spawnCursorApprovalWatcher(pluginRoot, rec.Key); err != nil {
		logging.Warn("Cursor approval gate: failed to spawn watcher: %v", err)
		_ = store.RemovePending(rec.Key)
		return
	}
	logging.Debug("Cursor approval gate: watching %s call (key=%s, waitSeconds=%d)",
		kind, rec.Key, cfg.GetCursorApprovalWaitSeconds())
}

// runCursorExecutionDone cancels the watcher for a call that Cursor allowed to
// run, and stamps the session so sibling watchers can see the agent progressing.
func runCursorExecutionDone(pluginRoot string, input io.Reader) {
	cfg, err := config.LoadFromPluginRoot(pluginRoot, product.Cursor)
	if err != nil {
		logging.Warn("Cursor execution done: failed to load config: %v", err)
		return
	}
	if !cfg.ShouldNotifyOnCursorApprovalWait() {
		return
	}

	payload, err := approval.ParseGatePayload(input)
	if err != nil {
		logging.Warn("Cursor execution done: %v", err)
		return
	}

	store := approval.NewStore()
	key := payload.Key()
	if key == "" || !store.HasPending(key) {
		// Nothing is waiting on this call: either the watcher already resolved it
		// or the gate never ran. Avoid leaving an orphan marker behind.
		return
	}
	if err := store.TouchActivity(payload.Session()); err != nil {
		logging.Warn("Cursor execution done: failed to stamp session activity: %v", err)
	}
	if err := store.MarkDone(key); err != nil {
		logging.Warn("Cursor execution done: failed to mark call complete: %v", err)
		return
	}
	logging.Debug("Cursor execution done: call completed, cancelling watcher (key=%s)", key)
}

// spawnCursorApprovalWatcher starts the detached watcher. Its stdio is detached
// from this process so nothing can pollute the permission decision this hook
// still has to print on stdout.
func spawnCursorApprovalWatcher(pluginRoot, key string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}

	cmd := exec.Command(exe, cursorApprovalWatchCommand, "--key", key)
	cmd.Env = append(os.Environ(), "AGENT_NOTIFICATIONS_PLUGIN_ROOT="+pluginRoot)
	platform.SetDetachedProcAttr(cmd)

	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err == nil {
		cmd.Stdin = devNull
		cmd.Stdout = devNull
		cmd.Stderr = devNull
		defer func() { _ = devNull.Close() }()
	}

	if err := cmd.Start(); err != nil {
		return err
	}
	if cmd.Process != nil {
		_ = cmd.Process.Release()
	}
	return nil
}

// runCursorApprovalWatch waits out the grace period and notifies only if the
// recorded call never got past Cursor's approval prompt.
func runCursorApprovalWatch(args []string) {
	key := parseCursorApprovalWatchArgs(args)
	if key == "" {
		fmt.Fprintln(os.Stderr, "cursor-approval-watch: --key is required")
		os.Exit(1)
	}

	pluginRoot := getPluginRoot()
	if _, err := logging.InitLogger(pluginRoot); err == nil {
		defer func() { _ = logging.Close() }()
	}
	logging.SetPrefix(fmt.Sprintf("PID:%d", os.Getpid()))

	cfg, err := config.LoadFromPluginRoot(pluginRoot, product.Cursor)
	if err != nil {
		logging.Warn("cursor-approval-watch: failed to load config: %v", err)
		return
	}

	time.Sleep(time.Duration(cfg.GetCursorApprovalWaitSeconds()) * time.Second)

	watcher := &approval.Watcher{Store: approval.NewStore()}
	rec, outcome := watcher.Resolve(key)
	if !outcome.ShouldNotify() {
		logging.Debug("cursor-approval-watch: no approval wait (key=%s, outcome=%s)", key, outcome)
		return
	}

	logging.Debug("cursor-approval-watch: still waiting for approval (key=%s, kind=%s)", key, rec.Kind)
	if err := sendCursorApprovalWaitNotification(pluginRoot, rec); err != nil {
		logging.Warn("cursor-approval-watch: failed to notify: %v", err)
	}
}

func parseCursorApprovalWatchArgs(args []string) string {
	for i := 0; i < len(args); i++ {
		if args[i] == "--key" && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// cursorApprovalWaitPayload is the synthetic Cursor Notification payload the
// watcher feeds back into the normal hook pipeline, so the approval wait goes
// through the same dedup, cooldown, and click-to-focus handling as every other
// notification.
type cursorApprovalWaitPayload struct {
	Product        string   `json:"product"`
	SessionID      string   `json:"session_id"`
	CWD            string   `json:"cwd,omitempty"`
	WorkspaceRoots []string `json:"workspace_roots,omitempty"`
	TranscriptPath string   `json:"transcript_path,omitempty"`
	Message        string   `json:"message"`
}

func sendCursorApprovalWaitNotification(pluginRoot string, rec approval.Record) error {
	handler, err := hooks.NewHandler(pluginRoot, product.Cursor)
	if err != nil {
		return fmt.Errorf("create handler: %w", err)
	}

	data, err := json.Marshal(cursorApprovalWaitPayload{
		Product:        product.Cursor,
		SessionID:      rec.SessionID,
		CWD:            rec.CWD,
		WorkspaceRoots: rec.WorkspaceRoots,
		TranscriptPath: rec.TranscriptPath,
		Message:        rec.Message(),
	})
	if err != nil {
		return fmt.Errorf("encode notification payload: %w", err)
	}

	return handler.HandleHook("Notification", bytes.NewReader(data))
}
