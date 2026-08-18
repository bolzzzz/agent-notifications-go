package jsonl

import (
	"strings"
	"testing"
)

func TestParseCursor_RolesAndContent(t *testing.T) {
	input := strings.Join([]string{
		`{"role":"user","message":{"content":"fix the bug"}}`,
		`{"role":"assistant","message":{"content":[{"type":"text","text":"Sure, on it."},{"type":"tool_use","name":"Edit"}]}}`,
		`{"type":"turn_ended","status":"success"}`,
		`{"role":"assistant","message":{"content":[{"type":"text","text":"Done."}]}}`,
	}, "\n")

	msgs, err := ParseCursor(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseCursor error: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("got %d messages, want 3 (turn_ended skipped)", len(msgs))
	}

	if msgs[0].Type != "user" || msgs[0].Message.Role != "user" {
		t.Errorf("msg[0] role = %q/%q, want user", msgs[0].Type, msgs[0].Message.Role)
	}
	if msgs[0].Message.ContentString != "fix the bug" {
		t.Errorf("msg[0] ContentString = %q", msgs[0].Message.ContentString)
	}
	if msgs[1].Type != "assistant" {
		t.Errorf("msg[1] type = %q, want assistant", msgs[1].Type)
	}
	if got := msgs[1].Message.Content[0].Text; got != "Sure, on it." {
		t.Errorf("msg[1] text = %q", got)
	}
	if got := msgs[1].Message.Content[1].Type; got != "tool_use" {
		t.Errorf("msg[1] block[1] type = %q, want tool_use", got)
	}
}

func TestParseCursor_SkipsMalformedLines(t *testing.T) {
	input := strings.Join([]string{
		`not json`,
		`{"role":"assistant","message":{"content":[{"type":"text","text":"ok"}]}}`,
		``,
		`{"type":"error","message":"boom"}`,
	}, "\n")

	msgs, err := ParseCursor(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseCursor error: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("got %d messages, want 1", len(msgs))
	}
	if msgs[0].Message.Content[0].Text != "ok" {
		t.Errorf("unexpected content: %+v", msgs[0])
	}
}

func TestParseCursor_SyntheticTimestampsOrdered(t *testing.T) {
	input := strings.Join([]string{
		`{"role":"user","message":{"content":"first"}}`,
		`{"role":"assistant","message":{"content":[{"type":"text","text":"second"}]}}`,
	}, "\n")

	msgs, err := ParseCursor(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseCursor error: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("got %d messages, want 2", len(msgs))
	}
	if !(msgs[0].Timestamp < msgs[1].Timestamp) {
		t.Errorf("timestamps not ordered: %q >= %q", msgs[0].Timestamp, msgs[1].Timestamp)
	}
}

func TestLastAssistantTextFromCursorMessages(t *testing.T) {
	input := strings.Join([]string{
		`{"role":"user","message":{"content":"question?"}}`,
		`{"role":"assistant","message":{"content":[{"type":"text","text":"Which option do you prefer?"}]}}`,
	}, "\n")

	msgs, err := ParseCursor(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseCursor error: %v", err)
	}
	if got := LastAssistantTextFromCursorMessages(msgs); got != "Which option do you prefer?" {
		t.Errorf("LastAssistantTextFromCursorMessages() = %q", got)
	}

	if got := LastAssistantTextFromCursorMessages(nil); got != "" {
		t.Errorf("LastAssistantTextFromCursorMessages(nil) = %q, want empty", got)
	}
}
