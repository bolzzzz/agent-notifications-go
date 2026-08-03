//go:build !linux

package main

import (
	"fmt"
	"os"
)

// runDaemon is a stub for non-Linux platforms
func runDaemon() {
	fmt.Fprintln(os.Stderr, "Error: notification daemon is only available on Linux")
	fmt.Fprintln(os.Stderr, "On macOS, click-to-focus uses terminal-notifier instead.")
	os.Exit(1)
}
