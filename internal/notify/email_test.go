package notify

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"fmt"
	"io"
	"math/big"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"strings"
	"sync"
	"testing"
	"time"
)

// testCert returns a certificate for 127.0.0.1 and a pool that trusts it.
func testCert(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "fake smtp"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, IsCA: true, BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, pool
}

type smtpMail struct {
	from   string
	to     []string
	data   string
	user   string
	secure bool
}

// A fakeSMTP is a mail server that keeps the mail it gets.
type fakeSMTP struct {
	ln   net.Listener
	cert tls.Certificate
	// implicit makes it speak TLS from the first byte, and offerTLS makes it
	// offer STARTTLS.
	implicit, offerTLS bool
	// login is the "user:password" it accepts, if it asks for one.
	login string
	// badQuit makes it refuse to say goodbye, after it took the mail.
	badQuit bool

	mu   sync.Mutex
	mail []smtpMail
}

func newFakeSMTP(t *testing.T, cert tls.Certificate, implicit, offerTLS bool, login string) *fakeSMTP {
	t.Helper()
	ln, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &fakeSMTP{ln: ln, cert: cert, implicit: implicit, offerTLS: offerTLS, login: login}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go s.serve(conn)
		}
	}()
	return s
}

func (s *fakeSMTP) port() int {
	addr, _ := s.ln.Addr().(*net.TCPAddr)
	return addr.Port
}

func (s *fakeSMTP) got() []smtpMail {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]smtpMail(nil), s.mail...)
}

func (s *fakeSMTP) serve(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	secure := false
	if s.implicit {
		conn, secure = tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{s.cert}}), true
	}
	r := bufio.NewReader(conn)
	say := func(line string) { _, _ = io.WriteString(conn, line+"\r\n") }
	say("220 fake ESMTP")
	var cur smtpMail
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		verb, arg, _ := strings.Cut(line, " ")
		switch strings.ToUpper(verb) {
		case "EHLO", "HELO":
			say("250-fake")
			if s.offerTLS && !secure {
				say("250-STARTTLS")
			}
			if s.login != "" {
				say("250-AUTH PLAIN")
			}
			say("250 8BITMIME")
		case "STARTTLS":
			say("220 go ahead")
			conn = tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{s.cert}})
			r, secure = bufio.NewReader(conn), true
		case "AUTH":
			_, enc, _ := strings.Cut(arg, " ")
			raw, _ := base64.StdEncoding.DecodeString(enc)
			parts := strings.Split(string(raw), "\x00")
			if len(parts) != 3 || parts[1]+":"+parts[2] != s.login {
				say("535 5.7.8 nope")
				continue
			}
			cur.user = parts[1]
			say("235 ok")
		case "MAIL":
			from, _, _ := strings.Cut(strings.TrimPrefix(arg[len("FROM:"):], "<"), ">")
			cur.from = from
			say("250 ok")
		case "RCPT":
			cur.to = append(cur.to, strings.Trim(arg[len("TO:"):], "<>"))
			say("250 ok")
		case "DATA":
			say("354 go on")
			var data strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				data.WriteString(strings.TrimPrefix(l, "."))
			}
			cur.data, cur.secure = data.String(), secure
			s.mu.Lock()
			s.mail = append(s.mail, cur)
			s.mu.Unlock()
			cur = smtpMail{user: cur.user}
			say("250 queued")
		case "QUIT":
			if s.badQuit {
				say("421 go away")
				return
			}
			say("221 bye")
			return
		default:
			say("502 what")
		}
	}
}

// parse returns the headers of m's mail, and its decoded body.
func (m smtpMail) parse(t *testing.T) (mail.Header, string) {
	t.Helper()
	msg, err := mail.ReadMessage(strings.NewReader(m.data))
	if err != nil {
		t.Fatalf("the mail doesn't parse: %v\n%s", err, m.data)
	}
	body, err := io.ReadAll(quotedprintable.NewReader(msg.Body))
	if err != nil {
		t.Fatal(err)
	}
	return msg.Header, string(body)
}

func TestEmail(t *testing.T) {
	t.Parallel()
	cert, pool := testCert(t)
	env := NewEnv(nil)
	env.TLS = &tls.Config{RootCAs: pool}
	base := Config{Host: "127.0.0.1", From: "bield@example.com", To: []string{"alice@example.com", "bob@example.com"}}

	t.Run("STARTTLS and a login", func(t *testing.T) {
		t.Parallel()
		srv := newFakeSMTP(t, cert, false, true, "bield:pw")
		cfg := base
		cfg.Port, cfg.Username, cfg.Password = srv.port(), "bield", "pw"
		m := opened()
		m.Title = "Disk 96% full on café\r\nBcc: evil@example.com"
		if err := (email{cfg, env}).Send(t.Context(), m); err != nil {
			t.Fatalf("Send() error = %v", err)
		}
		got := srv.got()
		if len(got) != 1 {
			t.Fatalf("the server got %d mails, want 1", len(got))
		}
		if !got[0].secure || got[0].user != "bield" || got[0].from != "bield@example.com" || strings.Join(got[0].to, ",") != "alice@example.com,bob@example.com" {
			t.Errorf("mail = %+v, want it sent over TLS, signed in, to both", got[0])
		}
		h, body := got[0].parse(t)
		subject, err := new(mime.WordDecoder).DecodeHeader(h.Get("Subject"))
		if err != nil || subject != "Critical: Disk 96% full on café Bcc: evil@example.com" {
			t.Errorf("Subject = %q, %v", subject, err)
		}
		if h.Get("Bcc") != "" {
			t.Error("a line break in the title added a header")
		}
		from, _ := mail.ParseAddress(h.Get("From"))
		if from == nil || from.Address != "bield@example.com" || from.Name != "Home lab" {
			t.Errorf("From = %q, want Home lab <bield@example.com>", h.Get("From"))
		}
		if h.Get("To") != "<alice@example.com>, <bob@example.com>" || h.Get("Content-Type") != "text/plain; charset=utf-8" || h.Get("Message-ID") == "" || h.Get("Date") == "" {
			t.Errorf("headers = %v", h)
		}
		if !strings.Contains(body, "12 GB left on /") || !strings.Contains(body, "https://bield.example.ts.net/alerts?alert=12") {
			t.Errorf("body = %q", body)
		}
	})

	t.Run("TLS from the first byte", func(t *testing.T) {
		t.Parallel()
		srv := newFakeSMTP(t, cert, true, false, "")
		cfg := base
		cfg.Port, cfg.Security = srv.port(), TLS
		if err := (email{cfg, env}).Send(t.Context(), opened()); err != nil {
			t.Fatalf("Send() error = %v", err)
		}
		if got := srv.got(); len(got) != 1 || !got[0].secure {
			t.Errorf("mail = %+v, want one over TLS", got)
		}
	})

	t.Run("a server on this machine in the clear", func(t *testing.T) {
		t.Parallel()
		srv := newFakeSMTP(t, cert, false, false, "")
		cfg := base
		cfg.Port, cfg.Security = srv.port(), Plain
		if err := (email{cfg, env}).Send(t.Context(), opened()); err != nil {
			t.Fatalf("Send() error = %v", err)
		}
		if got := srv.got(); len(got) != 1 || got[0].secure {
			t.Errorf("mail = %+v, want one in the clear", got)
		}
	})

	t.Run("a server that can't do STARTTLS gets nothing", func(t *testing.T) {
		t.Parallel()
		srv := newFakeSMTP(t, cert, false, false, "bield:pw")
		cfg := base
		cfg.Port, cfg.Username, cfg.Password = srv.port(), "bield", "pw"
		err := (email{cfg, env}).Send(t.Context(), opened())
		if err == nil || !strings.Contains(err.Error(), "STARTTLS") {
			t.Errorf("Send() error = %v, want one that says the server has no STARTTLS", err)
		}
		if got := srv.got(); len(got) != 0 {
			t.Errorf("the server got %+v, want nothing", got)
		}
	})

	t.Run("a server that is rude after taking the mail", func(t *testing.T) {
		t.Parallel()
		srv := newFakeSMTP(t, cert, false, true, "")
		srv.badQuit = true
		cfg := base
		cfg.Port = srv.port()
		// The mail is delivered, so it didn't fail, and isn't sent again.
		if err := (email{cfg, env}).Send(t.Context(), opened()); err != nil {
			t.Errorf("Send() error = %v, want none: the server took the mail", err)
		}
		if got := srv.got(); len(got) != 1 {
			t.Errorf("the server got %d mails, want 1", len(got))
		}
	})

	t.Run("a wrong password", func(t *testing.T) {
		t.Parallel()
		srv := newFakeSMTP(t, cert, false, true, "bield:pw")
		cfg := base
		cfg.Port, cfg.Username, cfg.Password = srv.port(), "bield", "wrong"
		err := (email{cfg, env}).Send(t.Context(), opened())
		if err == nil || Reason(err) != "the mail server answered 535" {
			t.Errorf("Send() error = %v, reason %q, want the mail server's 535", err, Reason(err))
		}
		if strings.Contains(fmt.Sprint(err), "wrong") {
			t.Errorf("Send() error = %q, which has the password in it", err)
		}
	})

	t.Run("a certificate that isn't trusted", func(t *testing.T) {
		t.Parallel()
		srv := newFakeSMTP(t, cert, false, true, "")
		cfg := base
		cfg.Port = srv.port()
		err := (email{cfg, NewEnv(nil)}).Send(t.Context(), opened())
		if err == nil || Reason(err) != "the server's certificate isn't trusted" {
			t.Errorf("Send() error = %v, reason %q, want an untrusted certificate", err, Reason(err))
		}
		if got := srv.got(); len(got) != 0 {
			t.Errorf("the server got %+v, want nothing", got)
		}
	})
}

func TestConfig_port(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		cfg  Config
		want int
	}{
		{Config{}, 587},
		{Config{Security: StartTLS}, 587},
		{Config{Security: TLS}, 465},
		{Config{Security: Plain}, 25},
		{Config{Security: TLS, Port: 99}, 99},
	} {
		if got := tt.cfg.port(); got != tt.want {
			t.Errorf("port() of %q %d = %d, want %d", tt.cfg.Security, tt.cfg.Port, got, tt.want)
		}
	}
}
