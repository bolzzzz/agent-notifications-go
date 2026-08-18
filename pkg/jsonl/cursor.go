package jsonl

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"time"
)

// Cursor CLI agent transcripts (written when transcripts are enabled and the
// path is passed via transcript_path / CURSOR_TRANSCRIPT_PATH) are JSONL. Lines
// are either:
//
//	{"role":"user"|"assistant","message":{"content":[...]}}
//	{"type":"turn_ended","status":"success"|"error", ...}
//
// Unlike Claude Code JSONL there is no top-level type on message rows, no
// nested message.role, no timestamps, and no tool_result blocks. This file
// normalizes those rows into the shared Message shape so the analyzer and
// summary packages can reuse the existing state machine.
//
// Synthetic RFC3339 timestamps (ordinal seconds from a fixed epoch) preserve
// append order so FilterMessagesAfterTimestamp can still isolate the current
// turn when the last user row is located via GetLastUserTimestamp.

const cursorTimeLayout = "2006-01-02T15:04:05.000Z07:00"

// cursorEpoch is the base for synthetic timestamps. Absolute values do not
// matter; only lexicographic / chronological order between rows does.
var cursorEpoch = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

// cursorRecord is one line of a Cursor agent transcript.
type cursorRecord struct {
	Type    string          `json:"type"`
	Role    string          `json:"role"`
	Status  string          `json:"status"`
	Message json.RawMessage `json:"message"`
}

type cursorMessageBody struct {
	Content json.RawMessage `json:"content"`
}

// ParseCursorFile parses a Cursor agent transcript file into Claude-shaped
// messages.
func ParseCursorFile(path string) ([]Message, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	return ParseCursor(f)
}

// ParseCursor parses a Cursor agent transcript from a reader.
//
// Malformed lines and turn_ended markers are skipped: a transcript is
// diagnostic input, and a single unreadable line should degrade the
// notification, not suppress it.
func ParseCursor(r io.Reader) ([]Message, error) {
	var messages []Message
	ordinal := 0

	reader := bufio.NewReaderSize(r, 64*1024)
	for {
		line, err := reader.ReadBytes('\n')
		line = bytes.TrimRight(line, "\r\n")
		if len(line) > 0 {
			if msg, ok := cursorLineToMessage(line, ordinal); ok {
				messages = append(messages, msg)
				ordinal++
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

func cursorLineToMessage(line []byte, ordinal int) (Message, bool) {
	var rec cursorRecord
	if err := json.Unmarshal(line, &rec); err != nil {
		return Message{}, false
	}

	// Lifecycle markers are not messages.
	if rec.Type == "turn_ended" || rec.Type == "error" {
		return Message{}, false
	}

	role := rec.Role
	if role == "" {
		return Message{}, false
	}
	if role != "user" && role != "assistant" {
		return Message{}, false
	}

	var body cursorMessageBody
	if len(rec.Message) > 0 {
		if err := json.Unmarshal(rec.Message, &body); err != nil {
			return Message{}, false
		}
	}

	msg := Message{
		Type:      role,
		Timestamp: cursorEpoch.Add(time.Duration(ordinal) * time.Second).Format(cursorTimeLayout),
		Message: MessageContent{
			Role: role,
		},
	}

	if len(body.Content) == 0 {
		return msg, true
	}

	// Prefer array content (assistant / structured user turns).
	var blocks []Content
	if err := json.Unmarshal(body.Content, &blocks); err == nil {
		msg.Message.Content = normalizeCursorContent(blocks)
		return msg, true
	}

	var text string
	if err := json.Unmarshal(body.Content, &text); err == nil {
		msg.Message.ContentString = text
		return msg, true
	}

	return msg, true
}

func normalizeCursorContent(blocks []Content) []Content {
	out := make([]Content, 0, len(blocks))
	for _, b := range blocks {
		// Keep unknown blocks so future Cursor content types still round-trip
		// into ExtractRecentText / tooling helpers that key off Type.
		out = append(out, b)
	}
	return out
}

// LastAssistantTextFromCursorMessages returns the concatenated text of the
// last assistant message, or "" when none exists. Used as a payload fallback
// when the analyzer path is unavailable.
func LastAssistantTextFromCursorMessages(messages []Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Type != "assistant" {
			continue
		}
		var parts []byte
		for _, c := range messages[i].Message.Content {
			if c.Type == "text" && c.Text != "" {
				if len(parts) > 0 {
					parts = append(parts, '\n')
				}
				parts = append(parts, c.Text...)
			}
		}
		if len(parts) > 0 {
			return string(parts)
		}
		if messages[i].Message.ContentString != "" {
			return messages[i].Message.ContentString
		}
	}
	return ""
}
