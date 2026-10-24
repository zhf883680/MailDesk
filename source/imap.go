package main

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"html"
	"io"
	"mime"
	"net"
	"regexp"
	"strings"
	"time"

	md "github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/base"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/commonmark"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/table"
	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	_ "github.com/emersion/go-message/charset"
	"github.com/emersion/go-message/mail"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/transform"
)

func dialIMAP(mb Mailbox) (*client.Client, error) {
	host := strings.TrimSpace(mb.IMAPHost)
	port := mb.IMAPPort
	if port == 0 {
		port = 993
	}
	addr := net.JoinHostPort(host, fmt.Sprint(port))
	dialer := &net.Dialer{Timeout: 25 * time.Second}
	tlsCfg := &tls.Config{ServerName: host}
	var c *client.Client
	var err error
	if mb.IMAPTLS {
		c, err = client.DialWithDialerTLS(dialer, addr, tlsCfg)
	} else {
		c, err = client.DialWithDialer(dialer, addr)
		if err == nil {
			if err = c.StartTLS(tlsCfg); err != nil {
				c.Close()
				return nil, fmt.Errorf("STARTTLS 失败: %w", err)
			}
		}
	}
	if err != nil {
		return nil, fmt.Errorf("连接 IMAP 服务器失败: %w", err)
	}
	return c, nil
}

func imapLogin(mb Mailbox, pass string) (*client.Client, error) {
	c, err := dialIMAP(mb)
	if err != nil {
		return nil, err
	}
	user := mb.Username
	if user == "" {
		user = mb.Email
	}
	if err := c.Login(user, pass); err != nil {
		c.Close()
		return nil, fmt.Errorf("IMAP 登录失败（请检查账号与授权码）: %w", err)
	}
	sendIMAPID(c)
	return c, nil
}

// sendIMAPID 发送 RFC 2971 的 ID 命令，163 / 126 等要求客户端自报身份，
// 否则会返回 "Unsafe Login"。失败不影响后续流程。
type idCommand struct{}

func (idCommand) Command() *imap.Command {
	return &imap.Command{
		Name:      "ID",
		Arguments: []interface{}{imap.RawString(`("name" "MailDesk" "version" "1.0.0" "vendor" "MailDesk")`)},
	}
}
func (idCommand) Handle(resp imap.Resp) error { return nil }

type ignoreHandler struct{}

func (ignoreHandler) Handle(imap.Resp) error { return nil }

func sendIMAPID(c *client.Client) {
	_, _ = c.Execute(idCommand{}, ignoreHandler{})
}

// TestMailbox 测试 IMAP 与 SMTP 连通性
func TestMailbox(mb Mailbox, pass string) error {
	c, err := imapLogin(mb, pass)
	if err != nil {
		return err
	}
	mbox, err := c.Select("INBOX", true)
	if err != nil {
		c.Logout()
		return fmt.Errorf("打开收件箱失败: %w", err)
	}
	_ = mbox
	c.Logout()
	return testSMTP(mb, pass)
}

// fetchMailbox 拉取收件箱最近 limit 封邮件
func fetchMailbox(mb Mailbox, pass string, limit int) ([]*Email, error) {
	if limit <= 0 {
		limit = 40
	}
	c, err := imapLogin(mb, pass)
	if err != nil {
		return nil, err
	}
	defer c.Logout()
	c.Timeout = 60 * time.Second

	mbox, err := c.Select("INBOX", false)
	if err != nil {
		return nil, fmt.Errorf("打开收件箱失败: %w", err)
	}
	total := int(mbox.Messages)
	if total == 0 {
		return nil, nil
	}
	start := total - limit + 1
	if start < 1 {
		start = 1
	}
	seqset := new(imap.SeqSet)
	seqset.AddRange(uint32(start), uint32(total))

	type header struct {
		seq   uint32
		uid   uint32
		flags []string
		size  uint32
		env   *imap.Envelope
	}
	var headers []header
	ch := make(chan *imap.Message, 32)
	done := make(chan error, 1)
	go func() {
		done <- c.Fetch(seqset, []imap.FetchItem{
			imap.FetchEnvelope, imap.FetchUid, imap.FetchFlags, imap.FetchRFC822Size,
		}, ch)
	}()
	for msg := range ch {
		headers = append(headers, header{seq: msg.SeqNum, uid: msg.Uid, flags: msg.Flags, size: msg.Size, env: msg.Envelope})
	}
	if err := <-done; err != nil {
		return nil, fmt.Errorf("读取邮件头失败: %w", err)
	}

	// 第二遍：拉取正文（跳过超大邮件）
	bodySet := new(imap.SeqSet)
	index := map[uint32]int{}
	for i, h := range headers {
		index[h.seq] = i
		if h.size > 0 && h.size > 3*1024*1024 {
			continue
		}
		bodySet.AddNum(h.seq)
	}
	bodies := map[uint32][]byte{}
	if !bodySet.Empty() {
		section := &imap.BodySectionName{Peek: true}
		bch := make(chan *imap.Message, 16)
		bdone := make(chan error, 1)
		go func() {
			bdone <- c.Fetch(bodySet, []imap.FetchItem{imap.FetchUid, section.FetchItem()}, bch)
		}()
		for msg := range bch {
			r := msg.GetBody(section)
			if r == nil {
				continue
			}
			b, err := io.ReadAll(io.LimitReader(r, 6*1024*1024))
			if err == nil {
				bodies[msg.SeqNum] = b
			}
		}
		if err := <-bdone; err != nil {
			return nil, fmt.Errorf("读取邮件正文失败: %w", err)
		}
	}

	out := make([]*Email, 0, len(headers))
	for _, h := range headers {
		e := &Email{
			MailboxID: mb.ID,
			UID:       h.uid,
			Size:      h.size,
			Seen:      hasFlag(h.flags, imap.SeenFlag),
			Flagged:   hasFlag(h.flags, imap.FlaggedFlag),
			To:        []Addr{},
			CC:        []Addr{},
		}
		e.ID = makeID(mb.ID, h.uid)
		if h.env != nil {
			e.Subject = decodeHeader(h.env.Subject)
			e.Date = h.env.Date
			if len(h.env.From) > 0 {
				e.FromName = decodeHeader(h.env.From[0].PersonalName)
				e.FromAddr = h.env.From[0].MailboxName + "@" + h.env.From[0].HostName
			}
			for _, a := range h.env.To {
				e.To = append(e.To, Addr{Name: decodeHeader(a.PersonalName), Addr: a.MailboxName + "@" + a.HostName})
			}
			for _, a := range h.env.Cc {
				e.CC = append(e.CC, Addr{Name: decodeHeader(a.PersonalName), Addr: a.MailboxName + "@" + a.HostName})
			}
		}
		if e.Date.IsZero() {
			e.Date = time.Now()
		}
		if raw, ok := bodies[h.seq]; ok {
			applyRawBody(e, raw)
		}
		if e.Subject == "" {
			e.Subject = "（无主题）"
		}
		if e.Snippet == "" {
			e.Snippet = "（无正文预览）"
		}
		out = append(out, e)
	}
	return out, nil
}

func hasFlag(flags []string, f string) bool {
	for _, v := range flags {
		if strings.EqualFold(v, f) {
			return true
		}
	}
	return false
}

var decoders = map[string]func([]byte) ([]byte, error){
	"gbk":     func(b []byte) ([]byte, error) { return transformBytes(b, simplifiedchinese.GBK.NewDecoder()) },
	"gb2312":  func(b []byte) ([]byte, error) { return transformBytes(b, simplifiedchinese.GBK.NewDecoder()) },
	"gb18030": func(b []byte) ([]byte, error) { return transformBytes(b, simplifiedchinese.GB18030.NewDecoder()) },
	"big5":    func(b []byte) ([]byte, error) { return transformBytes(b, traditionalchinese.Big5.NewDecoder()) },
}

func transformBytes(b []byte, t transform.Transformer) ([]byte, error) {
	out, _, err := transform.Bytes(t, b)
	return out, err
}

func decodeHeader(s string) string {
	if s == "" {
		return ""
	}
	dec := &mime.WordDecoder{CharsetReader: func(charset string, input io.Reader) (io.Reader, error) {
		cs := strings.ToLower(strings.TrimSpace(charset))
		if fn, ok := decoders[cs]; ok {
			b, err := io.ReadAll(input)
			if err != nil {
				return nil, err
			}
			out, err := fn(b)
			if err != nil {
				return bytes.NewReader(b), nil
			}
			return bytes.NewReader(out), nil
		}
		return input, nil
	}}
	if out, err := dec.DecodeHeader(s); err == nil {
		return out
	}
	return s
}

// applyRawBody 解析 MIME 结构，抽取纯文本与 HTML
func applyRawBody(e *Email, raw []byte) {
	mr, err := mail.CreateReader(bytes.NewReader(raw))
	if err != nil {
		// 退化处理：当作纯文本
		txt := strings.TrimSpace(string(raw))
		e.Text = txt
		e.Snippet = snippetOf(txt)
		return
	}
	defer mr.Close()
	if d, err := mr.Header.Date(); err == nil && !d.IsZero() {
		e.Date = d
	}
	if sub, err := mr.Header.Subject(); err == nil && sub != "" {
		e.Subject = decodeHeader(sub)
	}
	if list, err := mr.Header.AddressList("From"); err == nil && len(list) > 0 {
		e.FromName = decodeHeader(list[0].Name)
		e.FromAddr = list[0].Address
	}
	if list, err := mr.Header.AddressList("To"); err == nil && len(list) > 0 {
		e.To = e.To[:0]
		for _, a := range list {
			e.To = append(e.To, Addr{Name: decodeHeader(a.Name), Addr: a.Address})
		}
	}
	if list, err := mr.Header.AddressList("Cc"); err == nil && len(list) > 0 {
		e.CC = e.CC[:0]
		for _, a := range list {
			e.CC = append(e.CC, Addr{Name: decodeHeader(a.Name), Addr: a.Address})
		}
	}

	var text, htmlBody string
	for {
		p, err := mr.NextPart()
		if err == io.EOF || err != nil {
			break
		}
		switch h := p.Header.(type) {
		case *mail.InlineHeader:
			ct, _, _ := h.ContentType()
			b, err := io.ReadAll(io.LimitReader(p.Body, 8*1024*1024))
			if err != nil {
				continue
			}
			switch {
			case strings.HasPrefix(ct, "text/plain"):
				text += string(b) + "\n"
			case strings.HasPrefix(ct, "text/html"):
				htmlBody += string(b) + "\n"
			}
		case *mail.AttachmentHeader:
			// 跳过附件
		}
	}
	e.Text = normalizeText(text)
	e.HTML = sanitizeHTML(htmlBody)
	if e.HTML != "" {
		// 转成 Markdown：去掉样式与冗余标签，后续翻译更省 token，展示也更干净
		e.Markdown = htmlToMarkdown(e.HTML)
	}
	if e.Text == "" && e.HTML != "" {
		e.Text = htmlToText(e.HTML)
	}
	// 控制缓存体积
	e.Text = truncate(e.Text, 60000)
	e.HTML = truncateBytes(e.HTML, 400000)
	e.Snippet = snippetOf(e.Text)
}

// htmlToMarkdown 把已净化的 HTML 邮件转成 Markdown，去掉内联样式与布局标签。
func htmlToMarkdown(src string) string {
	if strings.TrimSpace(src) == "" {
		return ""
	}
	out, err := mdConv.ConvertString(src)
	if err != nil {
		return ""
	}
	out = strings.TrimSpace(out)
	out = reBlankMD.ReplaceAllString(out, "\n\n")
	return truncate(out, 60000)
}

var reBlankMD = regexp.MustCompile(`\n{3,}`)

// mdConv 带表格插件，避免账单类邮件里的表格被压成一行文字
var mdConv = md.NewConverter(
	md.WithPlugins(
		base.NewBasePlugin(),
		commonmark.NewCommonmarkPlugin(),
		table.NewTablePlugin(table.WithHeaderPromotion(true)),
	),
)

// markdownToText 粗略去掉 Markdown 记号，作为纯文本兜底与搜索用。
func markdownToText(s string) string {
	s = reMdImage.ReplaceAllString(s, "$1")
	s = reMdLink.ReplaceAllString(s, "$1")
	s = reMdTableSep.ReplaceAllString(s, "")
	s = reMdMark.ReplaceAllString(s, "")
	s = reMdHeading.ReplaceAllString(s, "$1")
	s = strings.ReplaceAll(s, "\n", "\n")
	return normalizeText(s)
}

var (
	reMdImage    = regexp.MustCompile(`!\[([^\]]*)\]\([^)]*\)`)
	reMdLink     = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	reMdTableSep = regexp.MustCompile(`(?m)^\|?[\s:-]*-[\s:|-]*\|?$`)
	reMdMark     = regexp.MustCompile("(\\*\\*|__|`)")
	reMdHeading  = regexp.MustCompile(`(?m)^#{1,6}\s+`)
)

var (
	reSelfClose = regexp.MustCompile(`(?is)<(script|style|iframe|object|embed|link|meta)[^>]*/?>`)
	reEventAttr = regexp.MustCompile(`(?is)\son[a-z]+\s*=\s*"[^"]*"|\son[a-z]+\s*=\s*'[^']*'|\son[a-z]+\s*=\s*[^\s>]+`)
	reJSURL     = regexp.MustCompile(`(?is)javascript:`)
	reBr        = regexp.MustCompile(`(?is)<br\s*/?>|</p>|</div>|</tr>|</h[1-6]>|</li>`)
	reAllTag    = regexp.MustCompile(`(?is)<[^>]+>`)
	reSpace     = regexp.MustCompile(`[ \t\x{00a0}]+`)
	reBlank     = regexp.MustCompile(`\n{3,}`)
)

func sanitizeHTML(s string) string {
	if strings.TrimSpace(s) == "" {
		return ""
	}
	// RE2 不支持反向引用，按标签名逐个移除危险容器
	for _, tag := range []string{"script", "style", "iframe", "object", "embed", "noscript"} {
		re := regexp.MustCompile(`(?is)<` + tag + `[^>]*>.*?</` + tag + `>`)
		s = re.ReplaceAllString(s, "")
	}
	s = reSelfClose.ReplaceAllString(s, "")
	s = reEventAttr.ReplaceAllString(s, "")
	s = reJSURL.ReplaceAllString(s, "")
	if !strings.Contains(strings.ToLower(s), "<base ") {
		s = `<base target="_blank">` + s
	}
	return s
}

func htmlToText(s string) string {
	for _, tag := range []string{"script", "style"} {
		re := regexp.MustCompile(`(?is)<` + tag + `[^>]*>.*?</` + tag + `>`)
		s = re.ReplaceAllString(s, "")
	}
	s = reBr.ReplaceAllString(s, "\n")
	s = reAllTag.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	return normalizeText(s)
}

func truncateBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := s[:n]
	if i := strings.LastIndex(cut, ">"); i > 0 {
		return cut[:i+1]
	}
	return cut
}

func normalizeText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = reSpace.ReplaceAllString(s, " ")
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(l)
	}
	s = strings.Join(lines, "\n")
	s = reBlank.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

func snippetOf(s string) string {
	r := []rune(strings.Join(strings.Fields(s), " "))
	if len(r) > 160 {
		return string(r[:160]) + "…"
	}
	return string(r)
}

// setFlags 同步已读/星标状态
func setFlags(mb Mailbox, pass string, uid uint32, seen, flagged *bool) error {
	c, err := imapLogin(mb, pass)
	if err != nil {
		return err
	}
	defer c.Logout()
	if _, err := c.Select("INBOX", false); err != nil {
		return err
	}
	set := new(imap.SeqSet)
	set.AddNum(uid)
	if seen != nil {
		op := imap.FlagsOp(imap.RemoveFlags)
		if *seen {
			op = imap.AddFlags
		}
		if err := c.UidStore(set, imap.FormatFlagsOp(op, true), []interface{}{imap.SeenFlag}, nil); err != nil {
			return err
		}
	}
	if flagged != nil {
		op := imap.FlagsOp(imap.RemoveFlags)
		if *flagged {
			op = imap.AddFlags
		}
		if err := c.UidStore(set, imap.FormatFlagsOp(op, true), []interface{}{imap.FlaggedFlag}, nil); err != nil {
			return err
		}
	}
	return nil
}

// trashMail 将邮件移入垃圾箱（不存在垃圾箱则直接标记删除）
func trashMail(mb Mailbox, pass string, uid uint32) error {
	c, err := imapLogin(mb, pass)
	if err != nil {
		return err
	}
	defer c.Logout()
	if _, err := c.Select("INBOX", false); err != nil {
		return err
	}
	set := new(imap.SeqSet)
	set.AddNum(uid)

	trash := ""
	boxes := make(chan *imap.MailboxInfo, 32)
	listDone := make(chan error, 1)
	go func() { listDone <- c.List("", "*", boxes) }()
	for box := range boxes {
		n := strings.ToLower(box.Name)
		if n == "trash" || n == "deleted messages" || strings.Contains(n, "已删除") || strings.Contains(n, "垃圾") {
			trash = box.Name
		}
	}
	<-listDone

	if trash != "" {
		if err := c.UidMove(set, trash); err == nil {
			return nil
		}
	}
	if err := c.UidStore(set, imap.FormatFlagsOp(imap.AddFlags, true), []interface{}{imap.DeletedFlag}, nil); err != nil {
		return err
	}
	return c.Expunge(nil)
}
