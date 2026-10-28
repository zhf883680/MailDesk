package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Forwarder 负责把命中关键词的新邮件推送到 Bark
type Forwarder struct {
	cm       *ConfigManager
	st       *Store
	client   *http.Client
	queue    chan forwardJob
	inflight sync.Map
}

type forwardJob struct {
	id        string
	firstSync bool
	force     bool
}

func newForwarder(cm *ConfigManager, st *Store) *Forwarder {
	return &Forwarder{
		cm:     cm,
		st:     st,
		client: &http.Client{Timeout: 20 * time.Second},
		queue:  make(chan forwardJob, 256),
	}
}

// forwardWorkers 与 Web Push 保持一致：转发会等 AI 判完广告，
// 单线程会让排队中的验证码一起被拖慢，多开几个并行处理。
const forwardWorkers = 3

func (f *Forwarder) Start() {
	for i := 0; i < forwardWorkers; i++ {
		go func() {
			for job := range f.queue {
				f.handle(job)
				f.inflight.Delete(job.id)
			}
		}()
	}
}

func (f *Forwarder) Dispatch(id string, firstSync bool) {
	if id == "" {
		return
	}
	if _, loaded := f.inflight.LoadOrStore(id, true); loaded {
		return
	}
	select {
	case f.queue <- forwardJob{id: id, firstSync: firstSync}:
	default:
		f.inflight.Delete(id)
	}
}

func (f *Forwarder) Retry(id string) {
	if _, loaded := f.inflight.LoadOrStore(id, true); loaded {
		return
	}
	f.queue <- forwardJob{id: id, force: true}
}

func (f *Forwarder) handle(job forwardJob) {
	cfg := f.cm.Get()
	if !cfg.Forward.Enabled || strings.TrimSpace(cfg.Forward.DeviceKey) == "" {
		return
	}
	e := f.st.Copy(job.id)
	if e == nil {
		return
	}
	if !job.force && (e.Forwarded || len(e.ForwardLog) > 0) {
		return
	}
	// 首次同步不推送历史邮件，避免一次性轰炸
	if job.firstSync && time.Since(e.Date) > 30*time.Minute {
		return
	}
	rule, ok := matchForwardRule(cfg.ForwardRules, e)
	if !ok {
		return
	}
	// 同步后 AI 往往还没判完广告，直接发会把推广邮件推到手机；等分类结果再决定
	if !job.force {
		if e = f.waitForAIClassification(job.id); e == nil {
			return
		}
	}
	if f.cm.Get().IsAdLabel(e) {
		log.Printf("[forward] 跳过转发：广告默认忽略 id=%s", job.id)
		return
	}
	title, body := buildNotification(cfg, rule, e)
	err := sendBark(cfg.Forward, title, body)
	entry := ForwardLog{RuleName: rule.Name, OK: err == nil, At: time.Now()}
	if err != nil {
		entry.Error = truncate(err.Error(), 200)
	}
	f.st.Update(e.ID, func(em *Email) {
		em.ForwardLog = append(em.ForwardLog, entry)
		if err == nil {
			em.Forwarded = true
		}
	})
}

// forwardAIWait 是等待 AI 判定广告的上限。转发主要用于验证码，不能被 AI 卡太久。
const forwardAIWait = 60 * time.Second

// waitForAIClassification 等 AI 判定完广告再转发。
// 同步完成后转发队列立刻开始工作，此时 AI 分类往往还没出结果，
// 直接判断 IsAd 会把广告当成普通邮件推出去。
func (f *Forwarder) waitForAIClassification(emailID string) *Email {
	deadline := time.Now().Add(forwardAIWait)
	for {
		e := f.st.Copy(emailID)
		if e == nil {
			return nil
		}
		ai := f.cm.Get().AI
		if !ai.Enabled || !ai.Classify {
			return e
		}
		// 分类结果一落地就能判断广告，不必等翻译和自定义规则跑完
		if e.Summary != "" || e.Category != "" || e.AIError != "" {
			return e
		}
		if e.AIStatus != "pending" && e.AIStatus != "running" {
			return e
		}
		if time.Now().After(deadline) {
			log.Printf("[forward] AI 分类等待超时，按当前内容转发 id=%s", emailID)
			return e
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func matchForwardRule(rules []ForwardRule, e *Email) (ForwardRule, bool) {
	to := joinAddrs(e.To) + " " + joinAddrs(e.CC)
	for _, r := range rules {
		if !r.Enabled {
			continue
		}
		if len(r.MailboxIDs) > 0 {
			in := false
			for _, id := range r.MailboxIDs {
				if id == e.MailboxID {
					in = true
					break
				}
			}
			if !in {
				continue
			}
		}
		if r.FromContains != "" && !strings.Contains(strings.ToLower(e.FromName+" "+e.FromAddr), strings.ToLower(r.FromContains)) {
			continue
		}
		if len(r.Keywords) == 0 && r.FromContains == "" {
			continue
		}
		if len(r.Keywords) > 0 {
			hay := strings.ToLower(e.Subject + "\n" + e.Text + "\n" + e.FromName + " " + e.FromAddr + "\n" + to)
			hit := false
			for _, k := range r.Keywords {
				k = strings.ToLower(strings.TrimSpace(k))
				if k != "" && strings.Contains(hay, k) {
					hit = true
					break
				}
			}
			if !hit {
				continue
			}
		}
		return r, true
	}
	return ForwardRule{}, false
}

func buildNotification(cfg *Config, r ForwardRule, e *Email) (string, string) {
	code := extractCode(e)
	mailboxName := ""
	for _, mb := range cfg.Mailboxes {
		if mb.ID == e.MailboxID {
			mailboxName = mb.Name
			break
		}
	}
	subjectZh := e.SubjectZh
	if subjectZh == "" {
		subjectZh = e.Subject
	}
	vars := map[string]string{
		"{{subject}}":    e.Subject,
		"{{subject_zh}}": subjectZh,
		"{{from}}":       firstNonEmpty(e.FromName, e.FromAddr),
		"{{from_addr}}":  e.FromAddr,
		"{{snippet}}":    e.Snippet,
		"{{date}}":       e.Date.Format("01-02 15:04"),
		"{{mailbox}}":    mailboxName,
		"{{code}}":       code,
		"{{category}}":   e.Category,
		"{{summary}}":    e.Summary,
		"{{body}}":       truncate(e.Text, 300),
	}
	title := strings.TrimSpace(renderForwardTemplate(r.TitleTemplate, vars))
	if title == "" {
		title = subjectZh
	}
	var body string
	if strings.TrimSpace(r.BodyTemplate) != "" {
		body = renderForwardTemplate(r.BodyTemplate, vars)
	} else if code != "" {
		body = "验证码：" + code + "\n" + firstNonEmpty(e.FromName, e.FromAddr) + "\n" + truncate(e.Snippet, 100)
	} else {
		body = firstNonEmpty(e.FromName, e.FromAddr) + "\n" + truncate(e.Snippet, 160)
	}
	title = truncate(strings.TrimSpace(title), 120)
	body = strings.TrimSpace(body)
	body = truncate(body, 480)
	return title, body
}

func renderForwardTemplate(tpl string, vars map[string]string) string {
	out := tpl
	for k, v := range vars {
		out = strings.ReplaceAll(out, k, v)
	}
	// 清理模板中因变量为空留下的空行
	lines := strings.Split(out, "\n")
	kept := make([]string, 0, len(lines))
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			kept = append(kept, strings.TrimSpace(l))
		}
	}
	return strings.Join(kept, "\n")
}

var reCodeContext = regexp.MustCompile(`(?i)(?:验证码|校验码|动态码|安全码|确认码|verification code|login code|security code|one-time code|passcode|\bcode\b|\botp\b|\bpin\b)[^0-9]{0,14}(\d{4,8})`)
var reCode6 = regexp.MustCompile(`(?:^|[^0-9])(\d{6})(?:[^0-9]|$)`)

func extractCode(e *Email) string {
	text := e.Subject + "\n" + e.Text
	if m := reCodeContext.FindStringSubmatch(text); len(m) > 1 {
		return m[1]
	}
	if m := reCode6.FindStringSubmatch(text); len(m) > 1 {
		return m[1]
	}
	return ""
}

// ---------- Bark ----------

func barkEndpoint(cfg ForwardConfig) (string, string) {
	base := strings.TrimSpace(cfg.BaseURL)
	if base == "" {
		base = "https://api.day.app"
	}
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		base = "https://" + base
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		return "https://api.day.app/push", cfg.DeviceKey
	}
	key := strings.TrimSpace(cfg.DeviceKey)
	// 兼容直接粘贴 "https://api.day.app/设备码" 的写法
	segments := strings.Split(strings.Trim(u.Path, "/"), "/")
	if key == "" && len(segments) > 0 && segments[0] != "push" && segments[0] != "" {
		key = segments[0]
	}
	return u.Scheme + "://" + u.Host + "/push", key
}

func sendBark(cfg ForwardConfig, title, body string) error {
	endpoint, key := barkEndpoint(cfg)
	if key == "" {
		return errors.New("尚未填写 Bark 设备码")
	}
	payload := map[string]interface{}{
		"device_key": key,
		"title":      title,
		"body":       body,
		"group":      firstNonEmpty(cfg.Group, "MailDesk"),
		"level":      firstNonEmpty(cfg.Level, "active"),
		"isArchive":  1,
	}
	if cfg.Sound != "" {
		payload["sound"] = cfg.Sound
	}
	b, _ := json.Marshal(payload)

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(b))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
		if err != nil {
			lastErr = fmt.Errorf("请求 Bark 失败: %w", err)
			time.Sleep(600 * time.Millisecond)
			continue
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 32*1024))
		resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			lastErr = fmt.Errorf("Bark 返回 %d: %s", resp.StatusCode, truncate(string(raw), 160))
			continue
		}
		var r struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		}
		if json.Unmarshal(raw, &r) == nil && r.Code != 0 && r.Code != 200 {
			lastErr = fmt.Errorf("Bark 推送失败: %s", firstNonEmpty(r.Message, string(raw)))
			continue
		}
		return nil
	}
	if lastErr == nil {
		lastErr = errors.New("Bark 推送失败")
	}
	return lastErr
}

func (f *Forwarder) Test() error {
	cfg := f.cm.Get()
	if strings.TrimSpace(cfg.Forward.DeviceKey) == "" {
		return errors.New("请先填写 Bark 设备码（Bark App 首页可复制）")
	}
	body := "如果你看到这条通知，说明关键词转发已经配置成功。\n发送时间：" + time.Now().Format("2006-01-02 15:04:05")
	return sendBark(cfg.Forward, "MailDesk 测试通知", body)
}
