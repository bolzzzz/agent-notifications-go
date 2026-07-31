//go:build windows

package notifier

import (
	"testing"

	"golang.org/x/sys/windows"
)

// TestParentSnapshot_IncludesSelf verifies the process snapshot is populated and
// contains the current process (the table ancestorPIDs walks).
func TestParentSnapshot_IncludesSelf(t *testing.T) {
	snap := parentSnapshot()
	self := windows.GetCurrentProcessId()
	if _, ok := snap[self]; !ok {
		t.Errorf("parentSnapshot() missing current process %d (got %d entries)", self, len(snap))
	}
}

// TestAncestorPIDs_WellFormed checks the AttachConsole fallback targets: the
// chain excludes the current process, has no zero or duplicate PIDs, and walks
// at least to the parent.
func TestAncestorPIDs_WellFormed(t *testing.T) {
	self := windows.GetCurrentProcessId()
	chain := ancestorPIDs()

	if len(chain) == 0 {
		t.Fatalf("ancestorPIDs() returned no ancestors; expected at least the parent")
	}

	seen := map[uint32]bool{}
	for _, pid := range chain {
		switch {
		case pid == 0:
			t.Errorf("ancestorPIDs() returned a zero PID: %v", chain)
		case pid == self:
			t.Errorf("ancestorPIDs() included the current process %d: %v", self, chain)
		case seen[pid]:
			t.Errorf("ancestorPIDs() returned a duplicate PID %d: %v", pid, chain)
		}
		seen[pid] = true
	}
}

func TestAncestorChainStopsBeforeCycle(t *testing.T) {
	parents := map[uint32]uint32{
		10: 20,
		20: 30,
		30: 20,
	}

	chain := ancestorChain(parents, 10)
	want := []uint32{20, 30}
	if len(chain) != len(want) {
		t.Fatalf("ancestorChain() = %v, want %v", chain, want)
	}
	for i := range want {
		if chain[i] != want[i] {
			t.Fatalf("ancestorChain() = %v, want %v", chain, want)
		}
	}
}
