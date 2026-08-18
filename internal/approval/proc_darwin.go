//go:build darwin

package approval

import (
	"bufio"
	"bytes"
	"context"
	"os/exec"
	"time"
)

// procListTimeout bounds the process listing so a wedged ps never keeps the
// detached watcher alive.
const procListTimeout = 3 * time.Second

// CommandRunning reports whether a process is currently running the command.
// A running command means Cursor already approved it, so a gate that has not
// completed yet is merely slow rather than blocked on the user.
//
// Matching is intentionally strict: see commandLineMatches. A loose substring
// match used to silence real approval waits when an unrelated process contained
// the same text.
//
// ok=false means the answer is unknown: ps failed, or the command is too short
// to match without hitting unrelated processes.
func CommandRunning(command string) (running, ok bool) {
	needle := collapseWhitespace(command)
	if len(needle) < minCommandNeedle {
		return false, false
	}

	ctx, cancel := context.WithTimeout(context.Background(), procListTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, "ps", "-axo", "command=").Output()
	if err != nil {
		return false, false
	}

	scanner := bufio.NewScanner(bytes.NewReader(out))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		if commandLineMatches(collapseWhitespace(scanner.Text()), needle) {
			return true, true
		}
	}
	return false, true
}
