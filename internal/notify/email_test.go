package notify

import (
	"bufio"
	"context"
	"io"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ferhalemre/patrolbox/internal/config"
)

// fakeSMTP is a minimal plain-text SMTP server that records one transaction.
type fakeSMTP struct {
	ln   net.Listener
	done chan struct{}
	mu   sync.Mutex
	from string
	rcpt []string
	data string
	cmds []string
}

func startSMTP(t *testing.T) *fakeSMTP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &fakeSMTP{ln: ln, done: make(chan struct{})}
	t.Cleanup(func() { ln.Close() })
	go func() {
		defer close(s.done)
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		c.SetDeadline(time.Now().Add(10 * time.Second))
		s.serve(c)
	}()
	return s
}

func (s *fakeSMTP) serve(c net.Conn) {
	r := bufio.NewReader(c)
	w := func(l string) { io.WriteString(c, l+"\r\n") }
	w("220 fake ESMTP")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		s.mu.Lock()
		s.cmds = append(s.cmds, line)
		s.mu.Unlock()
		up := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(up, "EHLO"):
			w("250-fake")
			w("250 8BITMIME")
		case strings.HasPrefix(up, "MAIL FROM:"):
			s.mu.Lock()
			s.from = strings.Fields(line[len("MAIL FROM:"):])[0]
			s.mu.Unlock()
			w("250 ok")
		case strings.HasPrefix(up, "RCPT TO:"):
			s.mu.Lock()
			s.rcpt = append(s.rcpt, line[len("RCPT TO:"):])
			s.mu.Unlock()
			w("250 ok")
		case up == "DATA":
			w("354 go")
			var sb strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				sb.WriteString(strings.TrimPrefix(l, ".")) // undo dot-stuffing
			}
			s.mu.Lock()
			s.data = sb.String()
			s.mu.Unlock()
			w("250 queued")
		case up == "QUIT":
			w("221 bye")
			return
		default:
			w("250 ok")
		}
	}
}

func (s *fakeSMTP) port() int { return s.ln.Addr().(*net.TCPAddr).Port }

func (s *fakeSMTP) wait(t *testing.T) {
	t.Helper()
	select {
	case <-s.done:
	case <-time.After(5 * time.Second):
		t.Fatal("fake smtp did not finish")
	}
}

func TestEmailSend(t *testing.T) {
	s := startSMTP(t)
	ch := Channels(config.Notify{Email: config.Email{
		Host: "127.0.0.1", Port: s.port(),
		From: "patrolbox <patrol@example.com>",
		To:   []string{"ops@example.com", "Ayşe <ayse@example.org>"},
	}})[0]
	title := "patrolbox: 2 sorun — şimdi"
	body := "güncel durum: çalışmıyor\nПривет 你好\n.\nlast line"
	if err := ch.Send(context.Background(), Message{Title: title, Body: body, Level: "problem"}); err != nil {
		t.Fatal(err)
	}
	s.wait(t)

	if s.from != "<patrol@example.com>" {
		t.Fatalf("MAIL FROM %q", s.from)
	}
	if len(s.rcpt) != 2 || s.rcpt[0] != "<ops@example.com>" || s.rcpt[1] != "<ayse@example.org>" {
		t.Fatalf("rcpt %v", s.rcpt)
	}
	for _, c := range s.cmds {
		if strings.HasPrefix(strings.ToUpper(c), "AUTH") || strings.HasPrefix(strings.ToUpper(c), "STARTTLS") {
			t.Fatalf("unexpected command %q", c)
		}
	}

	// every line ends in CRLF, no bare LF
	if strings.Contains(strings.ReplaceAll(s.data, "\r\n", ""), "\n") {
		t.Fatalf("bare LF in message: %q", s.data)
	}
	msg, err := mail.ReadMessage(strings.NewReader(s.data))
	if err != nil {
		t.Fatal(err)
	}
	h := msg.Header
	if h.Get("MIME-Version") != "1.0" {
		t.Fatalf("MIME-Version %q", h.Get("MIME-Version"))
	}
	if ct := h.Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Fatalf("Content-Type %q", ct)
	}
	if h.Get("Content-Transfer-Encoding") != "quoted-printable" {
		t.Fatalf("CTE %q", h.Get("Content-Transfer-Encoding"))
	}
	rawSubj := h.Get("Subject")
	if !strings.HasPrefix(rawSubj, "=?utf-8?q?") {
		t.Fatalf("subject not RFC 2047: %q", rawSubj)
	}
	for _, r := range rawSubj {
		if r > 127 {
			t.Fatalf("non-ASCII in raw subject %q", rawSubj)
		}
	}
	subj, err := new(mime.WordDecoder).DecodeHeader(rawSubj)
	if err != nil || subj != title {
		t.Fatalf("subject = %q, %v", subj, err)
	}
	if _, err := h.Date(); err != nil {
		t.Fatalf("Date: %v", err)
	}
	if id := h.Get("Message-ID"); !strings.HasPrefix(id, "<") || !strings.HasSuffix(id, "@example.com>") {
		t.Fatalf("Message-ID %q", id)
	}
	from, err := h.AddressList("From")
	if err != nil || from[0].Address != "patrol@example.com" || from[0].Name != "patrolbox" {
		t.Fatalf("From %v %v", from, err)
	}
	to, err := h.AddressList("To")
	if err != nil || len(to) != 2 || to[1].Name != "Ayşe" || to[1].Address != "ayse@example.org" {
		t.Fatalf("To %v %v", to, err)
	}

	raw, err := io.ReadAll(msg.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(raw), "\r\n") && strings.Contains(string(raw), "\n") {
		t.Fatalf("body not CRLF: %q", raw)
	}
	dec, err := io.ReadAll(quotedprintable.NewReader(strings.NewReader(string(raw))))
	if err != nil {
		t.Fatal(err)
	}
	got := strings.ReplaceAll(string(dec), "\r\n", "\n")
	if strings.TrimRight(got, "\n") != body {
		t.Fatalf("body = %q, want %q", got, body)
	}
}

func TestEmailDefaultPortAndValidation(t *testing.T) {
	if _, err := mail.ParseAddress("patrol@example.com"); err != nil {
		t.Fatal(err)
	}
	// no host/to configured
	err := (&email{cfg: config.Email{From: "a@b.c"}}).Send(context.Background(), Message{Title: "t"})
	if err == nil {
		t.Fatal("want validation error")
	}
	// bad from
	err = (&email{cfg: config.Email{Host: "127.0.0.1", From: "nope", To: []string{"a@b.c"}}}).Send(context.Background(), Message{Title: "t"})
	if err == nil {
		t.Fatal("want bad-from error")
	}
}

func TestEmailRefusesPasswordWithoutTLS(t *testing.T) {
	s := startSMTP(t)
	ch := &email{cfg: config.Email{
		Host: "127.0.0.1", Port: s.port(), Username: "u", Password: "pw-secret",
		From: "a@example.com", To: []string{"b@example.com"},
	}}
	err := ch.Send(context.Background(), Message{Title: "t", Body: "b"})
	if err == nil || !strings.Contains(err.Error(), "TLS") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "pw-secret") {
		t.Fatal("password leaked")
	}
	for _, c := range s.cmds {
		if strings.HasPrefix(strings.ToUpper(c), "AUTH") {
			t.Fatalf("AUTH sent in clear: %q", c)
		}
	}
}

func TestEmailConnectRefused(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	ch := &email{cfg: config.Email{Host: "127.0.0.1", Port: port, From: "a@example.com", To: []string{"b@example.com"}}}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := ch.Send(ctx, Message{Title: "t"})
	if err == nil || !strings.Contains(err.Error(), strconv.Itoa(port)) {
		t.Fatalf("err = %v", err)
	}
}
