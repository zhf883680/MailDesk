package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode"
)

type AIService struct {
	cm         *ConfigManager
	st         *Store
	queue      chan string
	running    sync.Map
	client     *http.Client
	digestMu   sync.Mutex
	digestAt   time.Time
	digestText string
}

func newAIService(cm *ConfigManager, st *Store) *AIService {
	return &AIService{
		cm:     cm,
		st:     st,
		queue:  make(chan string, 512),
		client: &http.Client{Timeout: 150 * time.Second},
	}
}

func (a *AIService) Start() {
	for i := 0; i < 3; i++ {
		go func() {
			for id := range a.queue {
				a.process(id)
				a.running.Delete(id)
			}
		}()
	}
	go func() {
		time.Sleep(2 * time.Second)
		for _, e := range a.st.PendingEmails() {
			a.Enqueue(e.ID)
		}
	}()
}

func (a *AIService) Enqueue(id string) {
	if id == "" {
		return
	}
	if _, loaded := a.running.LoadOrStore(id, true); loaded {
		return
	}
	select {
	case a.queue <- id:
	default:
		a.running.Delete(id)
	}
}

// ---------- OpenAI 兼容客户端 ----------

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
	Stream      bool          `json:"stream"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func endpointOf(base string) string {
	b := strings.TrimSpace(base)
	if b == "" {
		b = "https://api.openai.com/v1"
	}
	b = strings.TrimRight(b, "/")
	if strings.HasSuffix(b, "/chat/completions") {
		return b
	}
	return b + "/chat/completions"
}

func (a *AIService) chat(cfg AIConfig, system, user string, temperature float64) (string, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return "", errors.New("尚未配置 AI API Key")
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return "", errors.New("尚未配置 AI 模型名称")
	}
	body, _ := json.Marshal(chatRequest{
		Model: cfg.Model,
		Messages: []chatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		Temperature: temperature,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpointOf(cfg.BaseURL), bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	resp, err := a.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求 AI 接口失败: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("AI 接口返回 %d: %s", resp.StatusCode, truncate(string(raw), 300))
	}
	var cr chatResponse
	if err := json.Unmarshal(raw, &cr); err != nil {
		return "", fmt.Errorf("AI 返回格式异常: %s", truncate(string(raw), 200))
	}
	if cr.Error != nil && cr.Error.Message != "" {
		return "", errors.New(cr.Error.Message)
	}
	if len(cr.Choices) == 0 {
		return "", errors.New("AI 未返回内容")
	}
	return strings.TrimSpace(cr.Choices[0].Message.Content), nil
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func (a *AIService) Test(cfg AIConfig) error {
	_, err := a.chat(cfg, "你是一个连通性测试助手。", "请只回复：OK", 0)
	return err
}

func (a *AIService) Polish(text, instruction string) (string, error) {
	cfg := a.cm.Get().AI
	if strings.TrimSpace(text) == "" {
		return "", errors.New("请先输入正文")
	}
	sys := "你是专业的中文商务邮件写作助手。请在保留原意的前提下润色用户提供的邮件正文：语句通顺、结构清晰、语气得体。只输出润色后的正文，不要解释。"
	if strings.TrimSpace(instruction) != "" {
		sys += "额外要求：" + instruction
	}
	return a.chat(cfg, sys, truncate(text, 4000), 0.5)
}

// ---------- 邮件处理流水线 ----------

const translateSystem = `你是一名专业的邮件翻译。把用户给出的英文邮件翻译成自然、准确、通顺的简体中文。
要求：
1. 保留段落结构、列表、数字、链接与专有名词；
2. 不要翻译人名、公司名、产品名（可保留英文）；
3. 不要添加任何解释、注释或前后缀。
输出格式必须严格为：
###SUBJECT###
（主题的中文译文，单行）
###BODY###
（正文的中文译文）`

const classifySystem = `你是邮件智能助理。请分析用户给出的邮件，并只输出一个 JSON 对象，不要包含 markdown 代码块或任何解释。
JSON 字段：
- "summary": 一句话中文摘要，不超过 40 字，说明这封邮件要做什么
- "importance": "high" / "medium" / "low" 三选一（high=需要尽快亲自处理的重要事务；medium=需要知晓或稍后处理；low=可忽略）
- "need_action": 布尔值，是否需要收件人回复或执行操作
- "is_ad": 布尔值，是否营销广告/推广/垃圾邮件
- "category": "工作" / "账单" / "通知" / "验证码" / "社交" / "营销" / "其他" 之一`

func (a *AIService) process(id string) {
	email := a.st.Copy(id)
	if email == nil {
		return
	}
	cfg := a.cm.Get().AI
	if !cfg.Enabled || strings.TrimSpace(cfg.APIKey) == "" {
		a.st.Update(id, func(e *Email) { e.AIStatus = "off"; e.AIError = "" })
		return
	}
	a.st.Update(id, func(e *Email) { e.AIStatus = "running"; e.AIError = "" })

	body := email.Text
	if strings.TrimSpace(body) == "" {
		body = htmlToText(email.HTML)
	}
	maxChars := cfg.MaxChars
	if maxChars <= 0 {
		maxChars = 6000
	}
	body = truncate(body, maxChars)
	meta := fmt.Sprintf("发件人：%s <%s>\n收件人：%s\n主题：%s\n时间：%s\n\n正文：\n%s",
		email.FromName, email.FromAddr, joinAddrs(email.To), email.Subject, email.Date.Format("2006-01-02 15:04"), body)

	var firstErr error

	// 1. 翻译
	if cfg.Translate && isProbablyEnglish(email.Subject+" "+body) {
		out, err := a.chat(cfg, translateSystem, meta, 0.2)
		if err != nil {
			firstErr = err
		} else {
			sub, txt := parseTranslation(out)
			a.st.Update(id, func(e *Email) {
				if sub != "" {
					e.SubjectZh = sub
				}
				if txt != "" {
					e.Translated = txt
				}
			})
		}
	}

	// 2. 智能分类
	if cfg.Classify {
		out, err := a.chat(cfg, classifySystem, meta, 0.1)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
		} else {
			var res struct {
				Summary    string `json:"summary"`
				Importance string `json:"importance"`
				NeedAction bool   `json:"need_action"`
				IsAd       bool   `json:"is_ad"`
				Category   string `json:"category"`
			}
			if err := json.Unmarshal([]byte(extractJSON(out)), &res); err != nil {
				if firstErr == nil {
					firstErr = fmt.Errorf("分类结果解析失败: %w", err)
				}
			} else {
				a.st.Update(id, func(e *Email) {
					e.Summary = strings.TrimSpace(res.Summary)
					e.Importance = normalizeImportance(res.Importance)
					e.NeedAction = res.NeedAction
					e.IsAd = res.IsAd
					e.Category = strings.TrimSpace(res.Category)
				})
			}
		}
	}

	// 3. 自定义规则
	rules := a.cm.Get().Rules
	for _, r := range rules {
		if !r.Enabled || !matchRule(r, email) {
			continue
		}
		prompt := buildRulePrompt(r.Prompt, email, body)
		out, err := a.chat(cfg, "你是用户的私人邮件助理。请严格按照用户给定的要求处理这封邮件，输出简体中文，直接给出结果，不要复述任务。", prompt, 0.3)
		rr := RuleResult{RuleID: r.ID, RuleName: r.Name, Color: r.Color, At: time.Now()}
		if err != nil {
			rr.Error = err.Error()
			if firstErr == nil {
				firstErr = err
			}
		} else {
			rr.Content = out
		}
		a.st.Update(id, func(e *Email) {
			replaced := false
			for i := range e.RuleResults {
				if e.RuleResults[i].RuleID == r.ID {
					e.RuleResults[i] = rr
					replaced = true
				}
			}
			if !replaced {
				e.RuleResults = append(e.RuleResults, rr)
			}
		})
	}

	a.st.Update(id, func(e *Email) {
		e.AIAt = time.Now()
		if firstErr != nil {
			e.AIStatus = "error"
			e.AIError = truncate(firstErr.Error(), 300)
		} else {
			e.AIStatus = "done"
			e.AIError = ""
		}
	})
}

func (a *AIService) Reanalyze(id string) {
	a.st.Update(id, func(e *Email) {
		e.AIStatus = "pending"
		e.RuleResults = nil
		e.Translated = ""
		e.SubjectZh = ""
		e.Summary = ""
		e.Importance = ""
		e.NeedAction = false
		e.IsAd = false
		e.Category = ""
	})
	a.Enqueue(id)
}

func parseTranslation(out string) (subject, body string) {
	s := strings.TrimSpace(out)
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	i := strings.Index(s, "###SUBJECT###")
	j := strings.Index(s, "###BODY###")
	if i >= 0 && j > i {
		subject = strings.TrimSpace(s[i+len("###SUBJECT###") : j])
		body = strings.TrimSpace(s[j+len("###BODY###"):])
		return strings.Trim(subject, "\"'“”"), body
	}
	if j >= 0 {
		return "", strings.TrimSpace(s[j+len("###BODY###"):])
	}
	lines := strings.SplitN(s, "\n", 2)
	if len(lines) == 2 && len([]rune(lines[0])) < 120 {
		return strings.Trim(strings.TrimSpace(lines[0]), "\"'“”"), strings.TrimSpace(lines[1])
	}
	return "", s
}

func extractJSON(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	i := strings.Index(s, "{")
	j := strings.LastIndex(s, "}")
	if i >= 0 && j > i {
		return s[i : j+1]
	}
	return s
}

func normalizeImportance(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "high", "urgent", "critical", "高", "重要":
		return "high"
	case "low", "低":
		return "low"
	case "medium", "normal", "中":
		return "medium"
	}
	return "medium"
}

func isProbablyEnglish(s string) bool {
	var cjk, latin int
	for _, r := range s {
		switch {
		case unicode.Is(unicode.Han, r):
			cjk++
		case unicode.IsLetter(r):
			latin++
		}
	}
	if cjk == 0 {
		return latin > 20
	}
	return float64(latin)/float64(cjk+latin) > 0.75 && cjk < 30
}

func joinAddrs(list []Addr) string {
	out := make([]string, 0, len(list))
	for _, a := range list {
		if a.Name != "" {
			out = append(out, fmt.Sprintf("%s <%s>", a.Name, a.Addr))
		} else {
			out = append(out, a.Addr)
		}
	}
	return strings.Join(out, ", ")
}

// ---------- 规则匹配 ----------

func matchRule(r Rule, e *Email) bool {
	from := strings.ToLower(e.FromName + " " + e.FromAddr)
	to := strings.ToLower(joinAddrs(e.To) + " " + joinAddrs(e.CC))
	if r.FromContains != "" && !strings.Contains(from, strings.ToLower(r.FromContains)) {
		return false
	}
	if r.ToContains != "" && !strings.Contains(to, strings.ToLower(r.ToContains)) {
		return false
	}
	if len(r.Keywords) == 0 && r.FromContains == "" && r.ToContains == "" {
		return false
	}
	if len(r.Keywords) > 0 {
		haystack := strings.ToLower(e.Subject + "\n" + e.Text + "\n" + e.FromName + " " + e.FromAddr + "\n" + to)
		hit := false
		for _, k := range r.Keywords {
			k = strings.ToLower(strings.TrimSpace(k))
			if k != "" && strings.Contains(haystack, k) {
				hit = true
				break
			}
		}
		if !hit {
			return false
		}
	}
	return true
}

func buildRulePrompt(tpl string, e *Email, body string) string {
	summary := e.Summary
	if summary == "" {
		summary = snippetOf(body)
	}
	pairs := map[string]string{
		"{{subject}}":     e.Subject,
		"{{from}}":        e.FromName + " <" + e.FromAddr + ">",
		"{{to}}":          joinAddrs(e.To),
		"{{date}}":        e.Date.Format("2006-01-02 15:04"),
		"{{body}}":        body,
		"{{summary}}":     summary,
		"{{translation}}": e.Translated,
	}
	out := tpl
	for k, v := range pairs {
		out = strings.ReplaceAll(out, k, v)
	}
	if !strings.Contains(tpl, "{{") {
		out = strings.TrimSpace(tpl) + "\n\n【邮件主题】" + e.Subject + "\n【发件人】" + e.FromName + " <" + e.FromAddr + ">\n【正文】\n" + body
	}
	return out
}

// ---------- 首页 AI 摘要 ----------

const demoDigest = `今天你共有 **6 件** 需要亲自处理的事项，其中 2 件建议优先回复。

- **Brightside 的 Marta** 同意 Q4 方案，但要求上线推迟到 11 月 18 日，并追问本地化页面与素材归属，希望周五前签约 —— 建议今天回复。
- **GitHub** 提示账户新增了一把 SSH 密钥，若不是你本人操作，请立刻检查安全设置。
- **Notion** 出现来自新加坡的异地登录记录，建议核对后修改密码。
- **域名 northwind.studio** 15 天后到期，续费 ¥89/年即可，别让它掉线。
- 设计评审还有 3 个阻塞项（空状态插图、图表对比度、筛选记忆），周四前要给结论。

其余 1 封电商促销与 1 份设计周刊可稍后浏览，不影响任何进度。`

func (a *AIService) Digest(force bool) (string, error) {
	a.digestMu.Lock()
	defer a.digestMu.Unlock()
	if !force && a.digestText != "" && time.Since(a.digestAt) < 30*time.Minute {
		return a.digestText, nil
	}
	cfg := a.cm.Get().AI
	if !cfg.Enabled || strings.TrimSpace(cfg.APIKey) == "" {
		if a.cm.Get().Demo {
			a.digestText = demoDigest
			a.digestAt = time.Now()
			return demoDigest, nil
		}
		return "", errors.New("尚未启用 AI，请在设置中配置")
	}
	emails := a.st.All()
	if len(emails) > 40 {
		emails = emails[:40]
	}
	if len(emails) == 0 {
		return "", errors.New("暂无邮件可供分析")
	}
	var b strings.Builder
	for _, e := range emails {
		fmt.Fprintf(&b, "- [%s] %s | 来自:%s | 重要性:%s | 需处理:%v | 广告:%v | 摘要:%s\n",
			e.Date.Format("01-02 15:04"), e.Subject, e.FromName, e.Importance, e.NeedAction, e.IsAd, e.Summary)
	}
	out, err := a.chat(cfg,
		"你是用户的私人邮件秘书。根据邮件列表生成一份简体中文的「今日邮件简报」，控制在 300 字以内。使用 markdown：先一句话总览，再用列表列出 3-5 件最需要关注的事（说明来自谁、要做什么），最后一句提醒可忽略的推广邮件。不要编造列表中不存在的信息。",
		b.String(), 0.3)
	if err != nil {
		return "", err
	}
	a.digestText = out
	a.digestAt = time.Now()
	return out, nil
}
