package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func sampleMail() *Email {
	return &Email{
		ID: "mb:1", MailboxID: "mb", Subject: "Your Stripe verification code",
		FromName: "Stripe", FromAddr: "no-reply@stripe.com",
		Date: time.Now(), Snippet: "Your verification code is 482913. It expires in 10 minutes.",
		Text:      "Hi Alex,\n\nYour verification code is 482913.\n\n— Stripe",
		SubjectZh: "Stripe 验证码",
	}
}

func TestExtractCode(t *testing.T) {
	cases := []struct{ text, want string }{
		{"Your verification code is 482913.", "482913"},
		{"您的验证码是 730512，5 分钟内有效", "730512"},
		{"安全码：9281（请勿泄露）", "9281"},
		{"Your order 2026 shipped on 2026-10-23", ""},
	}
	for _, c := range cases {
		e := &Email{Subject: "", Text: c.text}
		if got := extractCode(e); got != c.want {
			t.Errorf("extractCode(%q) = %q, want %q", c.text, got, c.want)
		}
	}
}

func TestBuildNotification(t *testing.T) {
	cfg := &Config{Mailboxes: []Mailbox{{ID: "mb", Name: "个人邮箱"}}}
	e := sampleMail()
	r := ForwardRule{Name: "验证码", TitleTemplate: "{{subject_zh}}", BodyTemplate: "{{code}}\n来自 {{from}} · {{mailbox}}"}
	title, body := buildNotification(cfg, r, e)
	if title != "Stripe 验证码" {
		t.Errorf("title = %q", title)
	}
	if !strings.Contains(body, "482913") || !strings.Contains(body, "Stripe") || !strings.Contains(body, "个人邮箱") {
		t.Errorf("body = %q", body)
	}
	// 默认正文：验证码优先
	_, body2 := buildNotification(cfg, ForwardRule{Name: "x"}, e)
	if !strings.HasPrefix(body2, "验证码：482913") {
		t.Errorf("default body = %q", body2)
	}
}

func TestMatchForwardRule(t *testing.T) {
	e := sampleMail()
	rules := []ForwardRule{
		{ID: "1", Name: "off", Enabled: false, Keywords: []string{"verification"}},
		{ID: "2", Name: "other", Enabled: true, Keywords: []string{"invoice"}},
		{ID: "3", Name: "hit", Enabled: true, Keywords: []string{"验证码", "verification code"}},
	}
	r, ok := matchForwardRule(rules, e)
	if !ok || r.ID != "3" {
		t.Fatalf("expected rule 3, got %v %v", ok, r.ID)
	}
	// 限定邮箱时不匹配
	r2 := []ForwardRule{{ID: "4", Name: "scope", Enabled: true, Keywords: []string{"stripe"}, MailboxIDs: []string{"other"}}}
	if _, ok := matchForwardRule(r2, e); ok {
		t.Errorf("mailbox scope should not match")
	}
}

func TestSendBark(t *testing.T) {
	var got map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/push" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"message":"success"}`))
	}))
	defer srv.Close()

	cfg := ForwardConfig{BaseURL: srv.URL, DeviceKey: "abc123", Group: "MailDesk", Level: "active"}
	if err := sendBark(cfg, "标题", "正文"); err != nil {
		t.Fatalf("sendBark: %v", err)
	}
	if got["device_key"] != "abc123" || got["title"] != "标题" || got["body"] != "正文" {
		t.Errorf("payload = %v", got)
	}
	if got["group"] != "MailDesk" {
		t.Errorf("group = %v", got["group"])
	}
}

func TestBarkEndpointWithKeyInURL(t *testing.T) {
	ep, key := barkEndpoint(ForwardConfig{BaseURL: "https://api.day.app/XXXXkey/"})
	if ep != "https://api.day.app/push" || key != "XXXXkey" {
		t.Errorf("endpoint=%s key=%s", ep, key)
	}
}

func TestSendBarkError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_, _ = w.Write([]byte(`{"code":500,"message":"bad key"}`))
	}))
	defer srv.Close()
	cfg := ForwardConfig{BaseURL: srv.URL, DeviceKey: "abc123"}
	if err := sendBark(cfg, "t", "b"); err == nil {
		t.Fatal("expected error")
	}
}
