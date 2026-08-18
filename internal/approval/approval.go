// Package approval tracks Cursor shell/MCP approval gates so a notification is
// only sent when the agent really stays blocked on the user.
//
// Cursor fires beforeShellExecution / beforeMCPExecution *before* it decides
// whether a call needs the user's approval, and the payload carries no hint
// about that decision. An auto-approved call is therefore indistinguishable
// from one that pops an approval dialog. Instead of notifying inline, the gate
// records the pending call and a detached watcher re-checks it after a grace
// period. Either of two signals means the call was approved and nothing is
// waiting: the matching after* event arrived, or the command is still running.
// Session-wide "another tool finished" is intentionally not used — Cursor often
// runs tools in parallel, and a sibling's completion must not silence a gate
// that is still sitting on the approval prompt.
package approval

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/777genius/agent-notifications-go/internal/platform"
)

// filePrefix namespaces every file this package writes into the temp directory.
const filePrefix = "agent-notifications-approval-"

// minCommandNeedle is the shortest normalized command string worth matching
// against process command lines. Anything shorter matches unrelated processes,
// so the probe reports "cannot tell" instead.
const minCommandNeedle = 4

// Kind distinguishes the two Cursor approval gates.
type Kind string

const (
	KindShell Kind = "shell"
	KindMCP   Kind = "mcp"
)

// Record is a call captured at a before* gate, pending a verdict.
type Record struct {
	Key             string   `json:"key"`
	Kind            Kind     `json:"kind"`
	SessionID       string   `json:"sessionId"`
	Command         string   `json:"command,omitempty"`
	ToolName        string   `json:"toolName,omitempty"`
	CWD             string   `json:"cwd,omitempty"`
	WorkspaceRoots  []string `json:"workspaceRoots,omitempty"`
	TranscriptPath  string   `json:"transcriptPath,omitempty"`
	StartedUnixNano int64    `json:"startedUnixNano"`
}

// Target is the subject of the gate: the shell command, or the MCP tool name.
func (r Record) Target() string {
	if c := strings.TrimSpace(r.Command); c != "" {
		return c
	}
	return strings.TrimSpace(r.ToolName)
}

// Message renders the notification body for a gate that never resolved.
func (r Record) Message() string {
	target := collapseWhitespace(r.Target())
	if target == "" {
		return "Waiting for your approval"
	}
	return "Waiting for approval: " + truncate(target, 160)
}

// GatePayload is the subset of a Cursor before*/after* hook payload this
// package needs. Both events carry the same identifying fields, which is what
// lets an after* event cancel the watcher its before* sibling started.
type GatePayload struct {
	SessionID      string   `json:"session_id"`
	ConversationID string   `json:"conversation_id"`
	GenerationID   string   `json:"generation_id"`
	Command        string   `json:"command"`
	ToolName       string   `json:"tool_name"`
	ServerName     string   `json:"server_name"`
	CWD            string   `json:"cwd"`
	WorkspaceRoots []string `json:"workspace_roots"`
	TranscriptPath string   `json:"transcript_path"`
}

// ParseGatePayload decodes a Cursor before*/after* hook payload.
func ParseGatePayload(r io.Reader) (GatePayload, error) {
	var payload GatePayload
	if err := json.NewDecoder(r).Decode(&payload); err != nil {
		return GatePayload{}, fmt.Errorf("parse Cursor gate payload: %w", err)
	}
	return payload, nil
}

// Session returns the session identifier, preferring Cursor's session_id and
// falling back to conversation_id.
func (p GatePayload) Session() string {
	if s := strings.TrimSpace(p.SessionID); s != "" {
		return s
	}
	return strings.TrimSpace(p.ConversationID)
}

// target mirrors Record.Target for a raw payload.
func (p GatePayload) target() string {
	if c := strings.TrimSpace(p.Command); c != "" {
		return c
	}
	if t := strings.TrimSpace(p.ToolName); t != "" {
		return t
	}
	return strings.TrimSpace(p.ServerName)
}

// Key identifies one execution across its before* and after* events.
func (p GatePayload) Key() string {
	return Key(p.Session(), p.GenerationID, p.target())
}

// dir resolves the working directory, which Cursor often sends empty on shell
// gates; the first workspace root is the best available stand-in.
func (p GatePayload) dir() string {
	if c := strings.TrimSpace(p.CWD); c != "" {
		return c
	}
	if len(p.WorkspaceRoots) > 0 {
		return strings.TrimSpace(p.WorkspaceRoots[0])
	}
	return ""
}

// Record converts a before* payload into a pending record.
func (p GatePayload) Record(kind Kind) Record {
	return Record{
		Key:             p.Key(),
		Kind:            kind,
		SessionID:       p.Session(),
		Command:         strings.TrimSpace(p.Command),
		ToolName:        firstNonEmpty(strings.TrimSpace(p.ToolName), strings.TrimSpace(p.ServerName)),
		CWD:             p.dir(),
		WorkspaceRoots:  p.WorkspaceRoots,
		TranscriptPath:  strings.TrimSpace(p.TranscriptPath),
		StartedUnixNano: time.Now().UnixNano(),
	}
}

// Key hashes the identifying fields of one execution into a short file-safe id.
func Key(sessionID, generationID, target string) string {
	sum := sha256.Sum256([]byte(sessionID + "\x00" + generationID + "\x00" + collapseWhitespace(target)))
	return hex.EncodeToString(sum[:])[:16]
}

// Store persists pending gates, completion markers, and per-session activity
// stamps as files in the temp directory, so the detached watcher can read what
// the short-lived hook processes wrote.
type Store struct {
	dir string
}

// NewStore stores state in the system temp directory.
func NewStore() *Store {
	return &Store{dir: platform.TempDir()}
}

// NewStoreAt stores state in dir. Used by tests.
func NewStoreAt(dir string) *Store {
	return &Store{dir: dir}
}

func (s *Store) pendingPath(key string) string {
	return filepath.Join(s.dir, filePrefix+key+".pending")
}

func (s *Store) donePath(key string) string {
	return filepath.Join(s.dir, filePrefix+key+".done")
}

func (s *Store) activityPath(sessionID string) string {
	sum := sha256.Sum256([]byte(sessionID))
	return filepath.Join(s.dir, filePrefix+hex.EncodeToString(sum[:])[:16]+".activity")
}

// WritePending records a gate awaiting a verdict.
func (s *Store) WritePending(rec Record) error {
	data, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("encode pending approval record: %w", err)
	}
	return os.WriteFile(s.pendingPath(rec.Key), data, 0o600)
}

// ReadPending loads the gate recorded for key.
func (s *Store) ReadPending(key string) (Record, error) {
	data, err := os.ReadFile(s.pendingPath(key))
	if err != nil {
		return Record{}, err
	}
	var rec Record
	if err := json.Unmarshal(data, &rec); err != nil {
		return Record{}, fmt.Errorf("decode pending approval record: %w", err)
	}
	return rec, nil
}

// HasPending reports whether a gate is still awaiting a verdict.
func (s *Store) HasPending(key string) bool {
	return platform.FileExists(s.pendingPath(key))
}

// RemovePending discards the gate recorded for key.
func (s *Store) RemovePending(key string) error {
	err := os.Remove(s.pendingPath(key))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// MarkDone records that the execution behind key finished, which cancels the
// watcher started by its before* gate.
func (s *Store) MarkDone(key string) error {
	return os.WriteFile(s.donePath(key), nil, 0o600)
}

// TakeDone reports whether key completed, consuming the marker.
func (s *Store) TakeDone(key string) bool {
	path := s.donePath(key)
	if !platform.FileExists(path) {
		return false
	}
	_ = os.Remove(path)
	return true
}

// TouchActivity stamps the moment an execution completed in this session.
func (s *Store) TouchActivity(sessionID string) error {
	stamp := strconv.FormatInt(time.Now().UnixNano(), 10)
	return os.WriteFile(s.activityPath(sessionID), []byte(stamp), 0o600)
}

// ActivityAfter reports whether any execution in this session completed at or
// after the given instant. It is the fallback for a completion marker whose key
// did not match, and it deliberately errs toward "the agent kept working".
func (s *Store) ActivityAfter(sessionID string, unixNano int64) bool {
	data, err := os.ReadFile(s.activityPath(sessionID))
	if err != nil {
		return false
	}
	stamp, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return false
	}
	return stamp >= unixNano
}

// Cleanup removes state left behind by gates nobody resolved.
func (s *Store) Cleanup(maxAgeSeconds int64) error {
	return platform.CleanupOldFiles(s.dir, filePrefix+"*", maxAgeSeconds)
}

// Outcome explains how a pending gate resolved.
type Outcome string

const (
	// OutcomeMissing means no pending gate was recorded for the key.
	OutcomeMissing Outcome = "missing"
	// OutcomeCompleted means the matching after* event arrived.
	OutcomeCompleted Outcome = "completed"
	// OutcomeRunning means the command is still executing.
	OutcomeRunning Outcome = "running"
	// OutcomeWaiting means the agent is still blocked on the user.
	OutcomeWaiting Outcome = "waiting"
)

// ShouldNotify reports whether the outcome deserves a desktop notification.
func (o Outcome) ShouldNotify() bool {
	return o == OutcomeWaiting
}

// Watcher decides whether a pending gate turned into a real wait for the user.
type Watcher struct {
	Store *Store
	// CommandRunning reports whether a shell command is still executing. An
	// ok=false result means the platform cannot tell. Defaults to the platform
	// probe when nil.
	CommandRunning func(command string) (running, ok bool)
}

// Resolve classifies the gate recorded for key and clears its pending state.
// Call it only after the grace period has elapsed.
func (w *Watcher) Resolve(key string) (Record, Outcome) {
	rec, err := w.Store.ReadPending(key)
	if err != nil {
		// A completion marker with no pending record has nothing left to cancel.
		w.Store.TakeDone(key)
		return Record{}, OutcomeMissing
	}
	defer func() { _ = w.Store.RemovePending(key) }()

	if w.Store.TakeDone(key) {
		return rec, OutcomeCompleted
	}
	if rec.Kind == KindShell && rec.Command != "" {
		probe := w.CommandRunning
		if probe == nil {
			probe = CommandRunning
		}
		if running, ok := probe(rec.Command); ok && running {
			return rec, OutcomeRunning
		}
	}
	return rec, OutcomeWaiting
}

// collapseWhitespace reduces every whitespace run to a single space so a
// command reads the same in a payload, a process command line, and a
// notification body.
func collapseWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// truncate shortens s to max bytes without cutting a rune in half.
func truncate(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	cut := max - 3
	for cut > 0 && !isRuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "..."
}

func isRuneStart(b byte) bool {
	return b&0xC0 != 0x80
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// commandLineMatches reports whether cmdline is the given command or a common
// shell `-c` wrapper around it. Shared by the Linux /proc and macOS ps probes.
func commandLineMatches(cmdline, needle string) bool {
	if cmdline == needle {
		return true
	}
	for _, prefix := range []string{
		"sh -c ", "bash -c ", "zsh -c ",
		"/bin/sh -c ", "/bin/bash -c ", "/usr/bin/bash -c ",
	} {
		if cmdline == prefix+needle {
			return true
		}
		if cmdline == prefix+"'"+needle+"'" || cmdline == prefix+`"`+needle+`"` {
			return true
		}
	}
	return false
}
