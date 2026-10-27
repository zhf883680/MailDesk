package main

import (
	"bytes"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func validPushSubscription(t *testing.T) PushSubscription {
	t.Helper()
	x, y := elliptic.P256().ScalarBaseMult(randomBytes(t, 32))
	publicKey := elliptic.Marshal(elliptic.P256(), x, y)
	return PushSubscription{
		Endpoint: "https://push.example.com/subscription/abc",
		P256dh:   base64.RawURLEncoding.EncodeToString(publicKey),
		Auth:     base64.RawURLEncoding.EncodeToString(randomBytes(t, 16)),
	}
}

func randomBytes(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	return b
}

func TestValidatePushSubscription(t *testing.T) {
	sub := validPushSubscription(t)
	if err := validatePushSubscription(sub); err != nil {
		t.Fatalf("valid subscription rejected: %v", err)
	}

	sub.Endpoint = "http://push.example.com/subscription/abc"
	if err := validatePushSubscription(sub); err == nil {
		t.Fatal("insecure push endpoint accepted")
	}

	sub = validPushSubscription(t)
	sub.Auth = "short"
	if err := validatePushSubscription(sub); err == nil {
		t.Fatal("invalid auth key accepted")
	}
}

func TestBuildPushPayload(t *testing.T) {
	email := sampleMail()
	email.Summary = "登录验证码 482913，10 分钟内有效，请尽快完成验证。"
	payload := buildPushPayload(email)
	var message pushMessage
	if err := json.Unmarshal(payload, &message); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if message.Title != "Stripe 验证码" {
		t.Errorf("title = %q", message.Title)
	}
	if message.Body != email.Summary {
		t.Errorf("body = %q", message.Body)
	}
	if strings.Contains(message.Body, email.FromName) || strings.Contains(message.Body, "\n") {
		t.Errorf("body should contain only the concise AI summary: %q", message.Body)
	}
	if !strings.Contains(message.URL, "email=") || !strings.Contains(message.URL, "mb%3A1") {
		t.Errorf("url = %q", message.URL)
	}
	if message.Tag != "maildesk-mb:1" {
		t.Errorf("tag = %q", message.Tag)
	}
}

func TestNotifyEmailWaitsForAIInterpretation(t *testing.T) {
	dir := t.TempDir()
	cm, err := newConfigManager(dir)
	if err != nil {
		t.Fatalf("newConfigManager: %v", err)
	}
	if err := cm.Update(func(c *Config) error {
		c.AI.Enabled = true
		c.AI.APIKey = "test-key"
		return nil
	}); err != nil {
		t.Fatalf("configure AI: %v", err)
	}
	st, err := newStore(filepath.Join(dir, "emails"))
	if err != nil {
		t.Fatalf("newStore: %v", err)
	}
	email := sampleMail()
	email.UID = 2
	email.ID = makeID(email.MailboxID, email.UID)
	email.AIStatus = "running"
	st.Upsert(email)

	sent := make(chan pushMessage, 1)
	service, err := newPushServiceWithSender(cm, st, func(payload []byte, sub PushSubscription) (int, error) {
		var message pushMessage
		if err := json.Unmarshal(payload, &message); err != nil {
			return 0, err
		}
		sent <- message
		return 201, nil
	})
	if err != nil {
		t.Fatalf("newPushService: %v", err)
	}
	if err := service.Subscribe(validPushSubscription(t)); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	var sends atomic.Int32
	done := make(chan error, 1)
	go func() {
		sends.Add(1)
		done <- service.NotifyEmail(email.ID)
	}()
	select {
	case message := <-sent:
		t.Fatalf("notification sent before AI completed: %#v", message)
	case <-time.After(250 * time.Millisecond):
	}

	st.Update(email.ID, func(e *Email) {
		e.Summary = "验证码即将过期，请立即完成登录。"
	})
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("NotifyEmail: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("NotifyEmail did not resume after AI completed")
	}
	select {
	case message := <-sent:
		if message.Body != "验证码即将过期，请立即完成登录。" {
			t.Fatalf("notification body = %q", message.Body)
		}
	case <-time.After(time.Second):
		t.Fatal("notification was not sent after AI completed")
	}
	if got := sends.Load(); got != 1 {
		t.Fatalf("NotifyEmail calls = %d, want 1", got)
	}
}

func TestPushServiceSendsNewEmailOnlyOnce(t *testing.T) {
	dir := t.TempDir()
	cm, err := newConfigManager(dir)
	if err != nil {
		t.Fatalf("newConfigManager: %v", err)
	}
	st, err := newStore(filepath.Join(dir, "emails"))
	if err != nil {
		t.Fatalf("newStore: %v", err)
	}
	email := sampleMail()
	email.UID = 1
	st.Upsert(email)

	var sends int
	service, err := newPushServiceWithSender(cm, st, func(payload []byte, sub PushSubscription) (int, error) {
		sends++
		if len(payload) == 0 || sub.Endpoint == "" {
			t.Fatal("empty push request")
		}
		return 201, nil
	})
	if err != nil {
		t.Fatalf("newPushService: %v", err)
	}
	if err := service.Subscribe(validPushSubscription(t)); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if got := len(cm.Get().Notifications.Subscriptions); got != 1 {
		t.Fatalf("stored subscriptions = %d, want 1", got)
	}
	if st.Copy(email.ID) == nil {
		t.Fatalf("email %q was not stored", email.ID)
	}
	if err := service.NotifyEmail(email.ID); err != nil {
		t.Fatalf("NotifyEmail: %v", err)
	}
	if err := service.NotifyEmail(email.ID); err != nil {
		t.Fatalf("NotifyEmail second call: %v", err)
	}
	if sends != 1 {
		t.Fatalf("send count = %d, want 1", sends)
	}
	if got := st.Get(email.ID); got == nil || !got.Notified {
		t.Fatalf("email not marked notified: %#v", got)
	}
}

func TestPushLogsHideSubscriptionSecret(t *testing.T) {
	dir := t.TempDir()
	cm, err := newConfigManager(dir)
	if err != nil {
		t.Fatalf("newConfigManager: %v", err)
	}
	st, err := newStore(filepath.Join(dir, "emails"))
	if err != nil {
		t.Fatalf("newStore: %v", err)
	}
	service, err := newPushServiceWithSender(cm, st, func(payload []byte, sub PushSubscription) (int, error) {
		return 201, nil
	})
	if err != nil {
		t.Fatalf("newPushService: %v", err)
	}
	var logs bytes.Buffer
	service.logger = log.New(&logs, "", 0)

	if err := service.Subscribe(validPushSubscription(t)); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	got := logs.String()
	if !strings.Contains(got, "[push]") || !strings.Contains(got, "push.example.com") {
		t.Fatalf("push log missing useful context: %q", got)
	}
	if strings.Contains(got, "/subscription/abc") {
		t.Fatalf("push log exposed subscription secret: %q", got)
	}
}

func TestSafePushErrorRedactsSubscriptionEndpoint(t *testing.T) {
	sub := validPushSubscription(t)
	err := fmt.Errorf(`Post %q: connection refused`, sub.Endpoint)

	got := safePushError(err, sub.Endpoint)
	if strings.Contains(got, "/subscription/abc") {
		t.Fatalf("safePushError exposed subscription secret: %q", got)
	}
	if !strings.Contains(got, "push.example.com") || !strings.Contains(got, "connection refused") {
		t.Fatalf("safePushError removed useful context: %q", got)
	}
}

func TestSendTestForcesNotificationWhenAppIsVisible(t *testing.T) {
	dir := t.TempDir()
	cm, err := newConfigManager(dir)
	if err != nil {
		t.Fatalf("newConfigManager: %v", err)
	}
	st, err := newStore(filepath.Join(dir, "emails"))
	if err != nil {
		t.Fatalf("newStore: %v", err)
	}
	var sent []byte
	service, err := newPushServiceWithSender(cm, st, func(payload []byte, sub PushSubscription) (int, error) {
		sent = append([]byte(nil), payload...)
		return 201, nil
	})
	if err != nil {
		t.Fatalf("newPushService: %v", err)
	}
	if err := service.Subscribe(validPushSubscription(t)); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := service.SendTest(); err != nil {
		t.Fatalf("SendTest: %v", err)
	}

	var message pushMessage
	if err := json.Unmarshal(sent, &message); err != nil {
		t.Fatalf("decode push payload: %v", err)
	}
	if force, ok := message.Data["force"].(bool); !ok || !force {
		t.Fatalf("test notification must set data.force=true: %#v", message.Data)
	}
}

func TestPushServiceRemovesRejectedSubscription(t *testing.T) {
	dir := t.TempDir()
	cm, err := newConfigManager(dir)
	if err != nil {
		t.Fatalf("newConfigManager: %v", err)
	}
	st, err := newStore(filepath.Join(dir, "emails"))
	if err != nil {
		t.Fatalf("newStore: %v", err)
	}
	service, err := newPushServiceWithSender(cm, st, func(payload []byte, sub PushSubscription) (int, error) {
		return 403, nil
	})
	if err != nil {
		t.Fatalf("newPushService: %v", err)
	}
	var logs bytes.Buffer
	service.logger = log.New(&logs, "", 0)
	if err := service.Subscribe(validPushSubscription(t)); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	if err := service.SendTest(); err == nil {
		t.Fatal("SendTest succeeded for rejected subscription")
	}
	if got := len(cm.Get().Notifications.Subscriptions); got != 0 {
		t.Fatalf("rejected subscription was not removed: %d remain", got)
	}
	if !strings.Contains(logs.String(), "status=403") {
		t.Fatalf("push log missing rejection status: %q", logs.String())
	}
}

func TestPushRejectionDetailIsLoggedAndRemoved(t *testing.T) {
	dir := t.TempDir()
	cm, err := newConfigManager(dir)
	if err != nil {
		t.Fatalf("newConfigManager: %v", err)
	}
	st, err := newStore(filepath.Join(dir, "emails"))
	if err != nil {
		t.Fatalf("newStore: %v", err)
	}
	service, err := newPushServiceWithSender(cm, st, func(payload []byte, sub PushSubscription) (int, error) {
		return 403, errors.New("BadJwtToken")
	})
	if err != nil {
		t.Fatalf("newPushService: %v", err)
	}
	var logs bytes.Buffer
	service.logger = log.New(&logs, "", 0)
	if err := service.Subscribe(validPushSubscription(t)); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	_ = service.SendTest()
	if got := len(cm.Get().Notifications.Subscriptions); got != 0 {
		t.Fatalf("rejected subscription was not removed: %d remain", got)
	}
	if !strings.Contains(logs.String(), "BadJwtToken") {
		t.Fatalf("push rejection detail missing from log: %q", logs.String())
	}
}

func TestVAPIDSubscriberIsPublicHTTPSURL(t *testing.T) {
	if !strings.HasPrefix(vapidSubscriber, "https://") {
		t.Fatalf("vapidSubscriber must be a public HTTPS URL, got %q", vapidSubscriber)
	}
}
