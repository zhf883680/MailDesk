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

func TestApplyRawBodyKeepsHTMLEmailAfterPartWithUnknownCharset(t *testing.T) {
	raw := []byte(strings.Join([]string{
		"From: Sender <sender@example.com>",
		"Subject: Invoice",
		"MIME-Version: 1.0",
		"Content-Type: multipart/mixed; boundary=OUT",
		"",
		"--OUT",
		"Content-Type: text/plain; charset=x-unknown-charset",
		"",
		"正文",
		"--OUT",
		"Content-Type: text/html; charset=utf-8",
		"",
		"<html><body><p>发票内容</p></body></html>",
		"--OUT--",
		"",
	}, "\r\n"))
	email := &Email{}

	applyRawBody(email, raw)

	if !strings.Contains(email.HTML, "发票内容") {
		t.Fatalf("unknown charset in an earlier part dropped the HTML body: html=%q text=%q", email.HTML, email.Text)
	}
}

func TestApplyRawBodyKeepsHTMLEmailAfterPartWithUnknownEncoding(t *testing.T) {
	raw := []byte(strings.Join([]string{
		"From: Sender <sender@example.com>",
		"Subject: Invoice",
		"MIME-Version: 1.0",
		"Content-Type: multipart/mixed; boundary=OUT",
		"",
		"--OUT",
		"Content-Type: application/octet-stream",
		"Content-Transfer-Encoding: x-uuencode",
		"",
		"begin 644 data.bin",
		"--OUT",
		"Content-Type: text/html; charset=utf-8",
		"",
		"<html><body><p>发票内容</p></body></html>",
		"--OUT--",
		"",
	}, "\r\n"))
	email := &Email{}

	applyRawBody(email, raw)

	if !strings.Contains(email.HTML, "发票内容") {
		t.Fatalf("unknown transfer encoding in an earlier part dropped the HTML body: html=%q", email.HTML)
	}
}

func TestApplyRawBodyKeepsBodyWhenTopLevelCharsetIsUnknown(t *testing.T) {
	raw := []byte(strings.Join([]string{
		"From: Sender <sender@example.com>",
		"Subject: Invoice",
		"MIME-Version: 1.0",
		"Content-Type: text/html; charset=x-unknown-charset",
		"",
		"<html><body><p>发票内容</p></body></html>",
	}, "\r\n"))
	email := &Email{}

	applyRawBody(email, raw)

	if !strings.Contains(email.HTML, "发票内容") {
		t.Fatalf("unknown top-level charset fell back to raw MIME: html=%q text=%q", email.HTML, email.Text)
	}
}

func TestApplyRawBodyFallsBackToHTMLPartMarkedAsAttachment(t *testing.T) {
	// 部分发件人把唯一的 HTML 正文声明为 attachment，此时仍需展示正文
	raw := []byte(strings.Join([]string{
		"From: Sender <sender@example.com>",
		"Subject: Invoice",
		"MIME-Version: 1.0",
		"Content-Type: multipart/mixed; boundary=OUT",
		"",
		"--OUT",
		"Content-Type: text/html; charset=utf-8",
		"Content-Disposition: attachment; filename=invoice.html",
		"",
		"<html><body><p>发票内容</p></body></html>",
		"--OUT",
		"Content-Type: application/pdf; name=a.pdf",
		"Content-Disposition: attachment; filename=a.pdf",
		"Content-Transfer-Encoding: base64",
		"",
		"JVBERi0=",
		"--OUT--",
		"",
	}, "\r\n"))
	email := &Email{}

	applyRawBody(email, raw)

	if !strings.Contains(email.HTML, "发票内容") {
		t.Fatalf("HTML body declared as attachment was dropped: html=%q", email.HTML)
	}
}
