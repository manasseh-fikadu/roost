package notify

import (
	"bytes"
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
)

// email sends mail through an SMTP server.
type email struct {
	cfg Config
	env *Env
}

// port returns the port to connect to.
func (c Config) port() int {
	switch {
	case c.Port != 0:
		return c.Port
	case c.Security == TLS:
		return 465
	case c.Security == Plain:
		return 25
	}
	return 587
}

func (e email) Send(ctx context.Context, m Message) error {
	from, err := mail.ParseAddress(e.cfg.From)
	if err != nil {
		return fmt.Errorf("%w: the from address: %w", errBadConfig, err)
	}
	to, header := make([]string, len(e.cfg.To)), make([]string, len(e.cfg.To))
	for i, a := range e.cfg.To {
		addr, err := mail.ParseAddress(a)
		if err != nil {
			return fmt.Errorf("%w: an address to send to: %w", errBadConfig, err)
		}
		to[i], header[i] = addr.Address, addr.String()
	}
	msg, err := e.build(m, from, header)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, SendTimeout)
	defer cancel()
	addr := net.JoinHostPort(e.cfg.Host, strconv.Itoa(e.cfg.port()))
	conn, err := e.env.Dial(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	// Closing the connection is how a context that ends stops the exchange.
	defer context.AfterFunc(ctx, func() { _ = conn.Close() })()
	defer func() { _ = conn.Close() }()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	tlsConfig := e.env.TLS.Clone()
	if tlsConfig == nil {
		tlsConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	tlsConfig.ServerName = e.cfg.Host
	if e.cfg.Security == TLS {
		secure := tls.Client(conn, tlsConfig)
		if err := secure.HandshakeContext(ctx); err != nil {
			return err
		}
		conn = secure
	}

	c, err := smtp.NewClient(conn, e.cfg.Host)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()
	if e.cfg.Security == "" || e.cfg.Security == StartTLS {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return errors.New("the mail server doesn't offer STARTTLS, so Bield won't send mail or a password to it in the clear")
		}
		if err := c.StartTLS(tlsConfig); err != nil {
			return err
		}
	}
	if e.cfg.Username != "" {
		if err := c.Auth(smtp.PlainAuth("", e.cfg.Username, e.cfg.Password, e.cfg.Host)); err != nil {
			return err
		}
	}
	if err := c.Mail(from.Address); err != nil {
		return err
	}
	for _, a := range to {
		if err := c.Rcpt(a); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	// The server has accepted the mail. A QUIT that goes wrong now doesn't
	// undo that, and saying it failed would send the mail again.
	_ = c.Quit()
	return nil
}

// build returns the mail for m, from an address to the addresses in the To header.
func (e email) build(m Message, from *mail.Address, to []string) ([]byte, error) {
	var id [12]byte
	_, _ = rand.Read(id[:]) // crypto/rand never fails
	domain := "bield.invalid"
	if _, host, ok := strings.Cut(from.Address, "@"); ok && host != "" {
		domain = host
	}
	// A display name with a non-ASCII character in it is encoded by String.
	if from.Name == "" {
		from.Name = oneLine(m.Instance)
	}
	var buf bytes.Buffer
	for _, h := range [][2]string{
		{"From", from.String()},
		{"To", strings.Join(to, ", ")},
		{"Subject", mime.QEncoding.Encode("utf-8", trunc(oneLine(m.Subject()), 200))},
		{"Date", m.At.Format(time.RFC1123Z)},
		{"Message-ID", "<" + hex.EncodeToString(id[:]) + "@" + domain + ">"},
		{"MIME-Version", "1.0"},
		{"Content-Type", "text/plain; charset=utf-8"},
		{"Content-Transfer-Encoding", "quoted-printable"},
		{"Auto-Submitted", "auto-generated"},
	} {
		buf.WriteString(h[0] + ": " + h[1] + "\r\n")
	}
	buf.WriteString("\r\n")
	body := quotedprintable.NewWriter(&buf)
	if _, err := body.Write([]byte(m.Text() + "\n")); err != nil {
		return nil, err
	}
	if err := body.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
