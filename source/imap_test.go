package main

import (
	"strings"
	"testing"
)

func TestApplyRawBodyDoesNotGenerateMarkdown(t *testing.T) {
	raw := []byte(strings.Join([]string{
		"From: Sender <sender@example.com>",
		"To: Alex <alex@example.com>",
		"Subject: HTML only",
		"MIME-Version: 1.0",
		"Content-Type: text/html; charset=utf-8",
		"",
		`<html><body><h1>Hello</h1><p>Plain <strong>HTML</strong> email.</p></body></html>`,
	}, "\r\n"))
	email := &Email{}

	applyRawBody(email, raw)

	if email.Markdown != "" {
		t.Fatalf("applyRawBody generated Markdown: %q", email.Markdown)
	}
	if !strings.Contains(email.Text, "Hello") || !strings.Contains(email.Text, "Plain HTML email.") {
		t.Fatalf("applyRawBody did not extract visible text: %q", email.Text)
	}
}
