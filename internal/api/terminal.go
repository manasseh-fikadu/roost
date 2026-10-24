package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/manasseh-fikadu/roost/internal/ssh"
)

const (
	// maxTerminalMessage is the largest message the browser can send, such
	// as a big paste.
	maxTerminalMessage = 1 << 20
	// terminalPing is how often the hub pings a terminal's WebSocket, so
	// proxies and NATs keep a quiet one open, and how long it waits for
	// the pong.
	terminalPing = 25 * time.Second
	// outputChunk is the most output one binary frame carries.
	outputChunk = 32 << 10
)

// terminal answers GET /api/devices/{id}/terminal, a WebSocket to a shell
// on the device. guard has checked that it comes from one of Roost's own
// pages. Once the WebSocket is open, a shell that can't start is an error
// message on it, which the browser can show, rather than an HTTP status,
// which it can't read.
func (h *handler) terminal(w http.ResponseWriter, r *http.Request) {
	id, q := r.PathValue("id"), r.URL.Query()
	opts := ssh.TerminalOptions{Session: q.Get("session"), Dir: q.Get("cwd")}
	opts.Cols, _ = strconv.Atoi(q.Get("cols"))
	opts.Rows, _ = strconv.Atoi(q.Get("rows"))
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		// Accept answered the request.
		return
	}
	defer func() { _ = conn.CloseNow() }()
	conn.SetReadLimit(maxTerminalMessage)
	ctx := r.Context()

	term, err := h.opts.SSH.OpenTerminal(ctx, id, opts)
	if err != nil {
		if ctx.Err() == nil {
			h.log.Info("couldn't open a terminal", "device", id, "error", err)
		}
		_ = writeMessage(ctx, conn, TerminalMessage{Type: TerminalMessageTypeError, Message: terminalError(err)})
		_ = conn.Close(websocket.StatusNormalClosure, "")
		return
	}
	defer func() { _ = term.Close() }()
	msg := "Opened a terminal on " + h.deviceName(r, id)
	if opts.Session != "" {
		msg += ", in tmux session " + opts.Session
	}
	h.logAction(r, EventKindTerminal, id, msg)
	if err := writeMessage(ctx, conn, TerminalMessage{Type: TerminalMessageTypeReady, Session: opts.Session}); err != nil {
		return
	}
	bridge(ctx, conn, term, h.opts.Stopping)
}

// terminalError says why a terminal couldn't open, for the browser.
func terminalError(err error) string {
	var ce *ssh.ConnectError
	switch {
	case errors.As(err, &ce):
		return "Roost couldn't log in: " + ce.Msg
	case errors.Is(err, context.DeadlineExceeded):
		return "Roost timed out starting the shell"
	default:
		return err.Error()
	}
}

func writeMessage(ctx context.Context, conn *websocket.Conn, m TerminalMessage) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return conn.Write(ctx, websocket.MessageText, b)
}

// bridge copies term's output to conn as binary frames, and conn's input,
// resizes and acks to term. When the shell exits, it sends its exit status
// and closes conn. It returns then, or when conn closes, or when stopping
// is closed, which closes conn too.
func bridge(ctx context.Context, conn *websocket.Conn, term *ssh.Terminal, stopping <-chan struct{}) {
	ctx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	defer wg.Wait()
	defer cancel()
	window := newWindow(TerminalWindow)

	wg.Go(func() {
		// A Read that fails, or whose ctx ends, closes conn.
		defer cancel()
		for {
			typ, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			var m TerminalMessage
			if typ != websocket.MessageText || json.Unmarshal(data, &m) != nil {
				continue
			}
			switch m.Type {
			case TerminalMessageTypeInput:
				if _, err := term.Write([]byte(m.Data)); err != nil {
					return
				}
			case TerminalMessageTypeResize:
				_ = term.Resize(m.Cols, m.Rows)
			case TerminalMessageTypeAck:
				window.ack(m.Bytes)
			case TerminalMessageTypeReady, TerminalMessageTypeExit, TerminalMessageTypeError:
			}
		}
	})
	wg.Go(func() {
		t := time.NewTicker(terminalPing)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-stopping:
				_ = conn.Close(websocket.StatusGoingAway, "Roost is shutting down")
				return
			case <-t.C:
			}
			pctx, done := context.WithTimeout(ctx, terminalPing)
			err := conn.Ping(pctx)
			done()
			if err != nil {
				cancel()
				return
			}
		}
	})

	// The output ends when the shell exits, once all of it is sent.
	ended := make(chan bool, 1)
	wg.Go(func() {
		buf := make([]byte, outputChunk)
		for {
			n, err := term.Read(buf)
			if n > 0 && (!window.take(ctx, n) || conn.Write(ctx, websocket.MessageBinary, buf[:n]) != nil) {
				ended <- false
				return
			}
			if err != nil {
				ended <- ctx.Err() == nil
				return
			}
		}
	})
	select {
	case exited := <-ended:
		if exited {
			// Without a status, the connection dropped, and the shell may
			// still run.
			msg := TerminalMessage{Type: TerminalMessageTypeExit}
			if code, ok := term.Wait(); ok {
				msg.Code = &code
			}
			_ = writeMessage(ctx, conn, msg)
			_ = conn.Close(websocket.StatusNormalClosure, "the shell exited")
			return
		}
	case <-ctx.Done():
	}
	// Closing the terminal ends its output.
	_ = term.Close()
}

// window counts the output sent to a browser that it hasn't said it wrote
// to the screen yet.
type window struct {
	size int
	mu   sync.Mutex
	sent int
	// acked is signaled when the browser says it wrote some.
	acked chan struct{}
}

func newWindow(size int) *window {
	return &window{size: size, acked: make(chan struct{}, 1)}
}

// take waits until there's room to send n more bytes, and counts them. It
// returns false if ctx ends first. Output can go past the window by one
// chunk.
func (w *window) take(ctx context.Context, n int) bool {
	for {
		w.mu.Lock()
		if w.sent < w.size {
			w.sent += n
			w.mu.Unlock()
			return true
		}
		w.mu.Unlock()
		select {
		case <-w.acked:
		case <-ctx.Done():
			return false
		}
	}
}

// ack counts n bytes the browser wrote to the screen.
func (w *window) ack(n int) {
	w.mu.Lock()
	w.sent = max(0, w.sent-n)
	w.mu.Unlock()
	select {
	case w.acked <- struct{}{}:
	default:
	}
}
