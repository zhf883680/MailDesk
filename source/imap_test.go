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

// 带两个附件的 multipart 样例：正文 HTML + 内联图片 + PDF + CSV
func attachmentFixture() []byte {
	return []byte(strings.Join([]string{
		"From: Billing <billing@example.com>",
		"Subject: Invoice",
		"MIME-Version: 1.0",
		"Content-Type: multipart/mixed; boundary=OUT",
		"",
		"--OUT",
		"Content-Type: text/html; charset=utf-8",
		"",
		"<html><body><p>发票内容</p></body></html>",
		"--OUT",
		"Content-Type: image/png; name=logo.png",
		"Content-Disposition: inline; filename=logo.png",
		"Content-Transfer-Encoding: base64",
		"",
		"iVBORw0KGgo=",
		"--OUT",
		"Content-Type: application/pdf; name=INV-1.pdf",
		"Content-Disposition: attachment; filename=\"INV-1.pdf\"",
		"Content-Transfer-Encoding: base64",
		"",
		"JVBERi0xLjQK",
		"--OUT",
		"Content-Type: text/csv; charset=utf-8",
		"Content-Disposition: attachment; filename*=UTF-8''%E6%98%8E%E7%BB%86.csv",
		"",
		"a,b\r\n1,2\r\n",
		"--OUT--",
		"",
	}, "\r\n"))
}

func TestApplyRawBodyCollectsAttachmentMetadata(t *testing.T) {
	email := &Email{}

	applyRawBody(email, attachmentFixture())

	if len(email.Attachments) != 2 {
		t.Fatalf("expected 2 attachments (inline image excluded), got %d: %+v", len(email.Attachments), email.Attachments)
	}
	pdf, csv := email.Attachments[0], email.Attachments[1]
	if pdf.Index != 0 || pdf.Filename != "INV-1.pdf" || pdf.ContentType != "application/pdf" {
		t.Fatalf("unexpected pdf metadata: %+v", pdf)
	}
	// base64 "JVBERi0xLjQK" 解码后是 9 字节
	if pdf.Size != 9 {
		t.Fatalf("expected decoded pdf size 9, got %d", pdf.Size)
	}
	if csv.Index != 1 || csv.Filename != "明细.csv" || csv.ContentType != "text/csv" {
		t.Fatalf("unexpected csv metadata: %+v", csv)
	}
	if strings.Contains(email.Text, "JVBERi0") || strings.Contains(email.HTML, "JVBERi0") {
		t.Fatalf("pdf bytes leaked into body: text=%q html=%q", email.Text, email.HTML)
	}
	if !strings.Contains(email.HTML, "发票内容") {
		t.Fatalf("html body missing: %q", email.HTML)
	}
}

func TestExtractAttachmentPart(t *testing.T) {
	raw := attachmentFixture()

	data, err := extractAttachmentPart(raw, 1)
	if err != nil {
		t.Fatalf("extractAttachmentPart: %v", err)
	}
	if data.Filename != "明细.csv" || data.MimeType != "text/csv" {
		t.Fatalf("unexpected attachment: %+v", data)
	}
	// 附件字节按原样返回，未做换行归一化
	if string(data.Bytes) != "a,b\r\n1,2\r\n" {
		t.Fatalf("unexpected csv bytes: %q", string(data.Bytes))
	}

	if _, err := extractAttachmentPart(raw, 9); err == nil {
		t.Fatal("expected an error for an out-of-range index")
	}
}

func TestSanitizeFilename(t *testing.T) {
	cases := map[string]string{
		"../../etc/passwd": "....etcpasswd",
		`a"b.pdf`:          "ab.pdf",
		"normal.pdf":       "normal.pdf",
		"":                 "",
		"..":               "",
		"  spaced.txt  ":   "spaced.txt",
	}
	for in, want := range cases {
		if got := sanitizeFilename(in); got != want {
			t.Fatalf("sanitizeFilename(%q) = %q, want %q", in, got, want)
		}
	}
}
