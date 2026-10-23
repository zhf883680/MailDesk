package main

import (
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/smtp"
	"strings"
	"time"
)

type loginAuth struct {
	username, password, host string
}

func (a *loginAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	if !server.TLS && !isLocalHost(server.Name) {
		return "", nil, errors.New("未加密的连接不允许发送密码")
	}
	return "LOGIN", nil, nil
}

func (a *loginAuth) Next(fromServer []byte, more bool) ([]byte, error) {
	if !more {
		return nil, nil
	}
	prompt := strings.ToLower(string(fromServer))
	switch {
	case strings.Contains(prompt, "username"):
		return []byte(a.username), nil
	case strings.Contains(prompt, "password"):
		return []byte(a.password), nil
	}
	return nil, fmt.Errorf("未知的 SMTP 认证挑战: %s", fromServer)
}

func isLocalHost(h string) bool {
	h = strings.ToLower(h)
	return h == "localhost" || h == "127.0.0.1" || h == "::1"
}

func smtpClient(mb Mailbox) (*smtp.Client, error) {
	host := strings.TrimSpace(mb.SMTPHost)
	port := mb.SMTPPort
	if port == 0 {
		port = 587
	}
	addr := net.JoinHostPort(host, fmt.Sprint(port))
	tlsCfg := &tls.Config{ServerName: host}
	dialer := &net.Dialer{Timeout: 25 * time.Second}
	if mb.SMTPTLS {
		conn, err := tls.DialWithDialer(dialer, "tcp", addr, tlsCfg)
		if err != nil {
			return nil, fmt.Errorf("连接 SMTP 服务器失败: %w", err)
		}
		return smtp.NewClient(conn, host)
	}
	conn, err := dialer.Dial("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("连接 SMTP 服务器失败: %w", err)
	}
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		return nil, err
	}
	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(tlsCfg); err != nil {
			c.Close()
			return nil, fmt.Errorf("SMTP STARTTLS 失败: %w", err)
		}
	}
	return c, nil
}

func smtpAuth(c *smtp.Client, mb Mailbox, pass string) error {
	user := mb.Username
	if user == "" {
		user = mb.Email
	}
	if ok, mechs := c.Extension("AUTH"); ok {
		upper := strings.ToUpper(mechs)
		if !strings.Contains(upper, "PLAIN") && strings.Contains(upper, "LOGIN") {
			return c.Auth(&loginAuth{username: user, password: pass, host: mb.SMTPHost})
		}
		return c.Auth(smtp.PlainAuth("", user, pass, mb.SMTPHost))
	}
	return nil
}

func testSMTP(mb Mailbox, pass string) error {
	c, err := smtpClient(mb)
	if err != nil {
		return err
	}
	defer c.Close()
	if err := smtpAuth(c, mb, pass); err != nil {
		return fmt.Errorf("SMTP 登录失败: %w", err)
	}
	return c.Quit()
}

type OutgoingMail struct {
	To      []string
	Cc      []string
	Subject string
	Body    string
}

func sendMail(mb Mailbox, pass string, out OutgoingMail) error {
	if len(out.To) == 0 {
		return errors.New("请填写收件人")
	}
	c, err := smtpClient(mb)
	if err != nil {
		return err
	}
	defer c.Close()
	if err := smtpAuth(c, mb, pass); err != nil {
		return fmt.Errorf("SMTP 认证失败: %w", err)
	}
	if err := c.Mail(mb.Email); err != nil {
		return err
	}
	for _, addr := range append(append([]string{}, out.To...), out.Cc...) {
		if err := c.Rcpt(strings.TrimSpace(addr)); err != nil {
			return fmt.Errorf("收件人 %s 被拒绝: %w", addr, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(buildMIME(mb, out)); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

func buildMIME(mb Mailbox, out OutgoingMail) []byte {
	var b strings.Builder
	boundary := "maildesk_" + randHex(8)
	writeHeader := func(k, v string) {
		b.WriteString(k + ": " + mime.QEncoding.Encode("utf-8", v) + "\r\n")
	}
	writeHeader("From", fmt.Sprintf("%s <%s>", mime.QEncoding.Encode("utf-8", mb.Name), mb.Email))
	writeHeader("To", strings.Join(out.To, ", "))
	if len(out.Cc) > 0 {
		writeHeader("Cc", strings.Join(out.Cc, ", "))
	}
	writeHeader("Subject", out.Subject)
	writeHeader("Date", time.Now().Format(time.RFC1123Z))
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: multipart/alternative; boundary=\"" + boundary + "\"\r\n\r\n")

	plain := out.Body
	b.WriteString("--" + boundary + "\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("Content-Transfer-Encoding: 8bit\r\n\r\n")
	b.WriteString(plain + "\r\n")

	b.WriteString("--" + boundary + "\r\n")
	b.WriteString("Content-Type: text/html; charset=utf-8\r\n")
	b.WriteString("Content-Transfer-Encoding: 8bit\r\n\r\n")
	b.WriteString(textToHTML(plain) + "\r\n")
	b.WriteString("--" + boundary + "--\r\n")
	return []byte(b.String())
}

func textToHTML(s string) string {
	esc := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
	parts := strings.Split(strings.ReplaceAll(esc, "\r\n", "\n"), "\n")
	var b strings.Builder
	b.WriteString(`<div style="font-family:-apple-system,BlinkMacSystemFont,'Segoe UI','PingFang SC',sans-serif;font-size:14px;line-height:1.75;color:#222">`)
	for _, p := range parts {
		if strings.TrimSpace(p) == "" {
			b.WriteString("<div style=\"height:12px\"></div>")
			continue
		}
		b.WriteString("<p style=\"margin:0 0 10px\">" + p + "</p>")
	}
	b.WriteString("</div>")
	return b.String()
}
