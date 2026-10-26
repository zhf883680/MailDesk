package main

import (
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
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
	payload := buildPushPayload(email)
	var message pushMessage
	if err := json.Unmarshal(payload, &message); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if message.Title != "Stripe 验证码" {
		t.Errorf("title = %q", message.Title)
	}
	if !strings.Contains(message.Body, "Stripe") || !strings.Contains(message.Body, "482913") {
		t.Errorf("body = %q", message.Body)
	}
	if !strings.Contains(message.URL, "email=") || !strings.Contains(message.URL, "mb%3A1") {
		t.Errorf("url = %q", message.URL)
	}
	if message.Tag != "maildesk-mb:1" {
		t.Errorf("tag = %q", message.Tag)
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
