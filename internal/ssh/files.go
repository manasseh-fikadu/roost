package ssh

import (
	"context"
	"errors"
	"io"
	"sync"

	"github.com/pkg/sftp"
	gossh "golang.org/x/crypto/ssh"
)

// ErrNoSFTP is why Bield can't open the files of a machine whose sshd has no
// SFTP subsystem.
var ErrNoSFTP = errors.New("the machine's sshd doesn't offer SFTP; turn it on with a Subsystem sftp line in sshd_config")

// SFTP is an SFTP client on a device's shared connection.
type SFTP struct {
	*sftp.Client
	// Home is the home folder of the user Bield logs in as.
	Home string
	// Cache is for what the file manager learns about the machine, such as
	// its users' names, and keeps as long as the client lasts.
	Cache sync.Map
	// done is closed when the client ends, such as when the connection
	// drops.
	done chan struct{}
}

// SFTP returns an SFTP client for a device, over its shared connection,
// which it connects first if need be, and a function to call once done with
// it. Many callers can use it at once. A client that ends, as the connection
// does when it drops, is replaced by the next call.
func (m *Manager) SFTP(ctx context.Context, deviceID string) (*SFTP, func(), error) {
	t, err := m.target(deviceID)
	if err != nil {
		return nil, nil, err
	}
	p, release, err := m.sharedPool(ctx, t)
	if err != nil {
		return nil, nil, err
	}
	c, err := p.sftpClient()
	if err != nil {
		release()
		return nil, nil, err
	}
	return c, release, nil
}

// sftpClient returns the connection's SFTP client, making it if there's none
// or it has ended.
func (p *pooled) sftpClient() (*SFTP, error) {
	p.sftpMu.Lock()
	defer p.sftpMu.Unlock()
	if p.sftp != nil {
		select {
		case <-p.sftp.done:
			p.sftp = nil
		default:
			return p.sftp, nil
		}
	}
	c, err := newSFTP(p.client)
	if err != nil {
		return nil, err
	}
	home, err := c.Getwd()
	if err != nil {
		_ = c.Close()
		return nil, err
	}
	s := &SFTP{Client: c, Home: home, done: make(chan struct{})}
	go func() {
		_ = c.Wait()
		close(s.done)
	}()
	p.sftp = s
	return s, nil
}

// newSFTP starts the SFTP subsystem on a new channel of client. It's
// sftp.NewClient, except that it can tell a machine with no SFTP from a
// connection that dropped.
func newSFTP(client *gossh.Client) (*sftp.Client, error) {
	sess, err := client.NewSession()
	if err != nil {
		return nil, err
	}
	stdin, err := sess.StdinPipe()
	if err != nil {
		_ = sess.Close()
		return nil, err
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		_ = sess.Close()
		return nil, err
	}
	if err := sess.RequestSubsystem("sftp"); err != nil {
		_ = sess.Close()
		if errors.Is(err, io.EOF) {
			return nil, err
		}
		return nil, ErrNoSFTP
	}
	c, err := sftp.NewClientPipe(stdout, stdin)
	if err != nil {
		_ = sess.Close()
		return nil, err
	}
	return c, nil
}

// A Stream is a command's output as it comes. Read it to the end, then Wait
// for how the command ended; Close it when done, which ends the command if
// it's still running.
type Stream struct {
	stdout  io.Reader
	session *gossh.Session
	stderr  *limitedBuffer
	release func()
	stop    func() bool

	waitOnce sync.Once
	code     int
}

// Stream runs a sh script on a device as Exec does, but returns its output as
// it comes, instead of waiting for it to end.
func (m *Manager) Stream(ctx context.Context, deviceID, script string, args ...string) (*Stream, error) {
	if err := checkArgs(args); err != nil {
		return nil, err
	}
	t, err := m.target(deviceID)
	if err != nil {
		return nil, err
	}
	client, release, err := m.sharedTo(ctx, t)
	if err != nil {
		return nil, err
	}
	sess, err := client.NewSession()
	if err != nil {
		release()
		return nil, err
	}
	s := &Stream{session: sess, stderr: &limitedBuffer{max: maxStderr}, release: release}
	sess.Stderr = s.stderr
	if s.stdout, err = sess.StdoutPipe(); err == nil {
		err = sess.Start(command(extraPath+script, args...))
	}
	if err != nil {
		_ = sess.Close()
		release()
		return nil, err
	}
	s.stop = context.AfterFunc(ctx, func() { _ = sess.Close() })
	return s, nil
}

// Read reads the command's output.
func (s *Stream) Read(p []byte) (int, error) { return s.stdout.Read(p) }

// Wait waits for the command to end, and returns its exit status, or -1 if it
// ended without one, and the start of what it wrote to stderr. Call it after
// reading to the end.
func (s *Stream) Wait() (code int, stderr string) {
	s.waitOnce.Do(func() {
		var exit *gossh.ExitError
		switch err := s.session.Wait(); {
		case err == nil:
		case errors.As(err, &exit):
			s.code = exit.ExitStatus()
		default:
			s.code = -1
		}
	})
	return s.code, s.stderr.String()
}

// Close ends the command, if it's still running, and lets go of the
// connection.
func (s *Stream) Close() error {
	s.stop()
	defer s.release()
	return s.session.Close()
}
