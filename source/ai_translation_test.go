package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestTranslationSourceUsesVisibleHTMLText(t *testing.T) {
	email := &Email{
		Subject:  "Order update",
		Text:     "fallback plain text",
		HTML:     `<style>.hidden{display:none}</style><div hidden>Secret preview</div><div>Hello <strong>Alex</strong></div><p>Order <a href="https://example.com/42">123</a> is ready.</p>`,
		Markdown: "# Heading that must not be sent",
	}

	got := translationSource(email)
	if !strings.Contains(got, "Hello Alex") || !strings.Contains(got, "Order 123 is ready.") {
		t.Fatalf("translationSource() = %q, want visible HTML text", got)
	}
	if strings.Contains(got, "Heading that must not be sent") || strings.Contains(got, "fallback plain text") {
		t.Fatalf("translationSource() used non-HTML content: %q", got)
	}
	if strings.Contains(got, "Secret preview") || strings.Contains(got, "display:none") {
		t.Fatalf("translationSource() included hidden content: %q", got)
	}
}

func TestTranslateNowPreservesHTMLFormatting(t *testing.T) {
	var bodyPrompt string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode chat request: %v", err)
		}
		if strings.Contains(req.Messages[0].Content, "HTML") {
			bodyPrompt = req.Messages[1].Content
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"<table><tr><td>项目</td><td>金额</td></tr><tr><td>订单</td><td><strong>已就绪</strong></td></tr></table><p>你好 <strong>Alex</strong>。</p>"}}]}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"订单更新"}}]}`))
	}))
	defer server.Close()

	dir := t.TempDir()
	cm, err := newConfigManager(dir)
	if err != nil {
		t.Fatalf("newConfigManager: %v", err)
	}
	if err := cm.Update(func(c *Config) error {
		c.AI.Enabled = true
		c.AI.APIKey = "test-key"
		c.AI.BaseURL = server.URL
		c.AI.Model = "test-model"
		return nil
	}); err != nil {
		t.Fatalf("configure AI: %v", err)
	}
	st, err := newStore(filepath.Join(dir, "emails"))
	if err != nil {
		t.Fatalf("newStore: %v", err)
	}
	email := &Email{
		ID:        "mb:1",
		MailboxID: "mb",
		UID:       1,
		Subject:   "Order update",
		Text:      "fallback plain text",
		HTML:      `<table><tr><td>Item</td><td>Amount</td></tr><tr><td>Order</td><td><strong>Ready</strong></td></tr></table><p>Hello <strong>Alex</strong>.</p>`,
		Markdown:  "# Markdown-only content",
	}
	st.Upsert(email)

	ai := newAIService(cm, st)
	ai.client = server.Client()
	if err := ai.translateNow(email, cm.Get().AI); err != nil {
		t.Fatalf("translateNow: %v", err)
	}

	if !strings.Contains(bodyPrompt, `<table>`) || !strings.Contains(bodyPrompt, `<strong>Ready</strong>`) {
		t.Fatalf("translation request did not contain the original HTML structure: %q", bodyPrompt)
	}
	if strings.Contains(bodyPrompt, "Markdown-only content") {
		t.Fatalf("translation request still contained Markdown: %q", bodyPrompt)
	}
	updated := st.Get(email.ID)
	if updated == nil || !strings.Contains(updated.TranslatedHTML, `<table>`) || !strings.Contains(updated.TranslatedHTML, `<strong>Alex</strong>`) {
		t.Fatalf("formatted translation not stored: %#v", updated)
	}
	if updated.Translated == "" || updated.SubjectZh != "订单更新" {
		t.Fatalf("derived text or translated subject missing: %#v", updated)
	}
}

func TestTranslateEmailUpgradesLegacyPlainTranslation(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode chat request: %v", err)
		}
		requests++
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(req.Messages[0].Content, "HTML") {
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"<p>保留 <strong>格式</strong> 的译文。</p>"}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"格式测试"}}]}`))
	}))
	defer server.Close()

	dir := t.TempDir()
	cm, err := newConfigManager(dir)
	if err != nil {
		t.Fatalf("newConfigManager: %v", err)
	}
	if err := cm.Update(func(c *Config) error {
		c.AI.Enabled = true
		c.AI.APIKey = "test-key"
		c.AI.BaseURL = server.URL
		c.AI.Model = "test-model"
		return nil
	}); err != nil {
		t.Fatalf("configure AI: %v", err)
	}
	st, err := newStore(filepath.Join(dir, "emails"))
	if err != nil {
		t.Fatalf("newStore: %v", err)
	}
	email := &Email{
		ID:         "mb:2",
		MailboxID:  "mb",
		UID:        2,
		Subject:    "Please preserve the original email formatting",
		HTML:       `<p>Keep the original <strong>formatting</strong> in this message.</p>`,
		Translated: "旧的纯文本译文",
		AIStatus:   "done",
	}
	st.Upsert(email)

	ai := newAIService(cm, st)
	ai.client = server.Client()
	updated, err := ai.TranslateEmail(email.ID)
	if err != nil {
		t.Fatalf("TranslateEmail: %v", err)
	}
	if requests < 2 {
		t.Fatalf("legacy translation skipped the AI upgrade: requests=%d", requests)
	}
	if updated == nil || !strings.Contains(updated.TranslatedHTML, `<strong>格式</strong>`) {
		t.Fatalf("legacy translation was not upgraded: %#v", updated)
	}
}
