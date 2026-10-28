package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDownloadAttachmentDemoReturnsContent(t *testing.T) {
	s, st := testEmailServer(t)
	if err := s.cm.Update(func(c *Config) error {
		c.Demo = true
		c.Mailboxes = []Mailbox{{ID: "mb", Name: "演示", Email: "demo@example.com", Demo: true}}
		return nil
	}); err != nil {
		t.Fatalf("config update: %v", err)
	}
	st.Upsert(&Email{
		ID: "mb:7", MailboxID: "mb", UID: 7, Subject: "Invoice",
		Attachments: []Attachment{{Index: 0, Filename: "明细.csv", ContentType: "text/csv", Size: 64}},
	})

	rec := httptest.NewRecorder()
	s.downloadAttachment(rec, httptest.NewRequest(http.MethodGet, "/api/emails/mb:7/attachments/0", nil), "mb:7", "0")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "text/csv" {
		t.Fatalf("unexpected content type: %q", got)
	}
	// 中文文件名必须以 RFC 5987 形式出现，浏览器才不会乱码
	disp := rec.Header().Get("Content-Disposition")
	if !strings.Contains(disp, "filename*=utf-8''") {
		t.Fatalf("expected an RFC 5987 filename in Content-Disposition, got %q", disp)
	}
	if rec.Body.Len() == 0 {
		t.Fatal("expected demo attachment bytes")
	}
}

func TestDownloadAttachmentRejectsBadIndexAndUnknownEmail(t *testing.T) {
	s, st := testEmailServer(t)
	if err := s.cm.Update(func(c *Config) error {
		c.Demo = true
		c.Mailboxes = []Mailbox{{ID: "mb", Name: "演示", Email: "demo@example.com", Demo: true}}
		return nil
	}); err != nil {
		t.Fatalf("config update: %v", err)
	}
	st.Upsert(&Email{
		ID: "mb:7", MailboxID: "mb", UID: 7, Subject: "Invoice",
		Attachments: []Attachment{{Index: 0, Filename: "a.pdf", ContentType: "application/pdf", Size: 8}},
	})

	cases := []struct {
		name  string
		id    string
		index string
		want  int
	}{
		{"unknown email", "mb:9", "0", http.StatusNotFound},
		{"index out of range", "mb:7", "3", http.StatusNotFound},
		{"non numeric index", "mb:7", "x", http.StatusBadRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/emails/"+c.id+"/attachments/"+c.index, nil)
			s.downloadAttachment(rec, req, c.id, c.index)
			if rec.Code != c.want {
				t.Fatalf("expected %d, got %d: %s", c.want, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestDownloadAttachmentRejectsNonGET(t *testing.T) {
	s, st := testEmailServer(t)
	st.Upsert(&Email{ID: "mb:7", MailboxID: "mb", UID: 7, Attachments: []Attachment{{Index: 0, Filename: "a.pdf"}}})

	rec := httptest.NewRecorder()
	s.downloadAttachment(rec, httptest.NewRequest(http.MethodPost, "/api/emails/mb:7/attachments/0", nil), "mb:7", "0")

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

// 没有邮箱配置时无法回源，应给出明确错误而不是空响应
func TestDownloadAttachmentWithoutMailboxFails(t *testing.T) {
	s, st := testEmailServer(t)
	st.Upsert(&Email{ID: "mb:7", MailboxID: "mb", UID: 7, Attachments: []Attachment{{Index: 0, Filename: "a.pdf"}}})

	rec := httptest.NewRecorder()
	s.downloadAttachment(rec, httptest.NewRequest(http.MethodGet, "/api/emails/mb:7/attachments/0", nil), "mb:7", "0")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

// 真实邮箱但没有授权码时，应返回可读的中文错误而不是空响应
func TestDownloadAttachmentWithoutPasswordFails(t *testing.T) {
	s, st := testEmailServer(t)
	if err := s.cm.Update(func(c *Config) error {
		c.Mailboxes = []Mailbox{{ID: "mb", Name: "工作", Email: "a@example.com"}}
		return nil
	}); err != nil {
		t.Fatalf("config update: %v", err)
	}
	st.Upsert(&Email{ID: "mb:7", MailboxID: "mb", UID: 7, Attachments: []Attachment{{Index: 0, Filename: "a.pdf"}}})

	rec := httptest.NewRecorder()
	s.downloadAttachment(rec, httptest.NewRequest(http.MethodGet, "/api/emails/mb:7/attachments/0", nil), "mb:7", "0")

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "授权码") {
		t.Fatalf("expected a password hint in the error, got %s", rec.Body.String())
	}
}
