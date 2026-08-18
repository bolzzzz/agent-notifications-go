//go:build !linux && !darwin

package approval

// CommandRunning cannot inspect processes on this platform, so it always
// reports "unknown". A slow but approved command therefore looks the same as a
// blocked one; approvalWaitSeconds is the only guard against that.
func CommandRunning(command string) (running, ok bool) {
	return false, false
}
