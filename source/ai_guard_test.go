package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 网关会丢弃 system 消息，因此任务说明必须同时出现在 user 消息里。
func TestChatInlinesInstructionIntoUserMessage(t *testing.T) {
	var got chatRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer server.Close()

	a := &AIService{client: server.Client()}
	if _, err := a.chat(AIConfig{APIKey: "k", Model: "m", BaseURL: server.URL}, "只输出译文本身", "Hello", 0); err != nil {
		t.Fatalf("chat: %v", err)
	}
	if len(got.Messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(got.Messages))
	}
	user := got.Messages[1].Content
	if !strings.Contains(user, "只输出译文本身") || !strings.Contains(user, "Hello") {
		t.Fatalf("user message does not carry the task instruction: %q", user)
	}
}

func TestParseClassifyJSONToleratesProseAndFences(t *testing.T) {
	cases := []string{
		"```json\n{\"summary\":\"账户密码已设置\",\"importance\":\"high\",\"need_action\":true,\"is_ad\":false,\"category\":\"通知\"}\n```",
		"好的，结果如下：\n{\"summary\":\"账户密码已设置\",\"importance\":\"high\",\"need_action\":true,\"is_ad\":false,\"category\":\"通知\"}\n希望有帮助。",
	}
	for _, raw := range cases {
		res, err := parseClassifyJSON(raw)
		if err != nil {
			t.Fatalf("parseClassifyJSON(%q): %v", raw, err)
		}
		if res.Summary != "账户密码已设置" || !res.NeedAction || res.Importance != "high" {
			t.Fatalf("unexpected result: %+v", res)
		}
	}

	if _, err := parseClassifyJSON("这封邮件看起来是账号安全通知。"); err == nil {
		t.Fatal("plain prose should not parse as JSON")
	}
}

func TestSanitizeRejectsChatterAndKeepsRealTitles(t *testing.T) {
	bad := []string{
		"Noted — sign-in password set on **2026-11-24 at 13:24:27 CST**.",
		"If this is a RackNerd login alert you weren't expecting, treat it as a possible compromise attempt.",
		"这封邮件看起来是通知。\n\n建议你立即修改密码。",
	}
	for _, s := range bad {
		if got := sanitizeSubjectZh(s); got != "" {
			t.Fatalf("sanitizeSubjectZh(%q) = %q, want empty", s, got)
		}
	}

	good := []string{"登录密码已设置 2026-11-24 13:24:27(CST)", "Stripe 验证码", "广发信用卡电子账单"}
	for _, s := range good {
		if got := sanitizeSubjectZh(s); got != s {
			t.Fatalf("sanitizeSubjectZh(%q) = %q", s, got)
		}
	}
}

func TestLooksLikeChatterDetectsAssistantReplies(t *testing.T) {
	bad := "这封邮件看起来是 VMRack 的通知。仅从你贴出的 HTML 看，链接都指向 vmrack.net。"
	if !looksLikeChatter(bad) {
		t.Fatal("assistant reply should be detected as chatter")
	}
	good := "VMRack\n\n尊敬的用户：您的账户已设置登录密码。如果这不是您本人的操作，请联系我们。"
	if looksLikeChatter(good) {
		t.Fatal("normal translation should not be treated as chatter")
	}
}
