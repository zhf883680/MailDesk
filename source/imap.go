package main

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"mime"
	"net"
	"regexp"
	"strings"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/emersion/go-message"
	_ "github.com/emersion/go-message/charset"
	"github.com/emersion/go-message/mail"
	xhtml "golang.org/x/net/html"
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
	// 顶层字符集未知时 CreateReader 仍会返回可用的 Reader，不应整封丢弃
	mr, _ := mail.CreateReader(bytes.NewReader(raw))
	if mr == nil {
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
	// 有些发件人把唯一的正文声明成 attachment，先收集起来，只有在没有内联正文时才使用
	var attachText, attachHTML string
	var attachments []Attachment
	readTextPart := func(ct string, body io.Reader) (plain, html string) {
		b, err := io.ReadAll(io.LimitReader(body, 8*1024*1024))
		if err != nil {
			return "", ""
		}
		switch {
		case strings.HasPrefix(ct, "text/plain"):
			return string(b) + "\n", ""
		case strings.HasPrefix(ct, "text/html"):
			return "", string(b) + "\n"
		}
		return "", ""
	}
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			// 未知字符集/编码的分段会被跳过，但它后面的分段仍然有效，
			// 不能因为这一处错误丢掉整封邮件的正文
			if !message.IsUnknownCharset(err) && !message.IsUnknownEncoding(err) {
				break
			}
		}
		if p == nil {
			continue
		}
		switch h := p.Header.(type) {
		case *mail.InlineHeader:
			ct, _, _ := h.ContentType()
			plain, html := readTextPart(ct, p.Body)
			text += plain
			htmlBody += html
		case *mail.AttachmentHeader:
			ct, _, _ := h.ContentType()
			// 先按元数据记录，正文兜底只认文本类分段
			att := Attachment{Index: len(attachments), ContentType: ct}
			if name, ferr := h.Filename(); ferr == nil {
				att.Filename = sanitizeFilename(name)
			}
			if att.Filename == "" {
				att.Filename = defaultAttachmentName(ct)
			}
			// 只缓冲文本类附件，避免把 PDF、图片等大附件读进内存
			if !strings.HasPrefix(ct, "text/") {
				// Part.Body 已完成 base64/QP 解码，数出来的就是真实字节数；
				// 计数走 io.Discard，只是流式读取，不会把附件留在内存里
				n, _ := io.Copy(io.Discard, p.Body)
				att.Size = int(n)
				attachments = append(attachments, att)
				continue
			}
			b, rerr := io.ReadAll(io.LimitReader(p.Body, maxAttachSize))
			if rerr != nil {
				attachments = append(attachments, att)
				continue
			}
			att.Size = len(b)
			attachments = append(attachments, att)
			switch {
			case strings.HasPrefix(ct, "text/plain"):
				attachText += string(b) + "\n"
			case strings.HasPrefix(ct, "text/html"):
				attachHTML += string(b) + "\n"
			}
		}
	}
	if len(attachments) > 0 {
		e.Attachments = attachments
	}
	if strings.TrimSpace(text) == "" && strings.TrimSpace(htmlBody) == "" {
		text, htmlBody = attachText, attachHTML
	}
	e.Text = normalizeText(text)
	e.HTML = sanitizeHTML(htmlBody)
	if e.Text == "" && e.HTML != "" {
		e.Text = htmlToText(e.HTML)
	}
	// 控制缓存体积
	e.Text = truncate(e.Text, 60000)
	e.HTML = truncateBytes(e.HTML, 400000)
	e.Snippet = snippetOf(e.Text)
}

// 附件相关上限：解析元数据时单个附件最多统计这么多字节；
// 下载时整封邮件最多读取这么多字节。
const (
	maxAttachSize   = 10 * 1024 * 1024
	maxMessageBytes = 40 * 1024 * 1024
)

// sanitizeFilename 去掉路径分隔符与控制字符，避免把发件人给的文件名原样交给浏览器。
func sanitizeFilename(s string) string {
	s = strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', '"', '\r', '\n', '\t', 0:
			return -1
		}
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
	name := strings.TrimSpace(s)
	// 只由点组成（"." ".."）时没有实际文件名
	if strings.Trim(name, ".") == "" {
		return ""
	}
	if len(name) > 180 {
		name = name[:180]
	}
	return name
}

func defaultAttachmentName(ct string) string {
	ext := ""
	if t, _, err := mime.ParseMediaType(ct); err == nil {
		if exts, ok := attachmentExtByType[t]; ok {
			ext = exts
		}
	}
	return "附件" + ext
}

// attachmentExtByType 只覆盖常见类型，用于发件人没给文件名时兜底
var attachmentExtByType = map[string]string{
	"application/pdf": ".pdf",
	"application/zip": ".zip",
	"image/png":       ".png",
	"image/jpeg":      ".jpg",
	"image/gif":       ".gif",
	"text/plain":      ".txt",
	"text/csv":        ".csv",
}

// AttachmentData 是下载时取回的一段附件内容
type AttachmentData struct {
	Filename string
	MimeType string
	Bytes    []byte
}

// fetchAttachment 按 UID 重新取回整封邮件，解析出第 index 个附件分段。
func fetchAttachment(mb Mailbox, pass string, uid uint32, index int) (*AttachmentData, error) {
	c, err := imapLogin(mb, pass)
	if err != nil {
		return nil, err
	}
	defer c.Logout()
	c.Timeout = 60 * time.Second
	if _, err := c.Select("INBOX", true); err != nil {
		return nil, fmt.Errorf("打开收件箱失败: %w", err)
	}
	section := &imap.BodySectionName{Peek: true}
	set := new(imap.SeqSet)
	set.AddNum(uid)
	ch := make(chan *imap.Message, 4)
	done := make(chan error, 1)
	go func() {
		done <- c.UidFetch(set, []imap.FetchItem{section.FetchItem()}, ch)
	}()
	var raw []byte
	tooLarge := false
	for msg := range ch {
		r := msg.GetBody(section)
		if r == nil {
			continue
		}
		b, err := io.ReadAll(io.LimitReader(r, maxMessageBytes+1))
		if err != nil {
			continue
		}
		if len(b) > maxMessageBytes {
			tooLarge = true
			continue
		}
		raw = b
	}
	if err := <-done; err != nil {
		return nil, fmt.Errorf("读取邮件失败: %w", err)
	}
	if tooLarge {
		return nil, fmt.Errorf("邮件过大（超过 %dMB），请在邮箱客户端中下载附件", maxMessageBytes/1024/1024)
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("邮件已不在邮箱中，可能已被删除或移动")
	}
	return extractAttachmentPart(raw, index)
}

// extractAttachmentPart 从原始 MIME 中取出第 index 个附件分段（与 applyRawBody 的编号顺序一致）。
func extractAttachmentPart(raw []byte, index int) (*AttachmentData, error) {
	mr, _ := mail.CreateReader(bytes.NewReader(raw))
	if mr == nil {
		return nil, fmt.Errorf("无法解析邮件内容")
	}
	defer mr.Close()
	n := 0
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil && !message.IsUnknownCharset(err) && !message.IsUnknownEncoding(err) {
			break
		}
		if p == nil {
			continue
		}
		h, ok := p.Header.(*mail.AttachmentHeader)
		if !ok {
			continue
		}
		ct, _, _ := h.ContentType()
		if n == index {
			b, err := io.ReadAll(io.LimitReader(p.Body, maxAttachSize+1))
			if err != nil {
				return nil, fmt.Errorf("读取附件失败: %w", err)
			}
			if len(b) > maxAttachSize {
				return nil, fmt.Errorf("附件过大（超过 %dMB），请在邮箱客户端中下载", maxAttachSize/1024/1024)
			}
			name := ""
			if fn, ferr := h.Filename(); ferr == nil {
				name = sanitizeFilename(fn)
			}
			if name == "" {
				name = defaultAttachmentName(ct)
			}
			return &AttachmentData{Filename: name, MimeType: ct, Bytes: b}, nil
		}
		n++
		// 未读完的分段会把剩余字节丢弃，但不会影响后续分段，无需手动 drain
	}
	return nil, fmt.Errorf("附件不存在，邮件可能已更新")
}

var (
	reSelfClose = regexp.MustCompile(`(?is)<(script|style|iframe|object|embed|link|meta)[^>]*/?>`)
	reEventAttr = regexp.MustCompile(`(?is)\son[a-z]+\s*=\s*"[^"]*"|\son[a-z]+\s*=\s*'[^']*'|\son[a-z]+\s*=\s*[^\s>]+`)
	reJSURL     = regexp.MustCompile(`(?is)javascript:`)
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
	doc, err := xhtml.Parse(strings.NewReader(s))
	if err != nil {
		return ""
	}

	var out strings.Builder
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		switch n.Type {
		case xhtml.TextNode:
			out.WriteString(n.Data)
			return
		case xhtml.ElementNode:
			tag := strings.ToLower(n.Data)
			if hiddenHTMLNode(tag, n.Attr) {
				return
			}
			switch tag {
			case "br":
				out.WriteByte('\n')
				return
			case "td", "th":
				out.WriteByte(' ')
			}
			block := htmlBlockTags[tag]
			if block {
				out.WriteByte('\n')
			}
			for child := n.FirstChild; child != nil; child = child.NextSibling {
				walk(child)
			}
			if block {
				out.WriteByte('\n')
			}
			return
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	return normalizeText(out.String())
}

var htmlBlockTags = map[string]bool{
	"address": true, "article": true, "aside": true, "blockquote": true,
	"dd": true, "div": true, "dl": true, "dt": true, "fieldset": true,
	"figcaption": true, "figure": true, "footer": true, "form": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"header": true, "hr": true, "li": true, "main": true, "nav": true,
	"ol": true, "p": true, "pre": true, "section": true, "table": true,
	"tbody": true, "td": true, "tfoot": true, "th": true, "thead": true,
	"tr": true, "ul": true,
}

func hiddenHTMLNode(tag string, attrs []xhtml.Attribute) bool {
	switch tag {
	case "head", "script", "style", "template", "noscript", "iframe", "object", "embed", "svg", "canvas":
		return true
	}
	for _, attr := range attrs {
		switch strings.ToLower(attr.Key) {
		case "hidden":
			return true
		case "aria-hidden":
			if strings.EqualFold(strings.TrimSpace(attr.Val), "true") {
				return true
			}
		case "style":
			style := strings.ToLower(strings.ReplaceAll(attr.Val, " ", ""))
			if strings.Contains(style, "display:none") ||
				strings.Contains(style, "visibility:hidden") ||
				strings.Contains(style, "mso-hide:all") {
				return true
			}
		}
	}
	return false
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
