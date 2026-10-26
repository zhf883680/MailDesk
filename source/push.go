package main

import (
	"crypto/elliptic"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
)

const (
	maxPushSubscriptions = 20
	pushTTLSeconds       = 24 * 60 * 60
)

type PushKeys struct {
	P256dh string `json:"p256dh"`
	Auth   string `json:"auth"`
}

type PushSubscription struct {
	Endpoint  string    `json:"endpoint"`
	P256dh    string    `json:"p256dh"`
	Auth      string    `json:"auth"`
	UserAgent string    `json:"user_agent,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type PushConfig struct {
	Enabled         bool               `json:"enabled"`
	VAPIDPublicKey  string             `json:"vapid_public_key,omitempty"`
	VAPIDPrivateKey string             `json:"vapid_private_key,omitempty"`
	Subscriptions   []PushSubscription `json:"subscriptions,omitempty"`
}

type PushStatus struct {
	Enabled           bool   `json:"enabled"`
	PublicKey         string `json:"public_key"`
	SubscriptionCount int    `json:"subscription_count"`
}

type pushMessage struct {
	Title string         `json:"title"`
	Body  string         `json:"body"`
	URL   string         `json:"url"`
	Tag   string         `json:"tag"`
	Icon  string         `json:"icon"`
	Data  map[string]any `json:"data,omitempty"`
}

type pushSender func(payload []byte, sub PushSubscription) (status int, err error)

// PushService stores browser subscriptions and sends standard Web Push messages.
type PushService struct {
	cm       *ConfigManager
	st       *Store
	send     pushSender
	queue    chan string
	inflight sync.Map
}

func newPushService(cm *ConfigManager, st *Store) (*PushService, error) {
	return newPushServiceWithSender(cm, st, nil)
}

func newPushServiceWithSender(cm *ConfigManager, st *Store, sender pushSender) (*PushService, error) {
	if err := ensureVAPIDKeys(cm); err != nil {
		return nil, err
	}
	service := &PushService{
		cm:    cm,
		st:    st,
		queue: make(chan string, 256),
	}
	if sender != nil {
		service.send = sender
	} else {
		client := &http.Client{Timeout: 20 * time.Second}
		service.send = func(payload []byte, sub PushSubscription) (int, error) {
			cfg := cm.Get().Notifications
			resp, err := webpush.SendNotification(payload, &webpush.Subscription{
				Endpoint: sub.Endpoint,
				Keys: webpush.Keys{
					P256dh: sub.P256dh,
					Auth:   sub.Auth,
				},
			}, &webpush.Options{
				HTTPClient:      client,
				Subscriber:      "maildesk@localhost.invalid",
				TTL:             pushTTLSeconds,
				Urgency:         webpush.UrgencyNormal,
				VAPIDPublicKey:  cfg.VAPIDPublicKey,
				VAPIDPrivateKey: cfg.VAPIDPrivateKey,
			})
			if err != nil {
				return 0, err
			}
			defer resp.Body.Close()
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
			return resp.StatusCode, nil
		}
	}
	return service, nil
}

func ensureVAPIDKeys(cm *ConfigManager) error {
	cfg := cm.Get().Notifications
	if cfg.VAPIDPublicKey != "" && cfg.VAPIDPrivateKey != "" {
		return nil
	}
	privateKey, publicKey, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		return fmt.Errorf("生成 Web Push 密钥失败: %w", err)
	}
	return cm.Update(func(c *Config) error {
		if c.Notifications.VAPIDPublicKey == "" || c.Notifications.VAPIDPrivateKey == "" {
			c.Notifications.VAPIDPrivateKey = privateKey
			c.Notifications.VAPIDPublicKey = publicKey
		}
		return nil
	})
}

func (p *PushService) Start() {
	if p == nil {
		return
	}
	go func() {
		for id := range p.queue {
			_ = p.NotifyEmail(id)
			p.inflight.Delete(id)
		}
	}()
}

func (p *PushService) Enqueue(emailID string) {
	if p == nil || emailID == "" {
		return
	}
	if _, loaded := p.inflight.LoadOrStore(emailID, true); loaded {
		return
	}
	select {
	case p.queue <- emailID:
	default:
		p.inflight.Delete(emailID)
	}
}

func (p *PushService) Status() PushStatus {
	cfg := p.cm.Get().Notifications
	return PushStatus{
		Enabled:           cfg.Enabled && len(cfg.Subscriptions) > 0,
		PublicKey:         cfg.VAPIDPublicKey,
		SubscriptionCount: len(cfg.Subscriptions),
	}
}

func (p *PushService) Subscribe(sub PushSubscription) error {
	if err := validatePushSubscription(sub); err != nil {
		return err
	}
	sub.CreatedAt = time.Now()
	sub.UserAgent = truncate(sub.UserAgent, 300)
	return p.cm.Update(func(c *Config) error {
		subs := make([]PushSubscription, 0, len(c.Notifications.Subscriptions)+1)
		replaced := false
		for _, old := range c.Notifications.Subscriptions {
			if old.Endpoint == sub.Endpoint {
				subs = append(subs, sub)
				replaced = true
			} else {
				subs = append(subs, old)
			}
		}
		if !replaced {
			if len(subs) >= maxPushSubscriptions {
				return errors.New("通知设备数量已达上限")
			}
			subs = append(subs, sub)
		}
		c.Notifications.Subscriptions = subs
		c.Notifications.Enabled = true
		return nil
	})
}

func (p *PushService) Unsubscribe(endpoint string) error {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return errors.New("推送订阅地址不能为空")
	}
	return p.cm.Update(func(c *Config) error {
		subs := make([]PushSubscription, 0, len(c.Notifications.Subscriptions))
		for _, sub := range c.Notifications.Subscriptions {
			if sub.Endpoint != endpoint {
				subs = append(subs, sub)
			}
		}
		c.Notifications.Subscriptions = subs
		c.Notifications.Enabled = len(subs) > 0
		return nil
	})
}

func (p *PushService) NotifyEmail(emailID string) error {
	email := p.st.Copy(emailID)
	if email == nil || email.Notified {
		return nil
	}
	subs := p.cm.Get().Notifications.Subscriptions
	if len(subs) == 0 {
		return nil
	}
	okCount, err := p.sendToSubscriptions(buildPushPayload(email))
	if okCount > 0 {
		p.st.Update(emailID, func(e *Email) { e.Notified = true })
		return nil
	}
	if err != nil {
		return err
	}
	return errors.New("当前没有可用的通知订阅")
}

func (p *PushService) SendTest() error {
	subs := p.cm.Get().Notifications.Subscriptions
	if len(subs) == 0 {
		return errors.New("请先开启浏览器通知")
	}
	payload, _ := json.Marshal(pushMessage{
		Title: "MailDesk 测试通知",
		Body:  "Web Push 已经连接成功。",
		URL:   "/",
		Tag:   "maildesk-test",
		Icon:  "/icon-192.png",
	})
	okCount, err := p.sendToSubscriptions(payload)
	if okCount > 0 {
		return nil
	}
	if err != nil {
		return err
	}
	return errors.New("通知发送失败，请重新开启通知")
}

func (p *PushService) sendToSubscriptions(payload []byte) (int, error) {
	subs := p.cm.Get().Notifications.Subscriptions
	okCount := 0
	var lastErr error
	var expired []string
	for _, sub := range subs {
		status, err := p.send(payload, sub)
		if err != nil {
			lastErr = err
			continue
		}
		if status >= 200 && status < 300 {
			okCount++
			continue
		}
		lastErr = fmt.Errorf("推送服务返回 HTTP %d", status)
		if status == http.StatusGone || status == http.StatusNotFound {
			expired = append(expired, sub.Endpoint)
		}
	}
	if len(expired) > 0 {
		_ = p.removeSubscriptions(expired)
	}
	return okCount, lastErr
}

func (p *PushService) removeSubscriptions(endpoints []string) error {
	remove := make(map[string]bool, len(endpoints))
	for _, endpoint := range endpoints {
		remove[endpoint] = true
	}
	return p.cm.Update(func(c *Config) error {
		subs := make([]PushSubscription, 0, len(c.Notifications.Subscriptions))
		for _, sub := range c.Notifications.Subscriptions {
			if !remove[sub.Endpoint] {
				subs = append(subs, sub)
			}
		}
		c.Notifications.Subscriptions = subs
		c.Notifications.Enabled = len(subs) > 0
		return nil
	})
}

func buildPushPayload(email *Email) []byte {
	subject := firstNonEmpty(email.SubjectZh, email.Subject, "新邮件")
	from := firstNonEmpty(email.FromName, email.FromAddr, "未知发件人")
	body := from
	if email.Snippet != "" {
		body += "\n" + truncate(email.Snippet, 180)
	}
	message := pushMessage{
		Title: truncate(subject, 120),
		Body:  truncate(body, 360),
		URL:   "/?email=" + url.QueryEscape(email.ID),
		Tag:   "maildesk-" + email.ID,
		Icon:  "/icon-192.png",
		Data:  map[string]any{"email_id": email.ID},
	}
	payload, _ := json.Marshal(message)
	return payload
}

func validatePushSubscription(sub PushSubscription) error {
	endpoint := strings.TrimSpace(sub.Endpoint)
	if endpoint == "" || len(endpoint) > 4096 {
		return errors.New("推送订阅地址无效")
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return errors.New("推送订阅地址必须使用 HTTPS")
	}
	p256dh, err := decodeBase64URL(sub.P256dh)
	if err != nil || len(p256dh) != 65 || p256dh[0] != 4 {
		return errors.New("推送公钥无效")
	}
	x, y := elliptic.Unmarshal(elliptic.P256(), p256dh)
	if x == nil || y == nil {
		return errors.New("推送公钥无效")
	}
	auth, err := decodeBase64URL(sub.Auth)
	if err != nil || len(auth) != 16 {
		return errors.New("推送认证密钥无效")
	}
	return nil
}

func decodeBase64URL(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, errors.New("empty key")
	}
	if b, err := base64.RawURLEncoding.DecodeString(value); err == nil {
		return b, nil
	}
	return base64.URLEncoding.DecodeString(value)
}

func (s *Server) handlePush(w http.ResponseWriter, r *http.Request, parts []string) {
	if s.push == nil {
		fail(w, http.StatusServiceUnavailable, "Web Push 未初始化")
		return
	}
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}
	switch action {
	case "status":
		if r.Method != http.MethodGet {
			fail(w, http.StatusMethodNotAllowed, "方法不允许")
			return
		}
		ok(w, s.push.Status())
	case "subscribe":
		if r.Method != http.MethodPost {
			fail(w, http.StatusMethodNotAllowed, "方法不允许")
			return
		}
		var in struct {
			Endpoint string   `json:"endpoint"`
			Keys     PushKeys `json:"keys"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 16*1024)).Decode(&in); err != nil {
			fail(w, http.StatusBadRequest, "请求格式错误")
			return
		}
		sub := PushSubscription{
			Endpoint:  in.Endpoint,
			P256dh:    in.Keys.P256dh,
			Auth:      in.Keys.Auth,
			UserAgent: r.UserAgent(),
		}
		if err := s.push.Subscribe(sub); err != nil {
			fail(w, http.StatusBadRequest, err.Error())
			return
		}
		ok(w, s.push.Status())
	case "unsubscribe":
		if r.Method != http.MethodPost {
			fail(w, http.StatusMethodNotAllowed, "方法不允许")
			return
		}
		var in struct {
			Endpoint string `json:"endpoint"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 16*1024)).Decode(&in); err != nil {
			fail(w, http.StatusBadRequest, "请求格式错误")
			return
		}
		if err := s.push.Unsubscribe(in.Endpoint); err != nil {
			fail(w, http.StatusBadRequest, err.Error())
			return
		}
		ok(w, s.push.Status())
	case "test":
		if r.Method != http.MethodPost {
			fail(w, http.StatusMethodNotAllowed, "方法不允许")
			return
		}
		if err := s.push.SendTest(); err != nil {
			fail(w, http.StatusBadGateway, err.Error())
			return
		}
		ok(w, map[string]string{"message": "测试通知已发送"})
	default:
		fail(w, http.StatusNotFound, "接口不存在")
	}
}

func (s *Server) pushNotifyInBackground(emailID string) {
	if s.push != nil {
		s.push.Enqueue(emailID)
	}
}
