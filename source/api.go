package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

type ipState struct {
	fails  int
	locked bool
	last   time.Time
}

type Server struct {
	cm       *ConfigManager
	st       *Store
	ai       *AIService
	fw       *Forwarder
	push     *PushService
	mu       sync.Mutex
	sessions map[string]time.Time
	ips      map[string]*ipState
	syncing  map[string]bool
	started  time.Time
	static   http.Handler
}

const maxLoginFails = 3
const sessionTTL = 30 * 24 * time.Hour

func newServer(cm *ConfigManager, st *Store, ai *AIService, fw *Forwarder, push *PushService, static http.Handler) *Server {
	return &Server{
		cm: cm, st: st, ai: ai, fw: fw, push: push, static: static,
		sessions: map[string]time.Time{},
		ips:      map[string]*ipState{},
		syncing:  map[string]bool{},
		started:  time.Now(),
	}
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *Server) locked(ip string) *ipState {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.ips[ip]
	if st == nil {
		st = &ipState{}
		s.ips[ip] = st
	}
	return st
}

// ---------- 路由 ----------

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/", s.handleAPI)
	mux.Handle("/", s.static)
	return s.withHeaders(mux)
}

func (s *Server) withHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func ok(w http.ResponseWriter, data interface{}) {
	writeJSON(w, 200, map[string]interface{}{"ok": true, "data": data})
}

func fail(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]interface{}{"ok": false, "error": msg})
}

func (s *Server) handleAPI(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/")
	parts := strings.Split(strings.Trim(path, "/"), "/")

	switch parts[0] {
	case "login":
		s.handleLogin(w, r)
		return
	case "lock-status":
		s.handleLockStatus(w, r)
		return
	case "logout":
		s.handleLogout(w, r)
		return
	}

	if !s.authorized(r) {
		fail(w, 401, "登录已过期，请重新登录")
		return
	}

	switch parts[0] {
	case "bootstrap":
		s.handleBootstrap(w, r)
	case "mailboxes":
		s.handleMailboxes(w, r, parts)
	case "sync":
		s.handleSync(w, r)
	case "emails":
		s.handleEmails(w, r, parts)
	case "send":
		s.handleSend(w, r)
	case "settings":
		s.handleSettings(w, r, parts)
	case "ai":
		s.handleAI(w, r, parts)
	case "forward":
		s.handleForward(w, r, parts)
	case "push":
		s.handlePush(w, r, parts)
	case "digest":
		s.handleDigest(w, r)
	case "stats":
		s.handleStats(w, r)
	default:
		fail(w, 404, "接口不存在")
	}
}

// ---------- 登录与锁定 ----------

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		fail(w, 405, "方法不允许")
		return
	}
	ip := clientIP(r)
	st := s.locked(ip)
	s.mu.Lock()
	locked := st.locked
	s.mu.Unlock()
	if locked {
		fail(w, 423, "该 IP 已被锁定（连续 3 次密钥错误），只能重启程序解锁")
		return
	}
	var body struct {
		Key string `json:"key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		fail(w, 400, "请求格式错误")
		return
	}
	if !s.cm.CheckLoginKey(strings.TrimSpace(body.Key)) {
		s.mu.Lock()
		st.fails++
		st.last = time.Now()
		remaining := maxLoginFails - st.fails
		if st.fails >= maxLoginFails {
			st.locked = true
			remaining = 0
		}
		s.mu.Unlock()
		_ = remaining
		if st.locked {
			fail(w, 423, "密钥错误次数达到 3 次，该 IP 已锁定，只能重启程序解锁")
		} else {
			fail(w, 401, fmt.Sprintf("访问密钥不正确，还可尝试 %d 次", remaining))
		}
		return
	}
	s.mu.Lock()
	delete(s.ips, ip)
	token := randHex(24)
	s.sessions[token] = time.Now().Add(sessionTTL)
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{
		Name: "md_token", Value: token, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, MaxAge: int(sessionTTL.Seconds()),
	})
	ok(w, map[string]interface{}{"key_is_default": s.cm.Get().KeyIsDefault})
}

func (s *Server) handleLockStatus(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	s.mu.Lock()
	st := s.ips[ip]
	resp := map[string]interface{}{"locked": false, "remaining": maxLoginFails}
	if st != nil {
		resp["locked"] = st.locked
		resp["remaining"] = max(0, maxLoginFails-st.fails)
	}
	s.mu.Unlock()
	ok(w, resp)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("md_token"); err == nil {
		s.mu.Lock()
		delete(s.sessions, c.Value)
		s.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: "md_token", Value: "", Path: "/", MaxAge: -1})
	ok(w, nil)
}

func (s *Server) authorized(r *http.Request) bool {
	c, err := r.Cookie("md_token")
	if err != nil || c.Value == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	exp, exists := s.sessions[c.Value]
	if !exists {
		return false
	}
	if time.Now().After(exp) {
		delete(s.sessions, c.Value)
		return false
	}
	return true
}

// ---------- 概览 ----------

type MailboxStat struct {
	Total      int `json:"total"`
	Unread     int `json:"unread"`
	High       int `json:"high"`
	NeedAction int `json:"need_action"`
	Ads        int `json:"ads"`
}

type Stats struct {
	Total      int                    `json:"total"`
	Unread     int                    `json:"unread"`
	High       int                    `json:"high"`
	NeedAction int                    `json:"need_action"`
	Ads        int                    `json:"ads"`
	PendingAI  int                    `json:"pending_ai"`
	ByMailbox  map[string]MailboxStat `json:"by_mailbox"`
	LastSync   time.Time              `json:"last_sync"`
}

func (s *Server) stats() Stats {
	cfg := s.cm.Get()
	st := Stats{ByMailbox: map[string]MailboxStat{}}
	for _, mb := range cfg.Mailboxes {
		st.ByMailbox[mb.ID] = MailboxStat{}
	}
	for _, e := range s.st.All() {
		ms := st.ByMailbox[e.MailboxID]
		ms.Total++
		st.Total++
		if !e.Seen {
			ms.Unread++
			st.Unread++
		}
		if e.Importance == "high" && !e.Handled {
			ms.High++
			st.High++
		}
		if e.EffectiveNeedAction() {
			ms.NeedAction++
			st.NeedAction++
		}
		if cfg.CountsAsAd(e) {
			ms.Ads++
			st.Ads++
		}
		st.ByMailbox[e.MailboxID] = ms
		if e.AIStatus == "pending" || e.AIStatus == "running" {
			st.PendingAI++
		}
	}
	for _, mb := range cfg.Mailboxes {
		if mb.LastSync.After(st.LastSync) {
			st.LastSync = mb.LastSync
		}
	}
	return st
}

// handleStats 只返回计数，供前端定时轮询；避免反复下发邮箱、AI 等配置。
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		fail(w, 405, "方法不允许")
		return
	}
	ok(w, s.stats())
}

func (s *Server) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	cfg := s.cm.Get()
	mbs := make([]Mailbox, 0, len(cfg.Mailboxes))
	for _, mb := range cfg.Mailboxes {
		mb.HasPass = mb.PassEnc != ""
		mb.PassEnc = ""
		mbs = append(mbs, mb)
	}
	aiCfg := cfg.AI
	masked := ""
	if aiCfg.APIKey != "" {
		if len(aiCfg.APIKey) > 8 {
			masked = aiCfg.APIKey[:4] + "••••" + aiCfg.APIKey[len(aiCfg.APIKey)-4:]
		} else {
			masked = "••••"
		}
	}
	aiCfg.APIKey = masked
	fwd := cfg.Forward
	if fwd.DeviceKey != "" {
		fwd.DeviceKey = maskSecret(fwd.DeviceKey)
	}
	pushStatus := PushStatus{}
	if s.push != nil {
		pushStatus = s.push.Status()
	}
	ok(w, map[string]interface{}{
		"mailboxes":      mbs,
		"ai":             aiCfg,
		"rules":          cfg.Rules,
		"forward":        fwd,
		"forward_rules":  cfg.ForwardRules,
		"notifications":  pushStatus,
		"sync_minutes":   cfg.SyncMinutes,
		"fetch_limit":    cfg.FetchLimit,
		"key_is_default": cfg.KeyIsDefault,
		"demo":           cfg.Demo,
		"stats":          s.stats(),
		"version":        appVersion,
	})
}

// ---------- 邮箱管理 ----------

func (s *Server) handleMailboxes(w http.ResponseWriter, r *http.Request, parts []string) {
	// /api/mailboxes            GET, POST
	// /api/mailboxes/{id}       PUT, DELETE
	// /api/mailboxes/{id}/test  POST
	// /api/mailboxes/{id}/sync  POST
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			ok(w, s.cm.Get().Mailboxes)
		case http.MethodPost:
			s.saveMailbox(w, r, "")
		default:
			fail(w, 405, "方法不允许")
		}
		return
	}
	id := parts[1]
	if len(parts) == 3 {
		switch parts[2] {
		case "test":
			s.testMailbox(w, r, id)
		case "sync":
			go s.syncMailbox(id, true)
			ok(w, map[string]string{"status": "started"})
		default:
			fail(w, 404, "接口不存在")
		}
		return
	}
	switch r.Method {
	case http.MethodPut:
		s.saveMailbox(w, r, id)
	case http.MethodDelete:
		if err := s.cm.Update(func(c *Config) error {
			out := c.Mailboxes[:0]
			for _, mb := range c.Mailboxes {
				if mb.ID != id {
					out = append(out, mb)
				}
			}
			c.Mailboxes = out
			return nil
		}); err != nil {
			fail(w, 500, err.Error())
			return
		}
		s.st.DeleteMailbox(id)
		ok(w, nil)
	default:
		fail(w, 405, "方法不允许")
	}
}

type mailboxInput struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	Username string `json:"username"`
	Password string `json:"password"`
	IMAPHost string `json:"imap_host"`
	IMAPPort int    `json:"imap_port"`
	IMAPTLS  bool   `json:"imap_tls"`
	SMTPHost string `json:"smtp_host"`
	SMTPPort int    `json:"smtp_port"`
	SMTPTLS  bool   `json:"smtp_tls"`
	Color    string `json:"color"`
}

func (s *Server) saveMailbox(w http.ResponseWriter, r *http.Request, id string) {
	var in mailboxInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		fail(w, 400, "请求格式错误")
		return
	}
	if strings.TrimSpace(in.Email) == "" || strings.TrimSpace(in.IMAPHost) == "" || strings.TrimSpace(in.SMTPHost) == "" {
		fail(w, 400, "邮箱地址、IMAP 与 SMTP 服务器不能为空")
		return
	}
	var encPass string
	if in.Password != "" {
		var err error
		encPass, err = s.cm.Encrypt(in.Password)
		if err != nil {
			fail(w, 500, "密码加密失败")
			return
		}
	}
	var saved Mailbox
	err := s.cm.Update(func(c *Config) error {
		if id == "" {
			saved = Mailbox{
				ID: randHex(8), Name: firstNonEmpty(in.Name, in.Email), Email: in.Email,
				Username: firstNonEmpty(in.Username, in.Email), IMAPHost: in.IMAPHost,
				IMAPPort: in.IMAPPort, IMAPTLS: in.IMAPTLS, SMTPHost: in.SMTPHost,
				SMTPPort: in.SMTPPort, SMTPTLS: in.SMTPTLS, Color: firstNonEmpty(in.Color, "#0d6b5f"),
				PassEnc: encPass,
			}
			c.Mailboxes = append(c.Mailboxes, saved)
			return nil
		}
		for i := range c.Mailboxes {
			if c.Mailboxes[i].ID == id {
				mb := &c.Mailboxes[i]
				mb.Name = firstNonEmpty(in.Name, mb.Name)
				mb.Email = in.Email
				mb.Username = firstNonEmpty(in.Username, in.Email)
				mb.IMAPHost = in.IMAPHost
				mb.IMAPPort = in.IMAPPort
				mb.IMAPTLS = in.IMAPTLS
				mb.SMTPHost = in.SMTPHost
				mb.SMTPPort = in.SMTPPort
				mb.SMTPTLS = in.SMTPTLS
				if in.Color != "" {
					mb.Color = in.Color
				}
				if encPass != "" {
					mb.PassEnc = encPass
				}
				saved = *mb
				return nil
			}
		}
		return fmt.Errorf("邮箱不存在")
	})
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	saved.HasPass = saved.PassEnc != ""
	saved.PassEnc = ""
	ok(w, saved)
	go s.syncMailbox(saved.ID, true)
}

func (s *Server) testMailbox(w http.ResponseWriter, r *http.Request, id string) {
	var in mailboxInput
	_ = json.NewDecoder(r.Body).Decode(&in)
	mb, exists := s.cm.Mailbox(id)
	pass := in.Password
	if pass == "" && exists {
		pass, _ = s.cm.Password(mb)
	}
	if !exists {
		mb = Mailbox{Email: in.Email, Username: firstNonEmpty(in.Username, in.Email), IMAPHost: in.IMAPHost,
			IMAPPort: in.IMAPPort, IMAPTLS: in.IMAPTLS, SMTPHost: in.SMTPHost, SMTPPort: in.SMTPPort, SMTPTLS: in.SMTPTLS}
	}
	if mb.Username == "" {
		mb.Username = mb.Email
	}
	if err := TestMailbox(mb, pass); err != nil {
		fail(w, 400, err.Error())
		return
	}
	ok(w, map[string]string{"message": "IMAP 与 SMTP 连接成功"})
}

// ---------- 同步 ----------

func (s *Server) handleSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		fail(w, 405, "方法不允许")
		return
	}
	go s.syncAll(true)
	ok(w, map[string]string{"status": "started"})
}

func (s *Server) syncAll(force bool) {
	cfg := s.cm.Get()
	for _, mb := range cfg.Mailboxes {
		if mb.Demo {
			continue
		}
		s.syncMailbox(mb.ID, force)
	}
}

func (s *Server) syncMailbox(id string, force bool) {
	s.mu.Lock()
	if s.syncing[id] {
		s.mu.Unlock()
		return
	}
	s.syncing[id] = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.syncing, id)
		s.mu.Unlock()
	}()

	mb, exists := s.cm.Mailbox(id)
	if !exists || mb.Demo {
		return
	}
	lastSync := "never"
	if !mb.LastSync.IsZero() {
		lastSync = mb.LastSync.Format(time.RFC3339)
	}
	log.Printf("[sync] start mailbox=%s email=%s last_sync=%s force=%t", id, mb.Email, lastSync, force)
	cfg := s.cm.Get()
	pass, err := s.cm.Password(mb)
	if err != nil || pass == "" {
		s.cm.SaveMailboxRuntime(id, time.Time{}, "未设置邮箱密码或授权码")
		log.Printf("[sync] skipped mailbox=%s reason=missing_password", id)
		return
	}
	emails, err := fetchMailbox(mb, pass, cfg.FetchLimit)
	if err != nil {
		s.cm.SaveMailboxRuntime(id, time.Time{}, err.Error())
		log.Printf("[sync] failed mailbox=%s error=%v", id, err)
		return
	}
	firstSync := mb.LastSync.IsZero()
	added, need := s.st.MergeFetched(id, emails)
	s.cm.SaveMailboxRuntime(id, time.Now(), "")
	log.Printf("[sync] complete mailbox=%s fetched=%d added=%d ai_queued=%d first_sync=%t", id, len(emails), len(added), len(need), firstSync)
	for _, e := range need {
		s.ai.Enqueue(e.ID)
	}
	if s.fw != nil {
		for _, e := range added {
			s.fw.Dispatch(e.ID, firstSync)
		}
	}
	if !firstSync {
		for _, e := range added {
			s.pushNotifyInBackground(e.ID)
		}
	}
}

// ---------- 邮件列表与详情 ----------

func (s *Server) handleEmails(w http.ResponseWriter, r *http.Request, parts []string) {
	if len(parts) == 1 {
		if r.Method != http.MethodGet {
			fail(w, 405, "方法不允许")
			return
		}
		s.listEmails(w, r)
		return
	}
	if len(parts) == 2 && parts[1] == "ignore-ads" {
		s.ignoreAds(w, r)
		return
	}
	id := parts[1]
	if len(parts) == 3 {
		switch parts[2] {
		case "flags":
			s.setEmailFlags(w, r, id)
		case "analyze":
			s.ai.Reanalyze(id)
			ok(w, map[string]string{"status": "queued"})
		case "translate":
			if s.st.Get(id) == nil {
				fail(w, 404, "邮件不存在")
				return
			}
			if _, err := s.ai.TranslateEmail(id); err != nil {
				fail(w, 500, err.Error())
				return
			}
			ok(w, s.emailView(id))
		case "forward":
			if s.st.Get(id) == nil {
				fail(w, 404, "邮件不存在")
				return
			}
			s.fw.Retry(id)
			ok(w, map[string]string{"status": "queued"})
		default:
			fail(w, 404, "接口不存在")
		}
		return
	}
	switch r.Method {
	case http.MethodGet:
		e := s.st.Get(id)
		if e == nil {
			fail(w, 404, "邮件不存在")
			return
		}
		if !e.Seen {
			s.st.Update(id, func(em *Email) { em.Seen = true })
			if mb, exists := s.cm.Mailbox(e.MailboxID); exists && !mb.Demo {
				seen := true
				go func() {
					pass, err := s.cm.Password(mb)
					if err == nil {
						_ = setFlags(mb, pass, e.UID, &seen, nil)
					}
				}()
			}
		}
		ok(w, s.emailView(id))
	case http.MethodDelete:
		e := s.st.Get(id)
		if e == nil {
			fail(w, 404, "邮件不存在")
			return
		}
		s.st.Delete(id)
		if mb, exists := s.cm.Mailbox(e.MailboxID); exists && !mb.Demo {
			go func() {
				pass, err := s.cm.Password(mb)
				if err == nil {
					_ = trashMail(mb, pass, e.UID)
				}
			}()
		}
		ok(w, nil)
	default:
		fail(w, 405, "方法不允许")
	}
}

func (s *Server) listEmails(w http.ResponseWriter, r *http.Request) {
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	mb := r.URL.Query().Get("mailbox")
	filter := r.URL.Query().Get("filter")
	limit := parseInt(r.URL.Query().Get("limit"), 200)

	var source []*Email
	if mb != "" && mb != "all" {
		source = s.st.MailboxEmails(mb)
	} else {
		source = s.st.All()
	}
	cfgNow := s.cm.Get()
	out := make([]EmailSummary, 0, len(source))
	for _, e := range source {
		if filter == "unread" && e.Seen {
			continue
		}
		if filter == "important" && (e.Importance != "high" || e.Handled) {
			continue
		}
		if filter == "action" && !e.EffectiveNeedAction() {
			continue
		}
		if filter == "ads" && !cfgNow.CountsAsAd(e) {
			continue
		}
		if q != "" {
			hay := strings.ToLower(e.Subject + " " + e.SubjectZh + " " + e.FromName + " " + e.FromAddr + " " + e.Text + " " + e.Translated + " " + e.Summary + " " + e.Category)
			if !strings.Contains(hay, q) {
				continue
			}
		}
		sum := e.ToSummary()
		sum.IsAd = cfgNow.IsAdLabel(e)
		out = append(out, sum)
		if len(out) >= limit {
			break
		}
	}
	ok(w, out)
}

func (s *Server) setEmailFlags(w http.ResponseWriter, r *http.Request, id string) {
	var in struct {
		Seen       *bool   `json:"seen"`
		Flagged    *bool   `json:"flagged"`
		Handled    *bool   `json:"handled"`
		AdIgnored  *bool   `json:"ad_ignored"`
		AdOverride *string `json:"ad_override"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		fail(w, 400, "请求格式错误")
		return
	}
	e := s.st.Get(id)
	if e == nil {
		fail(w, 404, "邮件不存在")
		return
	}
	s.st.Update(id, func(em *Email) {
		if in.Seen != nil {
			em.Seen = *in.Seen
		}
		if in.Handled != nil {
			em.Handled = *in.Handled
		}
		if in.AdIgnored != nil {
			em.AdIgnored = *in.AdIgnored
		}
		if in.AdOverride != nil {
			switch *in.AdOverride {
			case "not_ad", "ad":
				em.AdOverride = *in.AdOverride
			case "":
				em.AdOverride = ""
			}
		}
		if in.Flagged != nil {
			em.Flagged = *in.Flagged
		}
	})
	if mb, exists := s.cm.Mailbox(e.MailboxID); exists && !mb.Demo {
		go func() {
			pass, err := s.cm.Password(mb)
			if err == nil {
				_ = setFlags(mb, pass, e.UID, in.Seen, in.Flagged)
			}
		}()
	}
	ok(w, s.emailView(id))
}

// emailView 返回给前端的邮件详情：把「已处理」与广告纠正折算成有效值。
func (s *Server) emailView(id string) *Email {
	e := s.st.Copy(id)
	if e == nil {
		return nil
	}
	if e.TranslatedHTML != "" {
		e.TranslatedHTML = cleanTranslatedHTML(e.TranslatedHTML)
		if text := htmlToText(e.TranslatedHTML); text != "" {
			e.Translated = truncate(text, 60000)
		}
	}
	e.NeedAction = e.EffectiveNeedAction()
	e.IsAd = s.cm.Get().IsAdLabel(e)
	return e
}

// ignoreAds 批量忽略广告：只影响广告统计与广告筛选，邮件仍保留在全部邮件中。
func (s *Server) ignoreAds(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		fail(w, 405, "方法不允许")
		return
	}
	var in struct {
		IDs    []string `json:"ids"`
		Ignore *bool    `json:"ignore"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || len(in.IDs) == 0 {
		fail(w, 400, "请求格式错误")
		return
	}
	ignore := true
	if in.Ignore != nil {
		ignore = *in.Ignore
	}
	updated := 0
	for _, id := range in.IDs {
		if s.st.Get(id) == nil {
			continue
		}
		s.st.Update(id, func(e *Email) { e.AdIgnored = ignore })
		updated++
	}
	ok(w, map[string]int{"updated": updated})
}

// ---------- 设置 ----------

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request, parts []string) {
	if len(parts) < 2 || r.Method != http.MethodPut {
		if r.Method == http.MethodGet {
			s.handleBootstrap(w, r)
			return
		}
		fail(w, 405, "方法不允许")
		return
	}
	switch parts[1] {
	case "ai":
		var in AIConfig
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			fail(w, 400, "请求格式错误")
			return
		}
		err := s.cm.Update(func(c *Config) error {
			if strings.Contains(in.APIKey, "••") || in.APIKey == "" {
				in.APIKey = c.AI.APIKey
			}
			if in.BaseURL == "" {
				in.BaseURL = "https://api.openai.com/v1"
			}
			if in.Model == "" {
				in.Model = "gpt-4o-mini"
			}
			if in.MaxChars <= 0 {
				in.MaxChars = 6000
			}
			c.AI = in
			return nil
		})
		if err != nil {
			fail(w, 500, err.Error())
			return
		}
		ok(w, nil)
	case "rules":
		var in []Rule
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			fail(w, 400, "请求格式错误")
			return
		}
		for i := range in {
			if in[i].ID == "" {
				in[i].ID = randHex(6)
			}
			if in[i].Color == "" {
				in[i].Color = "#0d6b5f"
			}
			if in[i].Name == "" {
				in[i].Name = "未命名规则"
			}
		}
		if err := s.cm.Update(func(c *Config) error { c.Rules = in; return nil }); err != nil {
			fail(w, 500, err.Error())
			return
		}
		ok(w, in)
	case "general":
		var in struct {
			SyncMinutes int `json:"sync_minutes"`
			FetchLimit  int `json:"fetch_limit"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			fail(w, 400, "请求格式错误")
			return
		}
		err := s.cm.Update(func(c *Config) error {
			if in.SyncMinutes >= 1 && in.SyncMinutes <= 1440 {
				c.SyncMinutes = in.SyncMinutes
			}
			if in.FetchLimit >= 5 && in.FetchLimit <= 200 {
				c.FetchLimit = in.FetchLimit
			}
			return nil
		})
		if err != nil {
			fail(w, 500, err.Error())
			return
		}
		ok(w, nil)
	case "forward":
		var in ForwardConfig
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			fail(w, 400, "请求格式错误")
			return
		}
		err := s.cm.Update(func(c *Config) error {
			if strings.Contains(in.DeviceKey, "••") || in.DeviceKey == "" {
				in.DeviceKey = c.Forward.DeviceKey
			}
			if strings.TrimSpace(in.BaseURL) == "" {
				in.BaseURL = "https://api.day.app"
			}
			if strings.TrimSpace(in.Group) == "" {
				in.Group = "MailDesk"
			}
			if strings.TrimSpace(in.Level) == "" {
				in.Level = "active"
			}
			c.Forward = in
			return nil
		})
		if err != nil {
			fail(w, 500, err.Error())
			return
		}
		ok(w, nil)
	case "forward-rules":
		var in []ForwardRule
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			fail(w, 400, "请求格式错误")
			return
		}
		for i := range in {
			if in[i].ID == "" {
				in[i].ID = randHex(6)
			}
			if in[i].Color == "" {
				in[i].Color = "#b0761c"
			}
			if in[i].Name == "" {
				in[i].Name = "未命名转发规则"
			}
			if in[i].MailboxIDs == nil {
				in[i].MailboxIDs = []string{}
			}
		}
		if err := s.cm.Update(func(c *Config) error { c.ForwardRules = in; return nil }); err != nil {
			fail(w, 500, err.Error())
			return
		}
		ok(w, in)
	case "security":
		var in struct {
			CurrentKey string `json:"current_key"`
			NewKey     string `json:"new_key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			fail(w, 400, "请求格式错误")
			return
		}
		if !s.cm.CheckLoginKey(strings.TrimSpace(in.CurrentKey)) {
			fail(w, 400, "当前密钥不正确")
			return
		}
		if err := s.cm.SetLoginKey(strings.TrimSpace(in.NewKey)); err != nil {
			fail(w, 400, err.Error())
			return
		}
		ok(w, nil)
	default:
		fail(w, 404, "接口不存在")
	}
}

func (s *Server) handleAI(w http.ResponseWriter, r *http.Request, parts []string) {
	if len(parts) < 2 || r.Method != http.MethodPost {
		fail(w, 405, "方法不允许")
		return
	}
	switch parts[1] {
	case "test":
		var in AIConfig
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			fail(w, 400, "请求格式错误")
			return
		}
		cur := s.cm.Get().AI
		if strings.Contains(in.APIKey, "••") || in.APIKey == "" {
			in.APIKey = cur.APIKey
		}
		if err := s.ai.Test(in); err != nil {
			fail(w, 400, err.Error())
			return
		}
		ok(w, map[string]string{"message": "AI 接口连通正常"})
	case "polish":
		var in struct {
			Text        string `json:"text"`
			Instruction string `json:"instruction"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			fail(w, 400, "请求格式错误")
			return
		}
		out, err := s.ai.Polish(in.Text, in.Instruction)
		if err != nil {
			fail(w, 400, err.Error())
			return
		}
		ok(w, map[string]string{"text": out})
	default:
		fail(w, 404, "接口不存在")
	}
}

func (s *Server) handleDigest(w http.ResponseWriter, r *http.Request) {
	force := r.URL.Query().Get("force") == "1"
	text, err := s.ai.Digest(force || r.Method == http.MethodPost)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	ok(w, map[string]interface{}{"text": text, "at": time.Now()})
}

// ---------- 发信 ----------

func (s *Server) handleSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		fail(w, 405, "方法不允许")
		return
	}
	var in struct {
		MailboxID string   `json:"mailbox_id"`
		To        []string `json:"to"`
		Cc        []string `json:"cc"`
		Subject   string   `json:"subject"`
		Body      string   `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		fail(w, 400, "请求格式错误")
		return
	}
	mb, exists := s.cm.Mailbox(in.MailboxID)
	if !exists {
		fail(w, 400, "请选择发件邮箱")
		return
	}
	pass, err := s.cm.Password(mb)
	if err != nil || pass == "" {
		fail(w, 400, "该邮箱未设置密码或授权码")
		return
	}
	if strings.TrimSpace(in.Subject) == "" {
		in.Subject = "（无主题）"
	}
	if err := sendMail(mb, pass, OutgoingMail{
		To: in.To, Cc: in.Cc, Subject: in.Subject, Body: in.Body,
	}); err != nil {
		fail(w, 500, err.Error())
		return
	}
	ok(w, map[string]string{"message": "邮件已发送"})
}

func maskSecret(s string) string {
	if len(s) > 8 {
		return s[:4] + "••••" + s[len(s)-4:]
	}
	if s == "" {
		return ""
	}
	return "••••"
}

func (s *Server) handleForward(w http.ResponseWriter, r *http.Request, parts []string) {
	if len(parts) < 2 || r.Method != http.MethodPost {
		fail(w, 405, "方法不允许")
		return
	}
	switch parts[1] {
	case "test":
		if err := s.fw.Test(); err != nil {
			fail(w, 400, err.Error())
			return
		}
		ok(w, map[string]string{"message": "测试通知已发送，请查看手机 Bark"})
	default:
		fail(w, 404, "接口不存在")
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func parseInt(s string, def int) int {
	if s == "" {
		return def
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return def
		}
		n = n*10 + int(c-'0')
	}
	if n <= 0 {
		return def
	}
	return n
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ---------- 定时同步 ----------

func (s *Server) startBackgroundSync() {
	go func() {
		time.Sleep(3 * time.Second)
		s.syncAll(false)
	}()
	for {
		cfg := s.cm.Get()
		d := time.Duration(cfg.SyncMinutes) * time.Minute
		// 启用关键词转发时至少每分钟检查一次，保证验证码能及时推送
		if cfg.Forward.Enabled && d > time.Minute {
			d = time.Minute
		}
		if d < time.Minute {
			d = time.Minute
		}
		time.Sleep(d)
		s.syncAll(false)
	}
}

func (s *Server) sortedMailboxIDs() []string {
	cfg := s.cm.Get()
	ids := make([]string, 0, len(cfg.Mailboxes))
	for _, mb := range cfg.Mailboxes {
		ids = append(ids, mb.ID)
	}
	sort.Strings(ids)
	return ids
}
