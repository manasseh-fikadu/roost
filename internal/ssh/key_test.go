package ssh_test

import (
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gossh "golang.org/x/crypto/ssh"

	"github.com/manasseh-fikadu/roost/internal/ssh"
)

func TestLoadKey(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "ssh")
	key, err := ssh.LoadKey(dir, "roost@hub")
	if err != nil {
		t.Fatal(err)
	}
	pub, _, _, _, err := gossh.ParseAuthorizedKey([]byte(key.PublicKey()))
	if err != nil {
		t.Fatalf("public key %q: %v", key.PublicKey(), err)
	}
	if pub.Type() != gossh.KeyAlgoED25519 || !strings.HasSuffix(key.PublicKey(), " roost@hub") {
		t.Errorf("public key = %q, want an ed25519 key named roost@hub", key.PublicKey())
	}
	for path, want := range map[string]os.FileMode{
		dir: 0o700, filepath.Join(dir, "id_ed25519"): 0o600, filepath.Join(dir, "id_ed25519.pub"): 0o644,
	} {
		if info, err := os.Stat(path); err != nil || info.Mode().Perm() != want {
			t.Errorf("%s: mode %v, %v, want %v", path, info.Mode().Perm(), err, want)
		}
	}
	// OpenSSH can use the key too.
	priv, err := os.ReadFile(filepath.Clean(filepath.Join(dir, "id_ed25519")))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(priv), "-----BEGIN OPENSSH PRIVATE KEY-----") {
		t.Errorf("the private key isn't in OpenSSH's format:\n%s", priv)
	}

	// The key is made once, and a key someone else could read is made
	// private again.
	if err := os.Chmod(filepath.Join(dir, "id_ed25519"), 0o644); err != nil { //nolint:gosec // G302: see above.
		t.Fatal(err)
	}
	again, err := ssh.LoadKey(dir, "another comment")
	if err != nil {
		t.Fatal(err)
	}
	if again.PublicKey() != key.PublicKey() {
		t.Errorf("loaded %q, want the key made before, %q", again.PublicKey(), key.PublicKey())
	}
	if info, err := os.Stat(filepath.Join(dir, "id_ed25519")); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("the private key's mode is %v, %v, want 0600", info.Mode().Perm(), err)
	}
}

func TestLoadKey_withoutPublicKey(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	key, err := ssh.LoadKey(dir, "roost@hub")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "id_ed25519.pub")); err != nil {
		t.Fatal(err)
	}
	again, err := ssh.LoadKey(dir, "roost@hub")
	if err != nil {
		t.Fatal(err)
	}
	if want, _, _ := strings.Cut(key.PublicKey(), " roost"); again.PublicKey() != want {
		t.Errorf("public key = %q, want %q without its comment", again.PublicKey(), want)
	}
}

func TestAuthorizedKey(t *testing.T) {
	t.Parallel()
	key, err := ssh.LoadKey(t.TempDir(), "roost@hub")
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name string
		from []netip.Addr
		want string
	}{
		{
			name: "tailnet",
			from: []netip.Addr{netip.MustParseAddr("100.64.0.1"), netip.MustParseAddr("fd7a:115c:a1e0::1")},
			want: `from="100.64.0.1,fd7a:115c:a1e0::1",no-agent-forwarding,no-X11-forwarding `,
		},
		{name: "anywhere", want: "no-agent-forwarding,no-X11-forwarding "},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := key.AuthorizedKey(tt.from); got != tt.want+key.PublicKey() {
				t.Errorf("AuthorizedKey = %q, want %q", got, tt.want+key.PublicKey())
			}
		})
	}
}

func TestAuthorizeHere(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	key, err := ssh.LoadKey(t.TempDir(), "roost@hub")
	if err != nil {
		t.Fatal(err)
	}
	keys := filepath.Join(home, ".ssh", "authorized_keys")
	if err := os.MkdirAll(filepath.Dir(keys), 0o700); err != nil {
		t.Fatal(err)
	}
	// A file without a final newline gets one before the new line.
	if err := os.WriteFile(keys, []byte("ssh-ed25519 AAAAOther other@laptop"), 0o600); err != nil {
		t.Fatal(err)
	}
	line := key.AuthorizedKey([]netip.Addr{netip.MustParseAddr("127.0.0.1")})
	for i, want := range []bool{true, false} {
		added, err := key.AuthorizeHere(line)
		if err != nil || added != want {
			t.Errorf("call %d: AuthorizeHere = %v, %v, want %v", i+1, added, err, want)
		}
	}
	got, err := os.ReadFile(filepath.Clean(keys))
	if err != nil {
		t.Fatal(err)
	}
	if want := "ssh-ed25519 AAAAOther other@laptop\n" + line + "\n"; string(got) != want {
		t.Errorf("authorized_keys =\n%s\nwant\n%s", got, want)
	}
}
