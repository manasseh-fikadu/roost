package netguard_test

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"testing"

	"github.com/manasseh-fikadu/roost/internal/netguard"
)

func TestForbiddenHost(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		host string
		want bool
	}{
		// Where AWS, Azure, Google Cloud, DigitalOcean, Oracle and others answer.
		{"169.254.169.254", true},
		{"169.254.170.2", true},
		{"169.254.0.1", true},
		{"169.254.255.255", true},
		{"fe80::1", true},
		{"[fe80::1]", true},
		{"fe80::1%eth0", true},
		{"FE80::1", true},
		{"fd00:ec2::254", true},
		{"::ffff:169.254.169.254", true},
		{"[::ffff:169.254.169.254]", true},
		{"metadata.google.internal", true},
		{"METADATA.GOOGLE.INTERNAL.", true},
		{"metadata", true},
		{"instance-data", true},
		{"ff02::1", true},
		// The ranges next to it, and what checks are for.
		{"169.253.255.255", false},
		{"169.255.0.0", false},
		{"127.0.0.1", false},
		{"::1", false},
		{"10.0.0.5", false},
		{"192.168.1.1", false},
		{"100.100.100.100", false},
		{"fd00:ec2::255", false},
		{"fec0::1", false},
		{"example.com", false},
		{"metadata.example.com", false},
		{"my-metadata", false},
		{"laptop", false},
		{"", false},
	} {
		t.Run(tt.host, func(t *testing.T) {
			t.Parallel()
			if got := netguard.ForbiddenHost(tt.host); got != tt.want {
				t.Errorf("ForbiddenHost(%q) = %v, want %v", tt.host, got, tt.want)
			}
		})
	}
}

func TestForbiddenAddr(t *testing.T) {
	t.Parallel()
	if !netguard.ForbiddenAddr(netip.MustParseAddr("::ffff:169.254.169.254")) {
		t.Error("an IPv4 address in IPv6 clothes got through")
	}
	if netguard.ForbiddenAddr(netip.MustParseAddr("::ffff:10.0.0.1")) {
		t.Error("a private address in IPv6 clothes was forbidden")
	}
}

func TestDial(t *testing.T) {
	t.Parallel()
	var dialed []string
	fake := func(_ context.Context, _, addr string) (net.Conn, error) {
		dialed = append(dialed, addr)
		return nil, errors.New("fake dial")
	}
	dial := netguard.Dial(fake)

	for _, addr := range []string{"169.254.169.254:80", "[fe80::1]:443", "metadata.google.internal:80", "[fd00:ec2::254]:80"} {
		if _, err := dial(t.Context(), "tcp", addr); !errors.Is(err, netguard.ErrForbidden) {
			t.Errorf("dial %s = %v, want ErrForbidden", addr, err)
		}
	}
	if len(dialed) != 0 {
		t.Errorf("dialed %v, want no connection to a forbidden address", dialed)
	}
	for _, addr := range []string{"127.0.0.1:8080", "10.0.0.5:22", "example.com:443", "laptop:80", "no port"} {
		if _, err := dial(t.Context(), "tcp", addr); err == nil || errors.Is(err, netguard.ErrForbidden) {
			t.Errorf("dial %s = %v, want the fake's error", addr, err)
		}
	}
	if len(dialed) != 5 {
		t.Errorf("dialed %v, want the five that are allowed", dialed)
	}
}
