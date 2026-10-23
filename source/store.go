package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Addr struct {
	Name string `json:"name"`
	Addr string `json:"addr"`
}

type ForwardLog struct {
	RuleName string    `json:"rule_name"`
	OK       bool      `json:"ok"`
	Error    string    `json:"error,omitempty"`
	At       time.Time `json:"at"`
}

type RuleResult struct {
	RuleID   string    `json:"rule_id"`
	RuleName string    `json:"rule_name"`
	Color    string    `json:"color"`
	Content  string    `json:"content"`
	At       time.Time `json:"at"`
	Error    string    `json:"error,omitempty"`
}

type Email struct {
	ID        string    `json:"id"`
	MailboxID string    `json:"mailbox_id"`
	UID       uint32    `json:"uid"`
	Subject   string    `json:"subject"`
	FromName  string    `json:"from_name"`
	FromAddr  string    `json:"from_addr"`
	To        []Addr    `json:"to"`
	CC        []Addr    `json:"cc"`
	Date      time.Time `json:"date"`
	Snippet   string    `json:"snippet"`
	Text      string    `json:"text"`
	HTML      string    `json:"html"`
	Seen      bool      `json:"seen"`
	Flagged   bool      `json:"flagged"`
	Size      uint32    `json:"size"`

	AIStatus    string       `json:"ai_status"` // off|pending|running|done|error
	SubjectZh   string       `json:"subject_zh"`
	Translated  string       `json:"translated"`
	Summary     string       `json:"summary"`
	Importance  string       `json:"importance"` // high|medium|low
	NeedAction  bool         `json:"need_action"`
	IsAd        bool         `json:"is_ad"`
	Category    string       `json:"category"`
	RuleResults []RuleResult `json:"rule_results"`
	AIAt        time.Time    `json:"ai_at"`
	AIError     string       `json:"ai_error"`

	Forwarded  bool         `json:"forwarded"`
	ForwardLog []ForwardLog `json:"forward_log"`
}

type EmailSummary struct {
	ID         string    `json:"id"`
	MailboxID  string    `json:"mailbox_id"`
	Subject    string    `json:"subject"`
	SubjectZh  string    `json:"subject_zh"`
	FromName   string    `json:"from_name"`
	FromAddr   string    `json:"from_addr"`
	Date       time.Time `json:"date"`
	Snippet    string    `json:"snippet"`
	Seen       bool      `json:"seen"`
	Flagged    bool      `json:"flagged"`
	AIStatus   string    `json:"ai_status"`
	Summary    string    `json:"summary"`
	Importance string    `json:"importance"`
	NeedAction bool      `json:"need_action"`
	IsAd       bool      `json:"is_ad"`
	Category   string    `json:"category"`
	HasTrans   bool      `json:"has_trans"`
	RuleCount  int       `json:"rule_count"`
	Forwarded  bool      `json:"forwarded"`
}

func (e *Email) ToSummary() EmailSummary {
	return EmailSummary{
		ID: e.ID, MailboxID: e.MailboxID, Subject: e.Subject, SubjectZh: e.SubjectZh, FromName: e.FromName,
		FromAddr: e.FromAddr, Date: e.Date, Snippet: e.Snippet, Seen: e.Seen,
		Flagged: e.Flagged, AIStatus: e.AIStatus, Summary: e.Summary,
		Importance: e.Importance, NeedAction: e.NeedAction, IsAd: e.IsAd,
		Category: e.Category, HasTrans: e.Translated != "", RuleCount: len(e.RuleResults),
		Forwarded: e.Forwarded,
	}
}

// ---------- 邮件缓存 ----------

type Store struct {
	mu    sync.RWMutex
	dir   string
	data  map[string]map[uint32]*Email
	dirty chan string
}

func newStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{dir: dir, data: map[string]map[uint32]*Email{}, dirty: make(chan string, 256)}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".json") {
			continue
		}
		mb := strings.TrimSuffix(ent.Name(), ".json")
		b, err := os.ReadFile(filepath.Join(dir, ent.Name()))
		if err != nil {
			continue
		}
		var payload struct {
			Emails []*Email `json:"emails"`
		}
		if json.Unmarshal(b, &payload) != nil {
			continue
		}
		s.data[mb] = map[uint32]*Email{}
		for _, e := range payload.Emails {
			if e == nil || e.UID == 0 {
				continue
			}
			if e.RuleResults == nil {
				e.RuleResults = []RuleResult{}
			}
			if e.ForwardLog == nil {
				e.ForwardLog = []ForwardLog{}
			}
			s.data[mb][e.UID] = e
		}
	}
	go s.saver()
	return s, nil
}

func (s *Store) saver() {
	pending := map[string]bool{}
	var timer <-chan time.Time
	for {
		select {
		case mb := <-s.dirty:
			pending[mb] = true
			if timer == nil {
				timer = time.After(500 * time.Millisecond)
			}
		case <-timer:
			for mb := range pending {
				s.saveNow(mb)
			}
			pending = map[string]bool{}
			timer = nil
		}
	}
}

func (s *Store) touch(mb string) {
	select {
	case s.dirty <- mb:
	default:
	}
}

func (s *Store) saveNow(mb string) {
	s.mu.RLock()
	emails := make([]*Email, 0, len(s.data[mb]))
	for _, e := range s.data[mb] {
		emails = append(emails, e)
	}
	s.mu.RUnlock()
	sort.Slice(emails, func(i, j int) bool { return emails[i].Date.After(emails[j].Date) })
	// 单邮箱最多保留 600 封
	if len(emails) > 600 {
		emails = emails[:600]
	}
	b, err := json.Marshal(struct {
		Emails []*Email `json:"emails"`
	}{emails})
	if err != nil {
		return
	}
	path := filepath.Join(s.dir, mb+".json")
	tmp := path + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, path)
	}
}

func (s *Store) Flush() {
	s.mu.RLock()
	ids := make([]string, 0, len(s.data))
	for id := range s.data {
		ids = append(ids, id)
	}
	s.mu.RUnlock()
	for _, id := range ids {
		s.saveNow(id)
	}
}

func (s *Store) Get(id string) *Email {
	mb, uid, ok := splitID(id)
	if !ok {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if m, ok := s.data[mb]; ok {
		return m[uid]
	}
	return nil
}

func (s *Store) Copy(id string) *Email {
	e := s.Get(id)
	if e == nil {
		return nil
	}
	cp := *e
	return &cp
}

func (s *Store) MailboxEmails(mb string) []*Email {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Email, 0, len(s.data[mb]))
	for _, e := range s.data[mb] {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date.After(out[j].Date) })
	return out
}

func (s *Store) All() []*Email {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*Email
	for _, m := range s.data {
		for _, e := range m {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date.After(out[j].Date) })
	return out
}

func (s *Store) Upsert(e *Email) {
	s.mu.Lock()
	if s.data[e.MailboxID] == nil {
		s.data[e.MailboxID] = map[uint32]*Email{}
	}
	s.data[e.MailboxID][e.UID] = e
	s.mu.Unlock()
	s.touch(e.MailboxID)
}

func (s *Store) Update(id string, fn func(e *Email)) *Email {
	mb, uid, ok := splitID(id)
	if !ok {
		return nil
	}
	s.mu.Lock()
	e, exists := s.data[mb][uid]
	if !exists {
		s.mu.Unlock()
		return nil
	}
	fn(e)
	s.mu.Unlock()
	s.touch(mb)
	return e
}

func (s *Store) Delete(id string) {
	mb, uid, ok := splitID(id)
	if !ok {
		return
	}
	s.mu.Lock()
	delete(s.data[mb], uid)
	s.mu.Unlock()
	s.touch(mb)
}

func (s *Store) DeleteMailbox(mb string) {
	s.mu.Lock()
	delete(s.data, mb)
	s.mu.Unlock()
	_ = os.Remove(filepath.Join(s.dir, mb+".json"))
}

// mergeFetched 合并新拉取的邮件，保留已有邮件的 AI 结果与本地状态。
// 返回需要 AI 处理的新邮件。
func (s *Store) MergeFetched(mb string, fetched []*Email) (added, needAI []*Email) {
	s.mu.Lock()
	if s.data[mb] == nil {
		s.data[mb] = map[uint32]*Email{}
	}
	for _, e := range fetched {
		old, ok := s.data[mb][e.UID]
		if ok {
			e.AIStatus = old.AIStatus
			e.SubjectZh = old.SubjectZh
			e.Translated = old.Translated
			e.Summary = old.Summary
			e.Importance = old.Importance
			e.NeedAction = old.NeedAction
			e.IsAd = old.IsAd
			e.Category = old.Category
			e.RuleResults = old.RuleResults
			e.AIAt = old.AIAt
			e.AIError = old.AIError
			e.Flagged = old.Flagged
			e.Forwarded = old.Forwarded
			if len(old.ForwardLog) > 0 {
				e.ForwardLog = old.ForwardLog
			}
			if e.Text == "" && old.Text != "" {
				e.Text = old.Text
				e.HTML = old.HTML
			}
			if e.AIStatus == "" {
				e.AIStatus = "pending"
			}
			if e.AIStatus == "pending" || e.AIStatus == "running" || e.AIStatus == "error" {
				needAI = append(needAI, e)
			}
		} else {
			if e.AIStatus == "" {
				e.AIStatus = "pending"
			}
			added = append(added, e)
			needAI = append(needAI, e)
		}
		s.data[mb][e.UID] = e
	}
	// 内存中同样只保留最近 600 封，避免长期运行无限增长
	if len(s.data[mb]) > 600 {
		list := make([]*Email, 0, len(s.data[mb]))
		for _, e := range s.data[mb] {
			list = append(list, e)
		}
		sort.Slice(list, func(i, j int) bool { return list[i].Date.After(list[j].Date) })
		for _, e := range list[600:] {
			delete(s.data[mb], e.UID)
		}
	}
	s.mu.Unlock()
	s.touch(mb)
	return added, needAI
}

func (s *Store) PendingEmails() []*Email {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*Email
	for _, m := range s.data {
		for _, e := range m {
			if e.AIStatus == "pending" || e.AIStatus == "running" {
				out = append(out, e)
			}
		}
	}
	return out
}

func splitID(id string) (string, uint32, bool) {
	i := strings.LastIndex(id, ":")
	if i <= 0 {
		return "", 0, false
	}
	var uid uint32
	for _, c := range id[i+1:] {
		if c < '0' || c > '9' {
			return "", 0, false
		}
		uid = uid*10 + uint32(c-'0')
	}
	if uid == 0 {
		return "", 0, false
	}
	return id[:i], uid, true
}

func makeID(mb string, uid uint32) string {
	return mb + ":" + itoa(uid)
}

func itoa(v uint32) string {
	if v == 0 {
		return "0"
	}
	var buf [10]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
