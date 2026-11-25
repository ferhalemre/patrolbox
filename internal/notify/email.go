package notify

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/ferhalemre/patrolbox/internal/config"
)

type email struct{ cfg config.Email }

func (*email) Name() string { return "email" }

const emailTimeout = 30 * time.Second

func (e *email) Send(ctx context.Context, m Message) error {
	c := e.cfg
	if c.Host == "" || len(c.To) == 0 {
		return errors.New("email: host and to are required")
	}
	fromRaw := c.From
	if fromRaw == "" && strings.Contains(c.Username, "@") {
		fromRaw = c.Username
	}
	from, err := mail.ParseAddress(fromRaw)
	if err != nil {
		return fmt.Errorf("email: bad from address: %w", err)
	}
	var to []*mail.Address
	for _, t := range c.To {
		a, err := mail.ParseAddress(t)
		if err != nil {
			return fmt.Errorf("email: bad to address: %w", err)
		}
		to = append(to, a)
	}
	port := c.Port
	if port == 0 {
		port = 587
	}
	msg, err := buildMessage(from, to, m, time.Now())
	if err != nil {
		return err
	}

	deadline := time.Now().Add(emailTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	addr := net.JoinHostPort(c.Host, strconv.Itoa(port))
	dialer := &net.Dialer{Deadline: deadline}
	tlsCfg := &tls.Config{ServerName: c.Host, MinVersion: tls.VersionTLS12}

	var conn net.Conn
	secure := false
	if port == 465 {
		conn, err = (&tls.Dialer{NetDialer: dialer, Config: tlsCfg}).DialContext(ctx, "tcp", addr)
		secure = true
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("email: connect %s: %w", addr, err)
	}
	defer conn.Close()
	conn.SetDeadline(deadline)
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()

	cl, err := smtp.NewClient(conn, c.Host)
	if err != nil {
		return fmt.Errorf("email: %w", err)
	}
	defer cl.Close()
	if !secure {
		if ok, _ := cl.Extension("STARTTLS"); ok {
			if err := cl.StartTLS(tlsCfg); err != nil {
				return fmt.Errorf("email: starttls: %w", err)
			}
			secure = true
		}
	}
	if c.Username != "" {
		if !secure {
			return errors.New("email: server offers no TLS; refusing to send the password in clear text")
		}
		if err := cl.Auth(smtp.PlainAuth("", c.Username, c.Password, c.Host)); err != nil {
			return fmt.Errorf("email: auth: %w", err)
		}
	}
	if err := cl.Mail(from.Address); err != nil {
		return fmt.Errorf("email: MAIL FROM: %w", err)
	}
	for _, a := range to {
		if err := cl.Rcpt(a.Address); err != nil {
			return fmt.Errorf("email: RCPT TO %s: %w", a.Address, err)
		}
	}
	w, err := cl.Data()
	if err != nil {
		return fmt.Errorf("email: DATA: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		return fmt.Errorf("email: DATA: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("email: DATA: %w", err)
	}
	cl.Quit()
	return nil
}

// buildMessage renders an RFC 5322 message with CRLF line endings.
func buildMessage(from *mail.Address, to []*mail.Address, m Message, now time.Time) ([]byte, error) {
	var id [12]byte
	if _, err := rand.Read(id[:]); err != nil {
		return nil, err
	}
	domain := "localhost"
	if i := strings.LastIndex(from.Address, "@"); i >= 0 {
		domain = from.Address[i+1:]
	}
	var rcpt []string
	for _, a := range to {
		rcpt = append(rcpt, a.String())
	}
	subject := strings.Join(strings.Fields(m.Title), " ") // no CR/LF in a header

	var b strings.Builder
	h := func(k, v string) { b.WriteString(k + ": " + v + "\r\n") }
	h("From", from.String())
	h("To", strings.Join(rcpt, ", "))
	h("Subject", mime.QEncoding.Encode("utf-8", subject))
	h("Date", now.Format(time.RFC1123Z))
	h("Message-ID", "<"+hex.EncodeToString(id[:])+"@"+domain+">")
	h("MIME-Version", "1.0")
	h("Content-Type", "text/plain; charset=utf-8")
	h("Content-Transfer-Encoding", "quoted-printable")
	b.WriteString("\r\n")

	qp := quotedprintable.NewWriter(&b)
	body := strings.ReplaceAll(m.Body, "\r\n", "\n")
	if _, err := qp.Write([]byte(body)); err != nil { // writer turns "\n" into CRLF
		return nil, err
	}
	if err := qp.Close(); err != nil {
		return nil, err
	}
	return []byte(b.String()), nil
}
