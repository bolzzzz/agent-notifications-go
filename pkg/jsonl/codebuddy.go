package jsonl

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"time"
)

// CodeBuddy Code writes a transcript format that is structurally different from
// Claude Code's, so it cannot be fed to Parse. This file normalizes CodeBuddy
// records into the Claude-shaped Message values the analyzer and summary
// packages already consume, which lets the entire existing state machine be
// reused unchanged.
//
// The differences that motivate each mapping below:
//
//	                Claude Code                     CodeBuddy
//	assistant       type:"assistant"                type:"message" + role:"assistant"
//	content         nested message.content[]        top-level content[]
//	text block      type:"text"                     type:"output_text" / "input_text"
//	tool call       content block type:"tool_use"   separate record type:"function_call"
//	tool args       input (object)                  arguments (JSON string)
//	timestamp       RFC3339 string                  epoch milliseconds (number)
//	api error       isApiErrorMessage:true          providerData.error{}
//
// Feeding a CodeBuddy transcript to Parse yields zero messages: the numeric
// timestamp fails to unmarshal into a string field, so every line is skipped.

// codeBuddyTimeLayout is a fixed-width RFC3339 layout with exactly three
// fractional digits.
//
// The width matters. GetLastUserTimestamp returns a raw string that
// HasRecentApiError compares with `>=`, so timestamps must order correctly
// lexicographically. time.RFC3339Nano trims trailing zeros, which makes
// ".200Z" sort after ".1234Z" — wrong. A fixed width keeps byte order and
// chronological order identical. Values are emitted in UTC so the zone offset
// is a constant "Z" and never perturbs the comparison.
const codeBuddyTimeLayout = "2006-01-02T15:04:05.000Z07:00"

// codeBuddyRecord is one line of a CodeBuddy transcript. Fields absent from a
// given record type stay at their zero value.
type codeBuddyRecord struct {
	ID           string                `json:"id"`
	Type         string                `json:"type"`
	Role         string                `json:"role"`
	Status       string                `json:"status"`
	Timestamp    int64                 `json:"timestamp"`
	Content      []codeBuddyContent    `json:"content"`
	Name         string                `json:"name"`
	Arguments    string                `json:"arguments"`
	ProviderData *codeBuddyProviderMsg `json:"providerData"`
}

type codeBuddyContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type codeBuddyProviderMsg struct {
	SkipRun bool               `json:"skipRun"`
	IsMeta  bool               `json:"isMeta"`
	Error   *codeBuddyErrorMsg `json:"error"`
}

type codeBuddyErrorMsg struct {
	Message         string `json:"message"`
	IsNetworkError  bool   `json:"isNetworkError"`
	IsStreamTimeout bool   `json:"isStreamTimeout"`
	IsRetryable     bool   `json:"isRetryable"`
}

// ParseCodeBuddyFile parses a CodeBuddy transcript file into Claude-shaped
// messages.
func ParseCodeBuddyFile(path string) ([]Message, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	return ParseCodeBuddy(f)
}

// ParseCodeBuddy parses a CodeBuddy transcript from a reader.
//
// Like Parse, it tolerates arbitrarily long lines and skips malformed ones
// rather than failing the whole file: a transcript is diagnostic input, and a
// single unreadable line should degrade the notification, not suppress it.
func ParseCodeBuddy(r io.Reader) ([]Message, error) {
	var (
		messages []Message
		// byID maps a CodeBuddy record id to the index in messages of the
		// assistant message carrying it, so function_call records can be merged
		// into their parent.
		byID = make(map[string]int)
	)

	reader := bufio.NewReaderSize(r, 64*1024)
	for {
		line, err := reader.ReadBytes('\n')
		line = bytes.TrimRight(line, "\r\n")
		if len(line) > 0 {
			var rec codeBuddyRecord
			if jsonErr := json.Unmarshal(line, &rec); jsonErr == nil {
				appendCodeBuddyRecord(&messages, byID, rec)
			}
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
	}

	return messages, nil
}

// appendCodeBuddyRecord folds a single record into the accumulating message
// list.
func appendCodeBuddyRecord(messages *[]Message, byID map[string]int, rec codeBuddyRecord) {
	switch rec.Type {
	case "message":
		appendCodeBuddyMessage(messages, byID, rec)
	case "function_call":
		appendCodeBuddyToolUse(messages, byID, rec)
	}
	// Everything else is intentionally dropped:
	//
	//   function_call_result   — the Claude-side equivalent (a tool_result block
	//                            on a user message) is ignored by the analyzer
	//                            anyway, and materializing it as a user message
	//                            would reset the "current turn" window that
	//                            GetLastUserTimestamp establishes.
	//   file-history-snapshot  — editor bookkeeping, no conversational content.
	//   ai-title               — generated session title, not part of the turn.
	//   summary                — compaction artifact.
}

func appendCodeBuddyMessage(messages *[]Message, byID map[string]int, rec codeBuddyRecord) {
	role := rec.Role
	if role != "assistant" && role != "user" {
		return
	}

	// CodeBuddy injects synthetic records that the user never typed, and they
	// must not be treated as real turns: their timestamps interleave with the
	// assistant's, so letting one become "the last user message" would make
	// FilterMessagesAfterTimestamp discard the very turn being reported on.
	//
	// Two distinct markers exist, and both matter:
	//   skipRun — system reminders, slash-command echoes, command output
	//   isMeta  — content injected back into the history by the runtime, such
	//             as hook stderr ("Stop hook: ...") and the reason text from a
	//             blocking prompt hook or /goal. These are especially damaging
	//             because they land *after* the assistant's reply.
	if pd := rec.ProviderData; pd != nil && (pd.SkipRun || pd.IsMeta) {
		return
	}
	// Belt-and-braces for scaffolding that arrives without the skipRun marker.
	if role == "user" && isCodeBuddyScaffolding(rec.Content) {
		return
	}

	msg := Message{
		Type:      role,
		Timestamp: codeBuddyTimestamp(rec.Timestamp),
		Message: MessageContent{
			Role: role,
		},
	}

	text := codeBuddyText(rec.Content)
	if text != "" {
		// Populate both shapes. GetLastUserTimestamp accepts either a string
		// content or an array whose first block is text, and the summary code
		// reads the array — filling both keeps every existing helper working.
		msg.Message.Content = []Content{{Type: "text", Text: text}}
		if role == "user" {
			msg.Message.ContentString = text
		}
	}

	if role == "assistant" {
		if apiErr, errText := codeBuddyAPIError(rec); apiErr {
			msg.IsApiErrorMessage = true
			msg.Error = errText
			if len(msg.Message.Content) == 0 && errText != "" {
				msg.Message.Content = []Content{{Type: "text", Text: errText}}
			}
		}
	}

	*messages = append(*messages, msg)

	// Record where this id landed so sibling function_call records can attach.
	// Parallel tool calls share their parent's id, so later calls find the same
	// slot and append to it.
	if role == "assistant" && rec.ID != "" {
		byID[rec.ID] = len(*messages) - 1
	}
}

// appendCodeBuddyToolUse turns a standalone function_call record into a
// tool_use content block.
//
// It is merged into the assistant message sharing its id rather than becoming
// its own message. ExtractTools reports each tool's Position as a message
// index, and the analyzer only inspects the last 15 messages; a turn issuing
// several parallel tool calls would otherwise inflate the message count enough
// to push the rest of the turn out of that window. Merging also mirrors Claude
// Code, where tool_use blocks live inside the assistant message.
//
// When the model calls a tool without saying anything first, CodeBuddy writes
// the function_call with no accompanying assistant message, so there is no
// parent to merge into. Observed transcripts contain runs of up to nine such
// records in a row. Giving each its own message would consume most of the
// 15-message window on tool calls alone, so a consecutive run is coalesced into
// a single synthetic assistant message — which is also how Claude Code would
// have represented the same parallel calls.
func appendCodeBuddyToolUse(messages *[]Message, byID map[string]int, rec codeBuddyRecord) {
	if rec.Name == "" {
		return
	}

	block := Content{
		Type:  "tool_use",
		Name:  rec.Name,
		Input: codeBuddyArguments(rec.Arguments),
	}

	// Same-id sibling: a parallel call from a parent that did emit text.
	if idx, ok := byID[rec.ID]; ok && idx < len(*messages) {
		parent := &(*messages)[idx]
		parent.Message.Content = append(parent.Message.Content, block)
		return
	}

	// Orphan: extend the run if the previous message is itself a synthetic
	// tool-only assistant message, otherwise start a new one.
	if n := len(*messages); n > 0 {
		if last := &(*messages)[n-1]; isCodeBuddyToolOnlyMessage(last) {
			last.Message.Content = append(last.Message.Content, block)
			if rec.ID != "" {
				byID[rec.ID] = n - 1
			}
			return
		}
	}

	*messages = append(*messages, Message{
		Type:      "assistant",
		Timestamp: codeBuddyTimestamp(rec.Timestamp),
		Message: MessageContent{
			Role:    "assistant",
			Content: []Content{block},
		},
	})
	if rec.ID != "" {
		byID[rec.ID] = len(*messages) - 1
	}
}

// isCodeBuddyToolOnlyMessage reports whether a message is a synthetic assistant
// message built purely from orphan tool calls, and so is safe to extend.
//
// Messages carrying text are excluded: appending a later tool call to one would
// misrepresent ordering, since the text was written before the call was made.
func isCodeBuddyToolOnlyMessage(msg *Message) bool {
	if msg.Type != "assistant" || msg.IsApiErrorMessage {
		return false
	}
	if msg.Message.ContentString != "" || len(msg.Message.Content) == 0 {
		return false
	}
	for _, block := range msg.Message.Content {
		if block.Type != "tool_use" {
			return false
		}
	}
	return true
}

// codeBuddyArguments decodes a function_call's JSON-string arguments.
//
// Failures are deliberately non-fatal and return an empty map: status
// classification depends only on the tool *name*, while the arguments merely
// enrich the summary text. Dropping the tool because its arguments are
// unparseable would turn a cosmetic problem into a missed notification.
func codeBuddyArguments(arguments string) map[string]interface{} {
	if strings.TrimSpace(arguments) == "" {
		return map[string]interface{}{}
	}
	var input map[string]interface{}
	if err := json.Unmarshal([]byte(arguments), &input); err != nil || input == nil {
		return map[string]interface{}{}
	}
	return input
}

// codeBuddyText concatenates the text blocks of a record, mapping CodeBuddy's
// role-specific block names onto Claude's single "text" type.
func codeBuddyText(blocks []codeBuddyContent) string {
	var b strings.Builder
	for _, block := range blocks {
		switch block.Type {
		case "output_text", "input_text", "text":
			if block.Text == "" {
				continue
			}
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(block.Text)
		}
	}
	return b.String()
}

// codeBuddyTimestamp converts epoch milliseconds to the fixed-width UTC string
// the Claude-shaped helpers parse and compare. Non-positive values yield an
// empty string, matching how Parse represents a missing timestamp.
func codeBuddyTimestamp(ms int64) string {
	if ms <= 0 {
		return ""
	}
	return time.UnixMilli(ms).UTC().Format(codeBuddyTimeLayout)
}

// codeBuddyAPIError reports whether a record represents a genuine API failure.
//
// A user pressing Esc also produces providerData.error with
// status:"incomplete", so the error text is screened first. Reporting an
// interrupt as an API error would fire an error notification every time the
// user deliberately stops the agent — the opposite of useful.
func codeBuddyAPIError(rec codeBuddyRecord) (bool, string) {
	if rec.ProviderData == nil || rec.ProviderData.Error == nil {
		return false, ""
	}
	msg := rec.ProviderData.Error.Message
	if isCodeBuddyUserInterrupt(msg) {
		return false, ""
	}
	return true, msg
}

// codeBuddyInterruptPhrases are the cancellation messages CodeBuddy reports
// through the same error channel as real failures.
var codeBuddyInterruptPhrases = []string{
	"interrupted by user",
	"aborted by user",
	"cancelled by user",
	"canceled by user",
	"request was aborted",
	"operation was aborted",
	"user rejected",
	"abenterror",
}

func isCodeBuddyUserInterrupt(message string) bool {
	lower := strings.ToLower(strings.TrimSpace(message))
	if lower == "" {
		// An error object with no message carries no evidence of a real
		// failure; treat it as benign rather than notifying on nothing.
		return true
	}
	for _, phrase := range codeBuddyInterruptPhrases {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}

// codeBuddyScaffoldingPrefixes mark injected pseudo-user turns that the user
// never typed.
var codeBuddyScaffoldingPrefixes = []string{
	"<system-reminder",
	"<command-name>",
	"<command-message>",
	"<local-command-stdout>",
	"<user-prompt-submit-hook>",
}

// isCodeBuddyScaffolding reports whether a user record is entirely injected
// content rather than a real user message.
func isCodeBuddyScaffolding(blocks []codeBuddyContent) bool {
	text := strings.TrimSpace(codeBuddyText(blocks))
	if text == "" {
		return false
	}
	lower := strings.ToLower(text)
	for _, prefix := range codeBuddyScaffoldingPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}
