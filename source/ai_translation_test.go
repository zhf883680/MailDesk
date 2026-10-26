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

func TestTranslateNowSendsPlainTextInsteadOfMarkdown(t *testing.T) {
	var bodyPrompt string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode chat request: %v", err)
		}
		if strings.Contains(req.Messages[0].Content, "翻译") {
			if strings.Contains(req.Messages[0].Content, "Markdown") {
				bodyPrompt = req.Messages[1].Content
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"已收到订单。"}}]}`))
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
		HTML:      `<p>Hello <strong>Alex</strong>, your order is ready.</p>`,
		Markdown:  "# Markdown-only content",
	}
	st.Upsert(email)

	ai := newAIService(cm, st)
	ai.client = server.Client()
	if err := ai.translateNow(email, cm.Get().AI); err != nil {
		t.Fatalf("translateNow: %v", err)
	}

	if !strings.Contains(bodyPrompt, "Hello Alex, your order is ready.") {
		t.Fatalf("translation request did not contain visible HTML text: %q", bodyPrompt)
	}
	if strings.Contains(bodyPrompt, "Markdown-only content") {
		t.Fatalf("translation request still contained Markdown: %q", bodyPrompt)
	}
	updated := st.Get(email.ID)
	if updated == nil || updated.Translated != "已收到订单。" {
		t.Fatalf("plain translation not stored: %#v", updated)
	}
}
