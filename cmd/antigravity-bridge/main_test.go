package main

import (
	"os"
	"strings"
	"testing"
)

func TestFirstPromptLine(t *testing.T) {
	cases := []struct {
		name   string
		prompt string
		want   string
	}{
		{"leading blank lines", "\n\n  Mawar2/Kaimi#47: Add README comment\n\nYou are an agent...", "Mawar2/Kaimi#47: Add README comment"},
		{"first line used", "feature/issue-12 fix\nmore text", "feature/issue-12 fix"},
		{"empty prompt", "   \n\t\n", "(untitled)"},
		{"caps length at 100", strings.Repeat("x", 250), strings.Repeat("x", 100)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := firstPromptLine(c.prompt); got != c.want {
				t.Errorf("firstPromptLine() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestRecordConversation(t *testing.T) {
	idx := t.TempDir() + "/conversation_index.log"
	t.Setenv("ANTIGRAVITY_CONVERSATION_INDEX", idx)

	recordConversation("Mawar2/Kaimi#47: Add README comment", "flash", "conv-abc-123")
	recordConversation("Mawar2/Kaimi PR#12 fix", "pro", "conv-def-456")

	data, err := os.ReadFile(idx)
	if err != nil {
		t.Fatalf("index file not written: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 index lines, got %d: %q", len(lines), string(data))
	}
	// Each line: timestamp \t label \t model \t conversationId
	first := strings.Split(lines[0], "\t")
	if len(first) != 4 {
		t.Fatalf("expected 4 tab-separated fields, got %d: %q", len(first), lines[0])
	}
	if first[1] != "Mawar2/Kaimi#47: Add README comment" || first[2] != "flash" || first[3] != "conv-abc-123" {
		t.Errorf("unexpected index fields: %#v", first)
	}
}
