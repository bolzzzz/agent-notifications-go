//go:build linux

package approval

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// CommandRunning reports whether a process is currently running the command.
// A running command means Cursor already approved it, so a gate that has not
// completed yet is merely slow rather than blocked on the user.
//
// Matching is intentionally strict: the collapsed cmdline must equal the
// command, or be a shell `-c` wrapper around it. A loose substring match used
// to silence real approval waits whenever an unrelated process happened to
// contain the same text (or a previous instance of the same command lingered).
//
// ok=false means the answer is unknown: /proc was unreadable, or the command is
// too short to match without hitting unrelated processes.
func CommandRunning(command string) (running, ok bool) {
	needle := collapseWhitespace(command)
	if len(needle) < minCommandNeedle {
		return false, false
	}

	entries, err := os.ReadDir("/proc")
	if err != nil {
		return false, false
	}

	self := os.Getpid()
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid == self {
			continue
		}
		data, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "cmdline"))
		if err != nil || len(data) == 0 {
			continue
		}
		cmdline := collapseWhitespace(strings.ReplaceAll(string(data), "\x00", " "))
		if commandLineMatches(cmdline, needle) {
			return true, true
		}
	}
	return false, true
}
