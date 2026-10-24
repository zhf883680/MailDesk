package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ---------- 数据模型 ----------

type AIConfig struct {
	Enabled       bool     `json:"enabled"`
	BaseURL       string   `json:"base_url"`
	APIKey        string   `json:"api_key"`
	Model         string   `json:"model"`
	Temperature   float64  `json:"temperature"`
	Translate     bool     `json:"translate"`
	TranslateMode string   `json:"translate_mode"` // auto=收到即翻译, manual=阅读时点击翻译
	Classify      bool     `json:"classify"`
	MaxChars      int      `json:"max_chars"`
	AdWhitelist   []string `json:"ad_whitelist"` // 绝不判为广告的发件人 / 域名
}

type Mailbox struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Username  string    `json:"username"`
	PassEnc   string    `json:"pass_enc"`
	IMAPHost  string    `json:"imap_host"`
	IMAPPort  int       `json:"imap_port"`
	IMAPTLS   bool      `json:"imap_tls"` // true = 995 隐式 TLS, false = 143 + STARTTLS
	SMTPHost  string    `json:"smtp_host"`
	SMTPPort  int       `json:"smtp_port"`
	SMTPTLS   bool      `json:"smtp_tls"` // true = 465 隐式 TLS, false = 587 STARTTLS
	Color     string    `json:"color"`
	Demo      bool      `json:"demo"`
	LastSync  time.Time `json:"last_sync"`
	LastError string    `json:"last_error"`
	// 运行时字段，不持久化
	HasPass bool `json:"has_pass"`
}

type Rule struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Enabled      bool     `json:"enabled"`
	Keywords     []string `json:"keywords"`
	FromContains string   `json:"from_contains"`
	ToContains   string   `json:"to_contains"`
	Prompt       string   `json:"prompt"`
	Color        string   `json:"color"`
}

type ForwardConfig struct {
	Enabled   bool   `json:"enabled"`
	BaseURL   string `json:"base_url"`
	DeviceKey string `json:"device_key"`
	Group     string `json:"group"`
	Sound     string `json:"sound"`
	Level     string `json:"level"`
}

type ForwardRule struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Enabled       bool     `json:"enabled"`
	Keywords      []string `json:"keywords"`
	FromContains  string   `json:"from_contains"`
	MailboxIDs    []string `json:"mailbox_ids"`
	TitleTemplate string   `json:"title_template"`
	BodyTemplate  string   `json:"body_template"`
	Color         string   `json:"color"`
}

type Config struct {
	Salt         string        `json:"salt"`
	LoginKeyHash string        `json:"login_key_hash"`
	KeyIsDefault bool          `json:"key_is_default"`
	AI           AIConfig      `json:"ai"`
	Mailboxes    []Mailbox     `json:"mailboxes"`
	Rules        []Rule        `json:"rules"`
	Forward      ForwardConfig `json:"forward"`
	ForwardRules []ForwardRule `json:"forward_rules"`
	SyncMinutes  int           `json:"sync_minutes"`
	FetchLimit   int           `json:"fetch_limit"`
	Demo         bool          `json:"demo"`
}

const defaultLoginKey = "admin123"

// ---------- 配置管理 ----------

type ConfigManager struct {
	mu      sync.RWMutex
	cfg     *Config
	dir     string
	secret  []byte
	changed chan struct{}
}

func newConfigManager(dir string) (*ConfigManager, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	cm := &ConfigManager{dir: dir, changed: make(chan struct{}, 1)}
	sec, err := loadOrCreateSecret(filepath.Join(dir, ".secret"))
	if err != nil {
		return nil, err
	}
	cm.secret = sec

	path := filepath.Join(dir, "config.json")
	cfg := &Config{}
	if b, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(b, cfg); err != nil {
			return nil, fmt.Errorf("配置文件解析失败: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	if cfg.Salt == "" {
		cfg.Salt = randHex(16)
	}
	if cfg.LoginKeyHash == "" {
		cfg.LoginKeyHash = hashKey(cfg.Salt, defaultLoginKey)
		cfg.KeyIsDefault = true
	}
	if cfg.AI.BaseURL == "" {
		cfg.AI.BaseURL = "https://api.openai.com/v1"
		cfg.AI.Model = "gpt-4o-mini"
		cfg.AI.Temperature = 0.2
		cfg.AI.Translate = true
		cfg.AI.Classify = true
		cfg.AI.MaxChars = 6000
	}
	if cfg.AI.MaxChars <= 0 {
		cfg.AI.MaxChars = 6000
	}
	if cfg.AI.TranslateMode == "" {
		cfg.AI.TranslateMode = "auto"
	}
	if cfg.SyncMinutes <= 0 {
		cfg.SyncMinutes = 5
	}
	if cfg.FetchLimit <= 0 {
		cfg.FetchLimit = 40
	}
	if cfg.Mailboxes == nil {
		cfg.Mailboxes = []Mailbox{}
	}
	if cfg.Rules == nil {
		cfg.Rules = []Rule{}
	}
	if cfg.Forward.BaseURL == "" {
		cfg.Forward.BaseURL = "https://api.day.app"
		cfg.Forward.Group = "MailDesk"
		cfg.Forward.Level = "active"
	}
	if cfg.ForwardRules == nil {
		cfg.ForwardRules = []ForwardRule{}
	}
	cm.cfg = cfg
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if err := cm.saveLocked(); err != nil {
			return nil, err
		}
	}
	return cm, nil
}

func (cm *ConfigManager) Get() *Config {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	c := *cm.cfg
	c.Mailboxes = append([]Mailbox(nil), cm.cfg.Mailboxes...)
	c.Rules = append([]Rule(nil), cm.cfg.Rules...)
	c.ForwardRules = append([]ForwardRule(nil), cm.cfg.ForwardRules...)
	for i := range c.Mailboxes {
		c.Mailboxes[i].HasPass = c.Mailboxes[i].PassEnc != ""
	}
	return &c
}

func (cm *ConfigManager) Update(fn func(c *Config) error) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	next := *cm.cfg
	next.Mailboxes = append([]Mailbox(nil), cm.cfg.Mailboxes...)
	next.Rules = append([]Rule(nil), cm.cfg.Rules...)
	next.ForwardRules = append([]ForwardRule(nil), cm.cfg.ForwardRules...)
	if err := fn(&next); err != nil {
		return err
	}
	prev := cm.cfg
	cm.cfg = &next
	if err := cm.saveLocked(); err != nil {
		cm.cfg = prev
		return err
	}
	select {
	case cm.changed <- struct{}{}:
	default:
	}
	return nil
}

func (cm *ConfigManager) saveLocked() error {
	b, err := json.MarshalIndent(cm.cfg, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(cm.dir, "config.json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ---------- 邮箱密码加解密 ----------

func (cm *ConfigManager) Encrypt(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	block, err := aes.NewCipher(cm.secret)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	out := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return base64.StdEncoding.EncodeToString(out), nil
}

func (cm *ConfigManager) Decrypt(enc string) (string, error) {
	if enc == "" {
		return "", nil
	}
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(cm.secret)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("密文长度异常")
	}
	plain, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// ---------- 登录密钥 ----------

func hashKey(salt, key string) string {
	h := sha256.Sum256([]byte(salt + "|maildesk|" + key))
	return hex.EncodeToString(h[:])
}

func (cm *ConfigManager) CheckLoginKey(key string) bool {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	want := cm.cfg.LoginKeyHash
	got := hashKey(cm.cfg.Salt, key)
	return subtle.ConstantTimeCompare([]byte(want), []byte(got)) == 1
}

func (cm *ConfigManager) SetLoginKey(key string) error {
	if len(strings.TrimSpace(key)) < 6 {
		return errors.New("密钥至少 6 位")
	}
	return cm.Update(func(c *Config) error {
		c.Salt = randHex(16)
		c.LoginKeyHash = hashKey(c.Salt, key)
		c.KeyIsDefault = false
		return nil
	})
}

// ---------- 工具 ----------

func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func loadOrCreateSecret(path string) ([]byte, error) {
	if b, err := os.ReadFile(path); err == nil && len(b) == 32 {
		return b, nil
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return nil, err
	}
	return b, nil
}

func (cm *ConfigManager) Mailbox(id string) (Mailbox, bool) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	for _, mb := range cm.cfg.Mailboxes {
		if mb.ID == id {
			return mb, true
		}
	}
	return Mailbox{}, false
}

func (cm *ConfigManager) Password(mb Mailbox) (string, error) {
	return cm.Decrypt(mb.PassEnc)
}

func (cm *ConfigManager) SaveMailboxRuntime(id string, lastSync time.Time, lastErr string) {
	_ = cm.Update(func(c *Config) error {
		for i := range c.Mailboxes {
			if c.Mailboxes[i].ID == id {
				if !lastSync.IsZero() {
					c.Mailboxes[i].LastSync = lastSync
				}
				c.Mailboxes[i].LastError = lastErr
			}
		}
		return nil
	})
}

// IsAdWhitelisted 判断发件人是否在广告白名单中。
// 支持两种写法：完整邮箱（精确匹配）、域名（匹配该域及其子域），避免误伤。
func (cfg *Config) IsAdWhitelisted(e *Email) bool {
	if e == nil {
		return false
	}
	addr := strings.ToLower(strings.TrimSpace(e.FromAddr))
	domain := ""
	if i := strings.LastIndex(addr, "@"); i >= 0 {
		domain = addr[i+1:]
	}
	for _, w := range cfg.AI.AdWhitelist {
		w = strings.ToLower(strings.TrimSpace(w))
		w = strings.TrimPrefix(w, "@")
		w = strings.TrimPrefix(w, "*.")
		w = strings.TrimSuffix(w, "/")
		if w == "" {
			continue
		}
		if strings.Contains(w, "@") {
			if addr == w {
				return true
			}
			continue
		}
		if domain == w || (domain != "" && strings.HasSuffix(domain, "."+w)) {
			return true
		}
	}
	return false
}

// CountsAsAd 判断邮件是否应在统计与「广告」列表中显示。
// 统一处理手动纠正与发件人白名单，避免统计和列表口径不一致。
func (cfg *Config) CountsAsAd(e *Email) bool {
	return e != nil && e.EffectiveIsAd() && !cfg.IsAdWhitelisted(e)
}

func sortMailboxes(list []Mailbox) {
	sort.SliceStable(list, func(i, j int) bool { return list[i].Name < list[j].Name })
}
