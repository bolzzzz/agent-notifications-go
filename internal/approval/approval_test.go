package approval

import (
	"os"
	"strings"
	"testing"
	"time"
)

// realCursorShellGatePayload is a verbatim beforeShellExecution payload from
// Cursor 3.16.24, including the empty cwd it sends on shell gates.
const realCursorShellGatePayload = `{
  "conversation_id": "f85478ae-ca9e-453a-adf9-a4c5b43b5957",
  "generation_id": "affa286f-e239-461a-af8d-98a4441ce6b5",
  "model": "auto-smart",
  "command": "echo hello && sleep 1",
  "cwd": "",
  "sandbox": false,
  "session_id": "f85478ae-ca9e-453a-adf9-a4c5b43b5957",
  "hook_event_name": "beforeShellExecution",
  "workspace_roots": ["/home/bo/Documents/agent-notifications-go"],
  "transcript_path": "/home/bo/.cursor/agent-transcripts/session.jsonl"
}`

func TestParseGatePayload_RealCursorShellGate(t *testing.T) {
	payload, err := ParseGatePayload(strings.NewReader(realCursorShellGatePayload))
	if err != nil {
		t.Fatalf("ParseGatePayload: %v", err)
	}

	rec := payload.Record(KindShell)
	if rec.SessionID != "f85478ae-ca9e-453a-adf9-a4c5b43b5957" {
		t.Errorf("session = %q", rec.SessionID)
	}
	if rec.Command != "echo hello && sleep 1" {
		t.Errorf("command = %q", rec.Command)
	}
	// Cursor sends an empty cwd on shell gates; the workspace root stands in so
	// click-to-focus and the folder name in the title still work.
	if rec.CWD != "/home/bo/Documents/agent-notifications-go" {
		t.Errorf("cwd = %q, want the workspace root", rec.CWD)
	}
	if rec.Kind != KindShell {
		t.Errorf("kind = %q", rec.Kind)
	}
	if rec.StartedUnixNano == 0 {
		t.Error("startedUnixNano was not stamped")
	}
	if rec.Key == "" {
		t.Error("key was not derived")
	}
}

// The after* event must derive the same key as its before* sibling, otherwise
// completion can never cancel the watcher.
func TestGatePayloadKey_MatchesAcrossBeforeAndAfter(t *testing.T) {
	before, err := ParseGatePayload(strings.NewReader(realCursorShellGatePayload))
	if err != nil {
		t.Fatal(err)
	}
	after, err := ParseGatePayload(strings.NewReader(`{
	  "conversation_id": "f85478ae-ca9e-453a-adf9-a4c5b43b5957",
	  "generation_id": "affa286f-e239-461a-af8d-98a4441ce6b5",
	  "command": "echo hello && sleep 1",
	  "output": "hello\n",
	  "duration": 2243.713,
	  "session_id": "f85478ae-ca9e-453a-adf9-a4c5b43b5957",
	  "hook_event_name": "afterShellExecution"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if before.Key() != after.Key() {
		t.Fatalf("before key %q != after key %q", before.Key(), after.Key())
	}
}

func TestKey_DistinguishesSessionGenerationAndTarget(t *testing.T) {
	base := Key("session", "generation", "git push")
	for name, other := range map[string]string{
		"other session":    Key("session2", "generation", "git push"),
		"other generation": Key("session", "generation2", "git push"),
		"other target":     Key("session", "generation", "git pull"),
	} {
		if other == base {
			t.Errorf("%s produced the same key", name)
		}
	}
	// Whitespace differences in the same command must not split the key.
	if Key("session", "generation", "git   push") != base {
		t.Error("whitespace changed the key")
	}
}

func TestRecordMessage(t *testing.T) {
	rec := Record{Command: "git push   origin main"}
	if got, want := rec.Message(), "Waiting for approval: git push origin main"; got != want {
		t.Errorf("message = %q, want %q", got, want)
	}

	mcp := Record{Kind: KindMCP, ToolName: "linear_create_issue"}
	if got, want := mcp.Message(), "Waiting for approval: linear_create_issue"; got != want {
		t.Errorf("MCP message = %q, want %q", got, want)
	}

	long := Record{Command: strings.Repeat("x", 500)}
	if got := long.Message(); len(got) > 200 {
		t.Errorf("message length = %d, want a truncated body", len(got))
	}

	empty := Record{}
	if got, want := empty.Message(), "Waiting for your approval"; got != want {
		t.Errorf("empty message = %q, want %q", got, want)
	}
}

// newTestWatcher returns a watcher over a temp store whose process probe is
// stubbed, so tests never depend on what is running on the host.
func newTestWatcher(t *testing.T, running bool, ok bool) *Watcher {
	t.Helper()
	return &Watcher{
		Store: NewStoreAt(t.TempDir()),
		CommandRunning: func(string) (bool, bool) {
			return running, ok
		},
	}
}

func pendingRecord() Record {
	return Record{
		Key:             "testkey",
		Kind:            KindShell,
		SessionID:       "session-1",
		Command:         "make test",
		StartedUnixNano: time.Now().UnixNano(),
	}
}

// The common case: Cursor auto-approved the command and it finished, so the
// after* event cancels the watcher and nothing is reported.
func TestResolve_CompletedIsSilent(t *testing.T) {
	w := newTestWatcher(t, false, true)
	rec := pendingRecord()
	if err := w.Store.WritePending(rec); err != nil {
		t.Fatal(err)
	}
	if err := w.Store.MarkDone(rec.Key); err != nil {
		t.Fatal(err)
	}

	got, outcome := w.Resolve(rec.Key)
	if outcome != OutcomeCompleted {
		t.Fatalf("outcome = %q, want %q", outcome, OutcomeCompleted)
	}
	if outcome.ShouldNotify() {
		t.Error("a completed call must not notify")
	}
	if got.Command != rec.Command {
		t.Errorf("record not returned: %#v", got)
	}
	if w.Store.HasPending(rec.Key) {
		t.Error("pending state was not cleared")
	}
}

// An auto-approved command that outlives the grace period is still running, so
// it is slow rather than blocked on the user.
func TestResolve_StillRunningIsSilent(t *testing.T) {
	w := newTestWatcher(t, true, true)
	rec := pendingRecord()
	if err := w.Store.WritePending(rec); err != nil {
		t.Fatal(err)
	}

	if _, outcome := w.Resolve(rec.Key); outcome != OutcomeRunning {
		t.Fatalf("outcome = %q, want %q", outcome, OutcomeRunning)
	}
}

// Fallback for a completion marker whose key did not match used to silence the
// gate when any other call in the session finished. That incorrectly suppressed
// real approval waits when Cursor ran tools in parallel, so session activity is
// no longer consulted — an unresolved gate with no matching after* event waits.
func TestResolve_SessionActivityStillWaits(t *testing.T) {
	w := newTestWatcher(t, false, true)
	rec := pendingRecord()
	if err := w.Store.WritePending(rec); err != nil {
		t.Fatal(err)
	}
	if err := w.Store.TouchActivity(rec.SessionID); err != nil {
		t.Fatal(err)
	}

	if _, outcome := w.Resolve(rec.Key); outcome != OutcomeWaiting {
		t.Fatalf("outcome = %q, want %q", outcome, OutcomeWaiting)
	}
}

// Activity recorded before the gate opened says nothing about this call.
func TestResolve_EarlierActivityStillWaits(t *testing.T) {
	w := newTestWatcher(t, false, true)
	if err := w.Store.TouchActivity("session-1"); err != nil {
		t.Fatal(err)
	}

	rec := pendingRecord()
	rec.StartedUnixNano = time.Now().UnixNano() + int64(time.Second)
	if err := w.Store.WritePending(rec); err != nil {
		t.Fatal(err)
	}

	if _, outcome := w.Resolve(rec.Key); outcome != OutcomeWaiting {
		t.Fatalf("outcome = %q, want %q", outcome, OutcomeWaiting)
	}
}

// The case the whole mechanism exists for: nothing completed, nothing is
// running, so Cursor is sitting on an approval prompt.
func TestResolve_WaitingNotifies(t *testing.T) {
	w := newTestWatcher(t, false, true)
	rec := pendingRecord()
	if err := w.Store.WritePending(rec); err != nil {
		t.Fatal(err)
	}

	got, outcome := w.Resolve(rec.Key)
	if outcome != OutcomeWaiting {
		t.Fatalf("outcome = %q, want %q", outcome, OutcomeWaiting)
	}
	if !outcome.ShouldNotify() {
		t.Error("a real approval wait must notify")
	}
	if got.Message() != "Waiting for approval: make test" {
		t.Errorf("message = %q", got.Message())
	}
	if w.Store.HasPending(rec.Key) {
		t.Error("pending state was not cleared")
	}
}

// MCP gates have no process to inspect, so they rely purely on the markers.
func TestResolve_MCPGateSkipsProcessProbe(t *testing.T) {
	probeCalled := false
	w := &Watcher{
		Store: NewStoreAt(t.TempDir()),
		CommandRunning: func(string) (bool, bool) {
			probeCalled = true
			return true, true
		},
	}
	rec := Record{
		Key:             "mcpkey",
		Kind:            KindMCP,
		SessionID:       "session-1",
		ToolName:        "linear_create_issue",
		StartedUnixNano: time.Now().UnixNano(),
	}
	if err := w.Store.WritePending(rec); err != nil {
		t.Fatal(err)
	}

	if _, outcome := w.Resolve(rec.Key); outcome != OutcomeWaiting {
		t.Fatalf("outcome = %q, want %q", outcome, OutcomeWaiting)
	}
	if probeCalled {
		t.Error("MCP gate must not consult the shell process probe")
	}
}

// A platform that cannot inspect processes must not silence a real wait.
func TestResolve_UnknownProcessStateStillWaits(t *testing.T) {
	w := newTestWatcher(t, false, false)
	rec := pendingRecord()
	if err := w.Store.WritePending(rec); err != nil {
		t.Fatal(err)
	}

	if _, outcome := w.Resolve(rec.Key); outcome != OutcomeWaiting {
		t.Fatalf("outcome = %q, want %q", outcome, OutcomeWaiting)
	}
}

func TestResolve_MissingRecordConsumesOrphanMarker(t *testing.T) {
	w := newTestWatcher(t, false, true)
	if err := w.Store.MarkDone("orphan"); err != nil {
		t.Fatal(err)
	}

	if _, outcome := w.Resolve("orphan"); outcome != OutcomeMissing {
		t.Fatalf("outcome = %q, want %q", outcome, OutcomeMissing)
	}
	if w.Store.TakeDone("orphan") {
		t.Error("orphan completion marker was left behind")
	}
}

func TestStoreTakeDoneConsumesMarker(t *testing.T) {
	store := NewStoreAt(t.TempDir())
	if store.TakeDone("k") {
		t.Error("TakeDone reported a marker that was never written")
	}
	if err := store.MarkDone("k"); err != nil {
		t.Fatal(err)
	}
	if !store.TakeDone("k") {
		t.Error("TakeDone did not see the marker")
	}
	if store.TakeDone("k") {
		t.Error("marker was not consumed")
	}
}

func TestStoreRemovePendingIsIdempotent(t *testing.T) {
	store := NewStoreAt(t.TempDir())
	if err := store.RemovePending("missing"); err != nil {
		t.Errorf("RemovePending on missing key: %v", err)
	}
}

func TestStoreCleanupRemovesStaleState(t *testing.T) {
	store := NewStoreAt(t.TempDir())
	rec := pendingRecord()
	if err := store.WritePending(rec); err != nil {
		t.Fatal(err)
	}

	stale := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(store.pendingPath(rec.Key), stale, stale); err != nil {
		t.Fatal(err)
	}
	if err := store.Cleanup(3600); err != nil {
		t.Fatal(err)
	}
	if store.HasPending("testkey") {
		t.Error("stale pending state survived cleanup")
	}
}

func TestCommandRunning_ShortCommandIsUnknown(t *testing.T) {
	if _, ok := CommandRunning("ls"); ok {
		t.Error("a command too short to match must report unknown")
	}
}

func TestCommandLineMatches(t *testing.T) {
	needle := "make test"
	for _, cmdline := range []string{
		"make test",
		"sh -c make test",
		`bash -c "make test"`,
		"/bin/sh -c 'make test'",
	} {
		if !commandLineMatches(cmdline, needle) {
			t.Errorf("commandLineMatches(%q, %q) = false, want true", cmdline, needle)
		}
	}
	for _, cmdline := range []string{
		"make test-race",
		"echo make test",
		"vim /tmp/make test notes",
		"bash -c make test-extra",
	} {
		if commandLineMatches(cmdline, needle) {
			t.Errorf("commandLineMatches(%q, %q) = true, want false", cmdline, needle)
		}
	}
}

func TestCollapseWhitespace(t *testing.T) {
	if got, want := collapseWhitespace("  a \n b\tc  "), "a b c"; got != want {
		t.Errorf("collapseWhitespace = %q, want %q", got, want)
	}
}

func TestTruncateKeepsRunesIntact(t *testing.T) {
	if got := truncate("héllo wörld", 8); !strings.HasSuffix(got, "...") {
		t.Errorf("truncate = %q, want an ellipsis suffix", got)
	}
	if got := truncate("short", 10); got != "short" {
		t.Errorf("truncate shortened a fitting string: %q", got)
	}
}
