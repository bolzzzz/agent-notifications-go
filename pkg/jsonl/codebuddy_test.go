package jsonl

import (
	"strings"
	"testing"
)

// parseCB is a helper that parses a CodeBuddy transcript from a literal string.
func parseCB(t *testing.T, lines ...string) []Message {
	t.Helper()
	msgs, err := ParseCodeBuddy(strings.NewReader(strings.Join(lines, "\n")))
	if err != nil {
		t.Fatalf("ParseCodeBuddy() error = %v", err)
	}
	return msgs
}

func TestParseCodeBuddy_BasicShape(t *testing.T) {
	msgs := parseCB(t,
		`{"id":"u1","type":"message","role":"user","timestamp":1785757068624,"content":[{"type":"input_text","text":"帮我改个 bug"}]}`,
		`{"id":"a1","type":"message","role":"assistant","timestamp":1785757070000,"content":[{"type":"output_text","text":"好的"}]}`,
	)

	if len(msgs) != 2 {
		t.Fatalf("len(msgs) = %d, want 2", len(msgs))
	}

	// type:"message"+role must collapse into the Claude-shaped Type field,
	// otherwise every downstream helper (which switches on "assistant"/"user")
	// silently sees nothing.
	if msgs[0].Type != "user" || msgs[1].Type != "assistant" {
		t.Errorf("Type = %q/%q, want user/assistant", msgs[0].Type, msgs[1].Type)
	}

	// input_text/output_text must be normalized to "text".
	if got := msgs[1].Message.Content[0].Type; got != "text" {
		t.Errorf("content type = %q, want text", got)
	}
	if got := msgs[1].Message.Content[0].Text; got != "好的" {
		t.Errorf("text = %q, want 好的", got)
	}

	// GetLastUserTimestamp accepts either shape; user messages fill both so the
	// summary code (array) and the windowing code (string) both work.
	if msgs[0].Message.ContentString != "帮我改个 bug" {
		t.Errorf("ContentString = %q", msgs[0].Message.ContentString)
	}
	if got := GetLastUserTimestamp(msgs); got != "2026-08-03T11:37:48.624Z" {
		t.Errorf("GetLastUserTimestamp() = %q", got)
	}
}

func TestParseCodeBuddy_ClaudeFormatYieldsNothing(t *testing.T) {
	// Guards the dispatch decision: the two formats are not interchangeable, so
	// routing must be product-driven rather than best-effort.
	msgs := parseCB(t,
		`{"parentUuid":null,"type":"user","timestamp":"2026-08-03T10:00:00.000Z","message":{"role":"user","content":"hi"}}`,
	)
	if len(msgs) != 0 {
		t.Errorf("len(msgs) = %d, want 0 for Claude-format input", len(msgs))
	}
}

func TestParseCodeBuddy_ToolCallMergedIntoParent(t *testing.T) {
	// Parallel tool calls share the parent assistant message's id.
	msgs := parseCB(t,
		`{"id":"a1","type":"message","role":"assistant","timestamp":1785757070000,"content":[{"type":"output_text","text":"查一下"}]}`,
		`{"id":"a1","type":"function_call","timestamp":1785757070100,"name":"Read","arguments":"{\"file_path\":\"/tmp/a\"}"}`,
		`{"id":"a1","type":"function_call","timestamp":1785757070200,"name":"Grep","arguments":"{\"pattern\":\"foo\"}"}`,
	)

	if len(msgs) != 1 {
		t.Fatalf("len(msgs) = %d, want 1 (calls merge into parent)", len(msgs))
	}
	blocks := msgs[0].Message.Content
	if len(blocks) != 3 {
		t.Fatalf("len(blocks) = %d, want 3 (text + 2 tool_use)", len(blocks))
	}
	if blocks[1].Type != "tool_use" || blocks[1].Name != "Read" {
		t.Errorf("block[1] = %+v, want tool_use/Read", blocks[1])
	}
	if got := blocks[1].Input["file_path"]; got != "/tmp/a" {
		t.Errorf("Input[file_path] = %v, want /tmp/a", got)
	}

	tools := ExtractTools(msgs)
	if len(tools) != 2 {
		t.Fatalf("len(tools) = %d, want 2", len(tools))
	}
	// Both tools must report the same position — that is what keeps the
	// analyzer's 15-message window from being consumed by one parallel batch.
	if tools[0].Position != tools[1].Position {
		t.Errorf("positions = %d/%d, want equal", tools[0].Position, tools[1].Position)
	}
}

func TestParseCodeBuddy_OrphanToolCallsCoalesce(t *testing.T) {
	// When the model calls tools without emitting text first, CodeBuddy writes
	// function_call records with no parent message. Real transcripts contain
	// runs of up to nine; each becoming its own message would blow out the
	// analyzer's 15-message window.
	lines := []string{
		`{"id":"u1","type":"message","role":"user","timestamp":1785757068000,"content":[{"type":"input_text","text":"go"}]}`,
	}
	for i := 0; i < 9; i++ {
		lines = append(lines, `{"id":"orphan-`+string(rune('a'+i))+`","type":"function_call","timestamp":178575707000`+string(rune('0'+i))+`,"name":"Read","arguments":"{}"}`)
	}
	msgs := parseCB(t, lines...)

	if len(msgs) != 2 {
		t.Fatalf("len(msgs) = %d, want 2 (user + one coalesced assistant)", len(msgs))
	}
	if got := len(ExtractTools(msgs)); got != 9 {
		t.Errorf("len(tools) = %d, want 9 (none dropped)", got)
	}
}

func TestParseCodeBuddy_OrphanRunDoesNotAbsorbLaterText(t *testing.T) {
	// A text-bearing message must start a fresh message rather than being
	// appended to the orphan run, so ordering stays truthful.
	msgs := parseCB(t,
		`{"id":"o1","type":"function_call","timestamp":1785757070000,"name":"Read","arguments":"{}"}`,
		`{"id":"a1","type":"message","role":"assistant","timestamp":1785757071000,"content":[{"type":"output_text","text":"done"}]}`,
		`{"id":"o2","type":"function_call","timestamp":1785757072000,"name":"Bash","arguments":"{}"}`,
	)
	if len(msgs) != 3 {
		t.Fatalf("len(msgs) = %d, want 3", len(msgs))
	}
	tools := ExtractTools(msgs)
	if len(tools) != 2 {
		t.Fatalf("len(tools) = %d, want 2", len(tools))
	}
	if GetLastTool(tools) != "Bash" {
		t.Errorf("GetLastTool() = %q, want Bash", GetLastTool(tools))
	}
}

func TestParseCodeBuddy_MalformedArgumentsKeepsTool(t *testing.T) {
	// Status classification depends only on the tool name; unparseable
	// arguments must degrade the summary, never suppress the notification.
	msgs := parseCB(t,
		`{"id":"a1","type":"function_call","timestamp":1785757070000,"name":"Edit","arguments":"{not valid json"}`,
		`{"id":"a2","type":"function_call","timestamp":1785757071000,"name":"Write","arguments":""}`,
	)
	tools := ExtractTools(msgs)
	if len(tools) != 2 {
		t.Fatalf("len(tools) = %d, want 2", len(tools))
	}
	for _, m := range msgs {
		for _, b := range m.Message.Content {
			if b.Type == "tool_use" && b.Input == nil {
				t.Errorf("tool %q has nil Input, want empty map", b.Name)
			}
		}
	}
}

func TestParseCodeBuddy_UnnamedToolCallDropped(t *testing.T) {
	msgs := parseCB(t,
		`{"id":"a1","type":"function_call","timestamp":1785757070000,"arguments":"{}"}`,
	)
	if len(msgs) != 0 {
		t.Errorf("len(msgs) = %d, want 0 for unnamed tool call", len(msgs))
	}
}

func TestParseCodeBuddy_InjectedUserRecordsIgnored(t *testing.T) {
	// The critical windowing case. skipRun marks system reminders and slash
	// echoes; isMeta marks runtime-injected content such as hook stderr. Both
	// land *after* the assistant reply, so treating either as the last user
	// message would make FilterMessagesAfterTimestamp discard the whole turn.
	msgs := parseCB(t,
		`{"id":"u1","type":"message","role":"user","timestamp":1785757060000,"content":[{"type":"input_text","text":"真实提问"}]}`,
		`{"id":"a1","type":"message","role":"assistant","timestamp":1785757070000,"content":[{"type":"output_text","text":"回答"}]}`,
		`{"id":"a1","type":"function_call","timestamp":1785757070500,"name":"Bash","arguments":"{}"}`,
		`{"id":"m1","type":"message","role":"user","timestamp":1785757080000,"providerData":{"isMeta":true},"content":[{"type":"input_text","text":"Stop hook: sh: 0: cannot open ..."}]}`,
		`{"id":"s1","type":"message","role":"user","timestamp":1785757081000,"providerData":{"skipRun":true},"content":[{"type":"input_text","text":"<command-name>/foo</command-name>"}]}`,
	)

	var users int
	for _, m := range msgs {
		if m.Type == "user" {
			users++
		}
	}
	if users != 1 {
		t.Fatalf("user messages = %d, want 1 (injected records dropped)", users)
	}

	ts := GetLastUserTimestamp(msgs)
	if ts != "2026-08-03T11:37:40.000Z" {
		t.Fatalf("GetLastUserTimestamp() = %q, want the real user turn", ts)
	}
	// The assistant turn must survive the window filter.
	win := FilterMessagesAfterTimestamp(msgs, ts)
	if len(win) != 1 {
		t.Fatalf("windowed messages = %d, want 1", len(win))
	}
	if got := len(ExtractTools(win)); got != 1 {
		t.Errorf("windowed tools = %d, want 1", got)
	}
}

func TestParseCodeBuddy_ScaffoldingPrefixFallback(t *testing.T) {
	// Same protection for scaffolding that arrives without an explicit marker.
	msgs := parseCB(t,
		`{"id":"u1","type":"message","role":"user","timestamp":1785757060000,"content":[{"type":"input_text","text":"真实提问"}]}`,
		`{"id":"u2","type":"message","role":"user","timestamp":1785757090000,"content":[{"type":"input_text","text":"<system-reminder>注入内容</system-reminder>"}]}`,
	)
	if got := GetLastUserTimestamp(msgs); got != "2026-08-03T11:37:40.000Z" {
		t.Errorf("GetLastUserTimestamp() = %q, want the real user turn", got)
	}
}

func TestParseCodeBuddy_UserInterruptIsNotAPIError(t *testing.T) {
	// Pressing Esc produces providerData.error with status "incomplete".
	// Reporting that as an API error would fire an error notification every
	// time the user deliberately stops the agent.
	msgs := parseCB(t,
		`{"id":"a1","type":"message","role":"assistant","timestamp":1785757070000,"status":"incomplete","content":[{"type":"output_text","text":"Interrupted by user"}],"providerData":{"error":{"message":"Interrupted by user","isRetryable":false}}}`,
	)
	if len(msgs) != 1 {
		t.Fatalf("len(msgs) = %d, want 1", len(msgs))
	}
	if msgs[0].IsApiErrorMessage {
		t.Error("IsApiErrorMessage = true, want false for a user interrupt")
	}
	if HasRecentApiError(msgs) {
		t.Error("HasRecentApiError() = true, want false")
	}
}

func TestParseCodeBuddy_RealAPIErrorDetected(t *testing.T) {
	msgs := parseCB(t,
		`{"id":"a1","type":"message","role":"assistant","timestamp":1785757070000,"status":"incomplete","content":[],"providerData":{"error":{"message":"upstream returned 529 overloaded","isNetworkError":true}}}`,
	)
	if len(msgs) != 1 {
		t.Fatalf("len(msgs) = %d, want 1", len(msgs))
	}
	if !msgs[0].IsApiErrorMessage {
		t.Fatal("IsApiErrorMessage = false, want true")
	}
	// The message must survive into the content so summaries have something to
	// show even though the record carried no text blocks.
	if len(msgs[0].Message.Content) == 0 {
		t.Error("error message produced no content block")
	}
	if !HasRecentApiError(msgs) {
		t.Error("HasRecentApiError() = false, want true")
	}
}

func TestParseCodeBuddy_EmptyErrorObjectIsNotAnError(t *testing.T) {
	msgs := parseCB(t,
		`{"id":"a1","type":"message","role":"assistant","timestamp":1785757070000,"content":[{"type":"output_text","text":"ok"}],"providerData":{"error":{"message":""}}}`,
	)
	if msgs[0].IsApiErrorMessage {
		t.Error("IsApiErrorMessage = true, want false for an empty error message")
	}
}

func TestCodeBuddyTimestampOrdering(t *testing.T) {
	// HasRecentApiError compares timestamps as strings, so byte order must match
	// chronological order. RFC3339Nano trims trailing zeros and would make
	// ".200Z" sort after ".1234Z"; the fixed-width layout avoids that.
	early := codeBuddyTimestamp(1785757070200) // .200
	late := codeBuddyTimestamp(1785757070999)  // .999
	if !(early < late) {
		t.Errorf("%q should sort before %q", early, late)
	}

	crossSecond := codeBuddyTimestamp(1785757071001)
	if !(late < crossSecond) {
		t.Errorf("%q should sort before %q", late, crossSecond)
	}

	if got := codeBuddyTimestamp(0); got != "" {
		t.Errorf("codeBuddyTimestamp(0) = %q, want empty", got)
	}
	if got := codeBuddyTimestamp(-1); got != "" {
		t.Errorf("codeBuddyTimestamp(-1) = %q, want empty", got)
	}

	// Must round-trip through the parser the Claude helpers use.
	if got := codeBuddyTimestamp(1785757070200); len(got) != len("2026-08-03T11:37:50.200Z") {
		t.Errorf("timestamp %q is not fixed width", got)
	}
}

func TestParseCodeBuddy_SkipsUnparseableAndUnknownRecords(t *testing.T) {
	msgs := parseCB(t,
		`{"id":"u1","type":"message","role":"user","timestamp":1785757060000,"content":[{"type":"input_text","text":"hi"}]}`,
		`{ this is not json`,
		`{"type":"file-history-snapshot","id":"x"}`,
		`{"type":"ai-title","id":"y"}`,
		`{"type":"function_call_result","id":"z","output":"..."}`,
		`{"id":"a1","type":"message","role":"system","timestamp":1785757061000,"content":[{"type":"output_text","text":"sys"}]}`,
		`{"id":"a2","type":"message","role":"assistant","timestamp":1785757070000,"content":[{"type":"output_text","text":"bye"}]}`,
	)
	if len(msgs) != 2 {
		t.Fatalf("len(msgs) = %d, want 2 (only user + assistant kept)", len(msgs))
	}
}

func TestParseCodeBuddy_HandlesLongLinesAndNoTrailingNewline(t *testing.T) {
	long := strings.Repeat("x", 200*1024)
	msgs := parseCB(t,
		`{"id":"a1","type":"message","role":"assistant","timestamp":1785757070000,"content":[{"type":"output_text","text":"`+long+`"}]}`,
	)
	if len(msgs) != 1 {
		t.Fatalf("len(msgs) = %d, want 1", len(msgs))
	}
	if got := len(msgs[0].Message.Content[0].Text); got != len(long) {
		t.Errorf("text length = %d, want %d", got, len(long))
	}
}

func TestParseCodeBuddy_MultipleTextBlocksJoined(t *testing.T) {
	msgs := parseCB(t,
		`{"id":"a1","type":"message","role":"assistant","timestamp":1785757070000,"content":[{"type":"output_text","text":"第一段"},{"type":"output_text","text":"第二段"}]}`,
	)
	if got := msgs[0].Message.Content[0].Text; got != "第一段\n第二段" {
		t.Errorf("text = %q, want joined blocks", got)
	}
}

func TestParseCodeBuddy_EmptyInput(t *testing.T) {
	msgs, err := ParseCodeBuddy(strings.NewReader(""))
	if err != nil {
		t.Fatalf("ParseCodeBuddy() error = %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("len(msgs) = %d, want 0", len(msgs))
	}
}
