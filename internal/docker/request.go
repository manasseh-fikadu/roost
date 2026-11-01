package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"time"
)

var (
	// ErrNoDocker means Bield knows of no Docker to ask on a machine.
	ErrNoDocker = errors.New("no Docker socket was found on the machine")
	// ErrUnreachable means Docker didn't answer where Bield looked for it.
	ErrUnreachable = errors.New("can't reach Docker")
)

// An Error is a failure Docker answered with.
type Error struct {
	// Status is the HTTP status Docker answered with.
	Status int
	// Message is Docker's explanation, if it gave one.
	Message string
}

func (e *Error) Error() string {
	if e.Message == "" {
		return "docker answered " + strconv.Itoa(e.Status)
	}
	return "docker answered " + strconv.Itoa(e.Status) + ": " + e.Message
}

// ClientStatus returns the HTTP status to answer a request with when Docker
// answered it with e. A bad request, a missing container or a conflict, such
// as stopping one that can't stop, are the caller's to hear about. The
// daemon's own failures aren't, so they're a bad gateway.
func (e *Error) ClientStatus() int {
	switch e.Status {
	case http.StatusBadRequest, http.StatusNotFound, http.StatusConflict:
		return e.Status
	}
	return http.StatusBadGateway
}

// idPattern matches the names and IDs of containers: Docker's own rule for a
// name, which an ID also follows.
var idPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)

// ValidID reports whether s can be the name or ID of a container. It's
// checked before anything reaches a path of the Engine API, so ".." and
// other path tricks can't.
func ValidID(s string) bool { return idPattern.MatchString(s) }

const (
	// callTimeout is how long a request that returns JSON can take.
	callTimeout = 15 * time.Second
	// headerTimeout is how long a request that streams waits for Docker to
	// start answering.
	headerTimeout = 15 * time.Second
	// maxJSON is the most JSON Bield reads from one answer.
	maxJSON = 64 << 20
)

// request sends a request and returns the answer, which the caller closes.
// It fails with an *Error if Docker answers 400 or higher. A 304, which
// Docker answers when a container already is as asked, isn't a failure.
func (c *Client) request(ctx context.Context, method, path string, query url.Values, body any) (*http.Response, error) {
	var payload io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		payload = bytes.NewReader(b)
	}
	u := "http://docker" + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, payload)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ue, ok := errors.AsType[*url.Error](err); ok {
			err = ue.Err
		}
		return nil, err
	}
	if resp.StatusCode >= http.StatusBadRequest {
		defer func() { _ = resp.Body.Close() }()
		var e struct {
			Message string `json:"message"`
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		out := &Error{Status: resp.StatusCode}
		if json.Unmarshal(raw, &e) == nil {
			out.Message = e.Message
		}
		return nil, out
	}
	return resp, nil
}

// getJSON reads the JSON at path into v, within callTimeout.
func (c *Client) getJSON(ctx context.Context, path string, query url.Values, v any) error {
	return c.getJSONWithin(ctx, callTimeout, path, query, v)
}

// getJSONWithin reads the JSON at path into v, within d.
func (c *Client) getJSONWithin(ctx context.Context, d time.Duration, path string, query url.Values, v any) error {
	ctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	resp, err := c.request(ctx, http.MethodGet, path, query, nil)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxJSON)).Decode(v); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("read what docker answered for %s: %w", path, err)
	}
	return nil
}

// stream starts a request whose answer goes on, such as a container's logs,
// and returns it once Docker starts to answer. Canceling ctx, or closing the
// body, ends it.
func (c *Client) stream(ctx context.Context, path string, query url.Values) (*http.Response, error) {
	ctx, cancel := context.WithCancel(ctx)
	timer := time.AfterFunc(headerTimeout, cancel)
	resp, err := c.request(ctx, http.MethodGet, path, query, nil)
	if !timer.Stop() {
		// The timer canceled the request, whether or not it was answered.
		if err == nil {
			_ = resp.Body.Close()
		}
		cancel()
		return nil, context.DeadlineExceeded
	}
	if err != nil {
		cancel()
		return nil, err
	}
	resp.Body = &cancelBody{ReadCloser: resp.Body, cancel: cancel}
	return resp, nil
}

// cancelBody cancels a context when it's closed. It cancels first: a Read
// that waits for more of a stream holds the body, so closing it would wait for
// the Read, which would wait for the stream.
type cancelBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b *cancelBody) Close() error {
	b.cancel()
	return b.ReadCloser.Close()
}

// post sends a POST with no body and returns its status. It ends the request
// within d.
func (c *Client) post(ctx context.Context, d time.Duration, path string, query url.Values) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	resp, err := c.request(ctx, http.MethodPost, path, query, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, nil
}
