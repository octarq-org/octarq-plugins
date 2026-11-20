package mail

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/smtp"
	"strings"
	"time"

	"github.com/octarq-org/octarq/server/plugin/safehttp"
)

func stripCRLF(s string) string {
	return strings.NewReplacer("\r", "", "\n", "", "\x00", "").Replace(s)
}

type Message struct {
	From    string   `json:"from,omitempty"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	Text    string   `json:"text,omitempty"`
	HTML    string   `json:"html,omitempty"`
}

type Sender interface {
	Send(m Message) error
}

type SMTPSender struct {
	host, port, user, pass, from string
}

func NewCustomSender(host, port, user, pass, from string) Sender {
	return &SMTPSender{
		host: host, port: port,
		user: user, pass: pass, from: from,
	}
}

func (s *SMTPSender) Send(m Message) error {
	from := m.From
	if from == "" {
		from = s.from
	}
	to := make([]string, len(m.To))
	for i, addr := range m.To {
		to[i] = stripCRLF(addr)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", stripCRLF(from))
	fmt.Fprintf(&b, "To: %s\r\n", strings.Join(to, ", "))
	fmt.Fprintf(&b, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", m.Subject))
	fmt.Fprintf(&b, "MIME-Version: 1.0\r\n")
	if m.HTML != "" {
		fmt.Fprintf(&b, "Content-Type: text/html; charset=UTF-8\r\n\r\n%s", m.HTML)
	} else {
		fmt.Fprintf(&b, "Content-Type: text/plain; charset=UTF-8\r\n\r\n%s", m.Text)
	}
	addr := net.JoinHostPort(s.host, s.port)
	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
		Control: safehttp.SMTPControl,
	}
	conn, err := dialer.Dial("tcp", addr)
	if err != nil {
		return err
	}
	if err := conn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		conn.Close()
		return err
	}
	c, err := smtp.NewClient(conn, s.host)
	if err != nil {
		conn.Close()
		return err
	}
	defer c.Close()

	if ok, _ := c.Extension("STARTTLS"); ok {
		config := &tls.Config{ServerName: s.host}
		if err := c.StartTLS(config); err != nil {
			return err
		}
	}

	if s.user != "" {
		if ok, _ := c.Extension("AUTH"); !ok {
			return errors.New("smtp: server doesn't support AUTH")
		}
		auth := smtp.PlainAuth("", s.user, s.pass, s.host)
		if err := c.Auth(auth); err != nil {
			return err
		}
	}

	if err := c.Mail(stripCRLF(from)); err != nil {
		return err
	}
	for _, rec := range to {
		if err := c.Rcpt(rec); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	_, err = w.Write([]byte(b.String()))
	if err != nil {
		if closeErr := w.Close(); closeErr != nil {
			slog.Warn("mail: close writer error on failed write", "error", closeErr)
		}
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

type ResendSender struct {
	apiKey string
	from   string
	client *http.Client
}

func NewResendSender(apiKey, from string) Sender {
	return &ResendSender{
		apiKey: apiKey,
		from:   from,
		client: safehttp.NewClient(10 * time.Second),
	}
}

func (s *ResendSender) Send(m Message) error {
	from := m.From
	if from == "" {
		from = s.from
	}
	payload := map[string]any{
		"from":    stripCRLF(from),
		"to":      m.To,
		"subject": m.Subject,
	}
	if m.HTML != "" {
		payload["html"] = m.HTML
	} else {
		payload["text"] = m.Text
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", "https://api.resend.com/emails", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("resend: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("resend error (%d): %s", resp.StatusCode, string(respBody))
	}
	return nil
}
