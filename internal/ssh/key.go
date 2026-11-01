package ssh

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"net/netip"
	"os"
	"path/filepath"
	"strings"

	gossh "golang.org/x/crypto/ssh"
)

// keyFile is the name of Bield's private key in its key directory. The
// public key is next to it, with .pub added.
const keyFile = "id_ed25519"

// Key is Bield's own SSH key, which it logs in to machines with.
type Key struct {
	signer gossh.Signer
	// public is the public key as in authorized_keys, with its comment.
	public string
}

// LoadKey reads Bield's key from dir, or makes one if there's none yet,
// named after comment, such as bield@bield. Only Bield's user can read the
// key: dir has mode 0700 and the private key 0600. If the key's mode lets
// anyone else read it, LoadKey makes it 0600 again.
func LoadKey(dir, comment string) (*Key, error) {
	path := filepath.Join(dir, keyFile)
	pemBytes, err := os.ReadFile(filepath.Clean(path))
	if errors.Is(err, fs.ErrNotExist) {
		return newKey(dir, comment)
	}
	if err != nil {
		return nil, fmt.Errorf("read Bield's SSH key: %w", err)
	}
	if info, err := os.Stat(path); err == nil && info.Mode().Perm()&0o077 != 0 {
		if err := os.Chmod(path, 0o600); err != nil {
			return nil, fmt.Errorf("make Bield's SSH key private: %w", err)
		}
	}
	signer, err := gossh.ParsePrivateKey(pemBytes)
	if err != nil {
		return nil, fmt.Errorf("read Bield's SSH key %s: %w", path, err)
	}
	pub, err := os.ReadFile(filepath.Clean(path + ".pub"))
	if err != nil || !hasKey(pub, signer.PublicKey()) {
		// The comment is lost with the .pub file, but the key still works.
		pub = gossh.MarshalAuthorizedKey(signer.PublicKey())
	}
	return &Key{signer: signer, public: strings.TrimSpace(string(pub))}, nil
}

// newKey makes a key in dir and saves it.
func newKey(dir, comment string) (*Key, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	block, err := gossh.MarshalPrivateKey(priv, comment)
	if err != nil {
		return nil, err
	}
	signer, err := gossh.NewSignerFromKey(priv)
	if err != nil {
		return nil, err
	}
	sshPub, err := gossh.NewPublicKey(pub)
	if err != nil {
		return nil, err
	}
	public := strings.TrimSpace(string(gossh.MarshalAuthorizedKey(sshPub)))
	if comment != "" {
		public += " " + comment
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("make Bield's SSH key directory: %w", err)
	}
	path := filepath.Join(dir, keyFile)
	if err := writeNew(path+".pub", []byte(public+"\n"), 0o644); err != nil {
		return nil, fmt.Errorf("save Bield's SSH key: %w", err)
	}
	// The private key goes last: once it's there, the key is complete.
	if err := writeNew(path, pem.EncodeToMemory(block), 0o600); err != nil {
		return nil, fmt.Errorf("save Bield's SSH key: %w", err)
	}
	return &Key{signer: signer, public: public}, nil
}

// writeNew writes data to a new file at path with mode perm, through a
// temporary file, so path is never there half written.
func writeNew(path string, data []byte, perm fs.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(data)
	err = errors.Join(err, f.Chmod(perm), f.Sync(), f.Close())
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		return errors.Join(err, os.Remove(tmp))
	}
	return nil
}

// PublicKey returns the public key as in authorized_keys, with its comment.
func (k *Key) PublicKey() string { return k.public }

// AuthorizedKey returns the line that lets Bield log in with k, for a
// machine's ~/.ssh/authorized_keys. The machine only takes the key from the
// addresses in from, and turns off agent and X11 forwarding for it.
func (k *Key) AuthorizedKey(from []netip.Addr) string {
	opts := make([]string, 0, 3)
	if len(from) > 0 {
		ips := make([]string, len(from))
		for i, ip := range from {
			ips[i] = ip.String()
		}
		opts = append(opts, `from="`+strings.Join(ips, ",")+`"`)
	}
	opts = append(opts, "no-agent-forwarding", "no-X11-forwarding")
	return strings.Join(opts, ",") + " " + k.public
}

// SetupCommand returns a command that adds line to the authorized_keys of
// the user who runs it, unless it's there already. It works in sh, bash,
// zsh and fish, so it has no braces or subshells.
func SetupCommand(line string) string {
	q := Quote(line)
	return "mkdir -p ~/.ssh && chmod 700 ~/.ssh && touch ~/.ssh/authorized_keys && chmod 600 ~/.ssh/authorized_keys && " +
		"grep -qxF " + q + " ~/.ssh/authorized_keys || echo " + q + " >> ~/.ssh/authorized_keys"
}

// AuthorizeHere adds line to the authorized_keys of the user Bield runs as,
// unless that file already has k, so Bield can log in to its own machine as
// that user. It reports whether it added the line.
func (k *Key) AuthorizeHere(line string) (bool, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return false, err
	}
	dir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false, err
	}
	path := filepath.Join(dir, "authorized_keys")
	existing, err := os.ReadFile(filepath.Clean(path))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	for l := range bytes.Lines(existing) {
		if hasKey(l, k.signer.PublicKey()) {
			return false, nil
		}
	}
	f, err := os.OpenFile(filepath.Clean(path), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return false, err
	}
	var out []byte
	if len(existing) > 0 && !bytes.HasSuffix(existing, []byte("\n")) {
		out = append(out, '\n')
	}
	out = append(out, line+"\n"...)
	_, err = f.Write(out)
	return err == nil, errors.Join(err, f.Close())
}

// hasKey reports whether the authorized_keys line in line, which may have
// options, is for key.
func hasKey(line []byte, key gossh.PublicKey) bool {
	parsed, _, _, _, err := gossh.ParseAuthorizedKey(line)
	return err == nil && bytes.Equal(parsed.Marshal(), key.Marshal())
}
