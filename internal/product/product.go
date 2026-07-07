// Package product identifies which AI CLI (Claude Code or CodeBuddy) is invoking
// the hook. Both tools share an identical hooks contract (JSON over stdin, same
// event names), so the only signals that differ are the environment variables
// each tool exports for its plugin mechanism.
package product

import "os"

const (
	// Claude is Anthropic's Claude Code CLI.
	Claude = "claude"
	// CodeBuddy is Tencent's CodeBuddy Code CLI.
	CodeBuddy = "codebuddy"
)

// Detect returns the product currently invoking the hook.
// CodeBuddy exports CODEBUDDY_PLUGIN_ROOT for its plugin hooks; when that is
// present we treat the caller as CodeBuddy, otherwise Claude Code.
func Detect() string {
	if os.Getenv("CODEBUDDY_PLUGIN_ROOT") != "" {
		return CodeBuddy
	}
	return Claude
}

// PluginRoot returns the plugin-root environment variable for the current
// product. CodeBuddy uses CODEBUDDY_PLUGIN_ROOT; Claude Code uses
// CLAUDE_PLUGIN_ROOT. This lets default asset paths (icon, sounds) resolve
// correctly under either tool even when invoked directly (without the wrapper
// script that normally re-exports CLAUDE_PLUGIN_ROOT).
func PluginRoot() string {
	if v := os.Getenv("CODEBUDDY_PLUGIN_ROOT"); v != "" {
		return v
	}
	return os.Getenv("CLAUDE_PLUGIN_ROOT")
}

// Name returns a human-friendly product name for user-visible text
// (notifications, webhook footers, logs).
func Name() string {
	if Detect() == CodeBuddy {
		return "CodeBuddy"
	}
	return "Claude Code"
}

// ShouldRespectJudgeMode reports whether the caller's judge-mode environment
// variable is set to "true". Both tools may spawn background judge instances
// (e.g. double-shot-latte); CodeBuddy uses CODEBUDDY_HOOK_JUDGE_MODE.
func ShouldRespectJudgeMode() bool {
	return os.Getenv("CODEBUDDY_HOOK_JUDGE_MODE") == "true" ||
		os.Getenv("CLAUDE_HOOK_JUDGE_MODE") == "true"
}
