package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
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

// forwardTestSetup 准备一个启用转发、只等推送外发的环境。
func forwardTestSetup(t *testing.T, handler http.HandlerFunc) (*Forwarder, *ConfigManager, *Store) {
	t.Helper()
	dir := t.TempDir()
	cm, err := newConfigManager(dir)
	if err != nil {
		t.Fatalf("newConfigManager: %v", err)
	}
	st, err := newStore(filepath.Join(dir, "emails"))
	if err != nil {
		t.Fatalf("newStore: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if handler != nil {
			handler(w, r)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"message":"success"}`))
	}))
	t.Cleanup(srv.Close)
	if err := cm.Update(func(c *Config) error {
		c.Forward = ForwardConfig{Enabled: true, BaseURL: srv.URL, DeviceKey: "abc123"}
		c.ForwardRules = []ForwardRule{{
			ID: "r1", Name: "验证码", Enabled: true,
			Keywords: []string{"verification code"},
		}}
		return nil
	}); err != nil {
		t.Fatalf("configure forward: %v", err)
	}
	return newForwarder(cm, st), cm, st
}

// 广告邮件命中关键词也不应转发到手机，与 Web Push 的行为保持一致。
func TestForwardSkipsAds(t *testing.T) {
	var sends int32
	f, _, st := forwardTestSetup(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&sends, 1)
	})
	email := sampleMail()
	email.UID = 5
	email.ID = makeID(email.MailboxID, email.UID)
	email.IsAd = true
	st.Upsert(email)

	f.handle(forwardJob{id: email.ID})

	if got := atomic.LoadInt32(&sends); got != 0 {
		t.Fatalf("bark sends = %d, want 0 for an ad", got)
	}
	if e := st.Get(email.ID); e == nil || e.Forwarded || len(e.ForwardLog) > 0 {
		t.Fatalf("ad email should not be marked forwarded: %#v", e)
	}
}

// 新邮件在同步后立即进入转发队列，此时 AI 还没判广告；应等分类结果再决定是否推送。
func TestForwardWaitsForAIClassification(t *testing.T) {
	var sends int32
	f, cm, st := forwardTestSetup(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&sends, 1)
	})
	if err := cm.Update(func(c *Config) error {
		c.AI.Enabled = true
		c.AI.Classify = true
		c.AI.APIKey = "test-key"
		return nil
	}); err != nil {
		t.Fatalf("configure AI: %v", err)
	}
	email := sampleMail()
	email.UID = 6
	email.ID = makeID(email.MailboxID, email.UID)
	email.AIStatus = "running"
	st.Upsert(email)

	done := make(chan struct{})
	go func() {
		f.handle(forwardJob{id: email.ID})
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("forwarding finished before AI classified the email")
	case <-time.After(300 * time.Millisecond):
	}
	if got := atomic.LoadInt32(&sends); got != 0 {
		t.Fatalf("bark sends = %d, want 0 while AI is still running", got)
	}

	// AI 判定为广告 → 不转发
	st.Update(email.ID, func(e *Email) {
		e.IsAd = true
		e.AIStatus = "done"
	})
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("forwarding did not resume after AI finished")
	}
	if got := atomic.LoadInt32(&sends); got != 0 {
		t.Fatalf("bark sends = %d, want 0 for an ad", got)
	}
}

// 非广告邮件仍然正常转发，避免广告过滤把正常通知一起挡掉。
func TestForwardSendsNonAd(t *testing.T) {
	var sends int32
	f, cm, st := forwardTestSetup(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&sends, 1)
	})
	if err := cm.Update(func(c *Config) error {
		c.AI.Enabled = true
		c.AI.Classify = true
		c.AI.APIKey = "test-key"
		return nil
	}); err != nil {
		t.Fatalf("configure AI: %v", err)
	}
	email := sampleMail()
	email.UID = 7
	email.ID = makeID(email.MailboxID, email.UID)
	email.AIStatus = "done"
	st.Upsert(email)

	f.handle(forwardJob{id: email.ID})

	if got := atomic.LoadInt32(&sends); got != 1 {
		t.Fatalf("bark sends = %d, want 1", got)
	}
	if e := st.Get(email.ID); e == nil || !e.Forwarded {
		t.Fatalf("email not marked forwarded: %#v", e)
	}
}

// 用户手动把误判的广告标为「不是广告」后，转发应照常进行。
func TestForwardSendsAdOverrideNotAd(t *testing.T) {
	var sends int32
	f, _, st := forwardTestSetup(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&sends, 1)
	})
	email := sampleMail()
	email.UID = 8
	email.ID = makeID(email.MailboxID, email.UID)
	email.AIStatus = "done"
	email.IsAd = true
	email.AdOverride = "not_ad"
	st.Upsert(email)

	f.handle(forwardJob{id: email.ID})

	if got := atomic.LoadInt32(&sends); got != 1 {
		t.Fatalf("bark sends = %d, want 1 for a manually corrected email", got)
	}
}

// 手动重试也不能把广告推到手机；要转发请先把邮件标为「不是广告」。
func TestForwardRetryStillSkipsAds(t *testing.T) {
	var sends int32
	f, _, st := forwardTestSetup(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&sends, 1)
	})
	email := sampleMail()
	email.UID = 9
	email.ID = makeID(email.MailboxID, email.UID)
	email.AIStatus = "done"
	email.IsAd = true
	st.Upsert(email)

	f.handle(forwardJob{id: email.ID, force: true})

	if got := atomic.LoadInt32(&sends); got != 0 {
		t.Fatalf("bark sends = %d, want 0 for an ad on manual retry", got)
	}
}
