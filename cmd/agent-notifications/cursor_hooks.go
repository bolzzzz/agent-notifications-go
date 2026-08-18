package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const cursorHooksVersion = 1

type cursorHookEntry struct {
	Command string `json:"command"`
	Timeout int    `json:"timeout"`
}

type cursorHooksOptions struct {
	executable string
	hooksPath  string
}

// cursorHookSpecs are the Cursor CLI hook events this program registers.
// stop / sessionStart / subagentStop drive the end-of-turn notifications.
// The before*/after* pairs exist only to detect approval waits: Cursor has no
// event for "auto-review decided this needs you", so the before* gate records
// the call and the matching after* event cancels it once the call runs (see
// cursor_approval.go).
var cursorHookSpecs = []struct {
	event    string
	hookName string
}{
	{event: "sessionStart", hookName: "SessionStart"},
	{event: "stop", hookName: "Stop"},
	{event: "subagentStop", hookName: "SubagentStop"},
	{event: "beforeShellExecution", hookName: "beforeShellExecution"},
	{event: "afterShellExecution", hookName: "afterShellExecution"},
	{event: "beforeMCPExecution", hookName: "beforeMCPExecution"},
	{event: "afterMCPExecution", hookName: "afterMCPExecution"},
}

func runInstallCursorHooks(args []string) error {
	opts, err := parseCursorHooksOptions(args)
	if err != nil {
		return err
	}
	if err := updateCursorHooksFile(opts.hooksPath, opts.executable, false); err != nil {
		return err
	}
	fmt.Printf("Cursor CLI hooks installed: %s\n", opts.hooksPath)
	fmt.Printf("Executable: %s\n", opts.executable)
	return nil
}

func runUninstallCursorHooks(args []string) error {
	opts, err := parseCursorHooksOptions(args)
	if err != nil {
		return err
	}
	if err := updateCursorHooksFile(opts.hooksPath, opts.executable, true); err != nil {
		return err
	}
	fmt.Printf("Cursor CLI hooks removed: %s\n", opts.hooksPath)
	return nil
}

func parseCursorHooksOptions(args []string) (cursorHooksOptions, error) {
	executable, err := os.Executable()
	if err != nil {
		return cursorHooksOptions{}, fmt.Errorf("detect executable: %w", err)
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return cursorHooksOptions{}, fmt.Errorf("resolve executable: %w", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return cursorHooksOptions{}, fmt.Errorf("detect home directory: %w", err)
	}
	opts := cursorHooksOptions{
		executable: executable,
		hooksPath:  filepath.Join(home, ".cursor", "hooks.json"),
	}

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--exe":
			if i+1 >= len(args) {
				return cursorHooksOptions{}, fmt.Errorf("--exe requires a path")
			}
			i++
			opts.executable, err = filepath.Abs(args[i])
			if err != nil {
				return cursorHooksOptions{}, fmt.Errorf("resolve --exe: %w", err)
			}
		case "--hooks-path":
			if i+1 >= len(args) {
				return cursorHooksOptions{}, fmt.Errorf("--hooks-path requires a path")
			}
			i++
			opts.hooksPath, err = filepath.Abs(args[i])
			if err != nil {
				return cursorHooksOptions{}, fmt.Errorf("resolve --hooks-path: %w", err)
			}
		default:
			return cursorHooksOptions{}, fmt.Errorf("unknown option: %s", args[i])
		}
	}
	return opts, nil
}

// updateCursorHooksFile merges or removes this program's entries while
// preserving unrelated hooks and unknown top-level fields.
func updateCursorHooksFile(path, executable string, uninstall bool) error {
	root := make(map[string]json.RawMessage)
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &root); err != nil {
			return fmt.Errorf("parse existing Cursor hooks %s: %w", path, err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read Cursor hooks %s: %w", path, err)
	}

	hooks := make(map[string][]json.RawMessage)
	if raw := root["hooks"]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &hooks); err != nil {
			return fmt.Errorf("parse hooks object in %s: %w", path, err)
		}
	}

	// Drop any previously-installed agent-notifications entries so re-running
	// the command is idempotent and never duplicates hooks.
	for event, entries := range hooks {
		filtered := entries[:0]
		for _, entry := range entries {
			if !isAgentNotificationsCursorHook(entry) {
				filtered = append(filtered, entry)
			}
		}
		if len(filtered) == 0 {
			delete(hooks, event)
		} else {
			hooks[event] = filtered
		}
	}

	if !uninstall {
		for _, spec := range cursorHookSpecs {
			entry := cursorHookEntry{
				Command: cursorCommand(executable, spec.hookName),
				Timeout: 30,
			}
			raw, err := json.Marshal(entry)
			if err != nil {
				return fmt.Errorf("encode %s hook: %w", spec.event, err)
			}
			hooks[spec.event] = append(hooks[spec.event], raw)
		}
	}

	version, err := json.Marshal(cursorHooksVersion)
	if err != nil {
		return err
	}
	root["version"] = version
	hooksJSON, err := json.Marshal(hooks)
	if err != nil {
		return fmt.Errorf("encode hooks: %w", err)
	}
	root["hooks"] = hooksJSON

	output, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return fmt.Errorf("encode Cursor hooks file: %w", err)
	}
	output = append(output, '\n')

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create Cursor config directory: %w", err)
	}

	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".hooks-*.json")
	if err != nil {
		return fmt.Errorf("create temporary hooks file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("set temporary hooks permissions: %w", err)
	}
	if _, err := tmp.Write(output); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temporary hooks file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary hooks file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace Cursor hooks file: %w", err)
	}
	return nil
}

func cursorCommand(executable, hookName string) string {
	quoted := shellSingleQuoted(executable)
	if runtime.GOOS == "windows" {
		quoted = `"` + strings.ReplaceAll(executable, `"`, `\"`) + `"`
	}
	return fmt.Sprintf("%s handle-hook %s --product cursor", quoted, hookName)
}

func isAgentNotificationsCursorHook(raw json.RawMessage) bool {
	var entry struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(raw, &entry); err != nil {
		return false
	}
	return strings.Contains(entry.Command, "handle-hook") &&
		strings.Contains(entry.Command, "--product cursor")
}
