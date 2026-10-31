package notify

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"
)

// SendTimeout is how long one message has to be sent.
const SendTimeout = 20 * time.Second

// A Sender sends messages to one channel.
type Sender interface {
	// Send sends m. The error never has a secret in it, such as the
	// address of a webhook.
	Send(ctx context.Context, m Message) error
}

// An Env is what senders reach servers with. It's safe for concurrent use.
type Env struct {
	// Dial connects to servers. Roost passes the tailnet node's, which
	// reaches the tailnet's names and addresses as well as the internet.
	// If it's nil, Env dials from this machine.
	Dial DialFunc
	// Client is for the senders that speak HTTP.
	Client *http.Client
	// TLS, if it's set, is what TLS connections to mail servers start from.
	// It's for tests, which trust a certificate of their own.
	TLS *tls.Config
}

// NewEnv returns an Env that dials with dial, or from this machine if dial
// is nil.
func NewEnv(dial DialFunc) *Env {
	if dial == nil {
		dial = (&net.Dialer{Timeout: 10 * time.Second}).DialContext
	}
	return &Env{
		Dial: dial,
		Client: &http.Client{
			Timeout: SendTimeout,
			Transport: &http.Transport{
				DialContext:         dial,
				TLSHandshakeTimeout: 10 * time.Second,
				ForceAttemptHTTP2:   true,
				MaxIdleConns:        8,
				IdleConnTimeout:     30 * time.Second,
			},
			// A redirect would send a secret on to a place the admin never
			// named, and turn a POST into a GET.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

// NewSender returns the sender of c, which must pass Check.
func NewSender(c Channel, env *Env) (Sender, error) {
	if err := c.Check(); err != nil {
		return nil, fmt.Errorf("%w: %w", errBadConfig, err)
	}
	cfg := c.Config
	switch c.Kind {
	case Ntfy:
		return ntfy{cfg, env}, nil
	case Discord:
		return discord{cfg, env}, nil
	case Slack:
		return slack{cfg, env}, nil
	case Telegram:
		return telegram{cfg, env, "https://api.telegram.org"}, nil
	case Email:
		return email{cfg, env}, nil
	case Webhook:
		return webhook{cfg, env}, nil
	}
	return nil, fmt.Errorf("%w: unknown kind %q", errBadConfig, c.Kind)
}

// maxAnswer is how much of a server's answer is kept to say what went wrong.
const maxAnswer = 300

// postJSON posts body, as JSON, to address, with the headers, and fails with
// a *StatusError unless the server answers with a success.
func (e *Env) postJSON(ctx context.Context, address string, headers map[string]string, body any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, address, bytes.NewReader(raw))
	if err != nil {
		return scrub(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Roost")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := e.Client.Do(req)
	if err != nil {
		return scrub(err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 200 && res.StatusCode < 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<16))
		return nil
	}
	answer, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
	return &StatusError{Code: res.StatusCode, Body: trunc(oneLine(string(answer)), maxAnswer)}
}

// scrub returns err without the address of the request in it, since the
// address of a webhook is a secret.
func scrub(err error) error {
	if uerr, ok := errors.AsType[*url.Error](err); ok {
		return uerr.Err
	}
	return err
}

// bearer returns the headers that send token, or none if there is none.
func bearer(token string) map[string]string {
	if token == "" {
		return nil
	}
	return map[string]string{"Authorization": "Bearer " + token}
}
