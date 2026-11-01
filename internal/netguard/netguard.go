// Package netguard keeps Roost from connecting to the addresses that serve
// credentials to whoever on a machine asks: link-local addresses, where cloud
// providers put their metadata services, such as AWS's 169.254.169.254.
//
// Checks and notification channels connect wherever an admin points them,
// from Roost's own machine. That is the point of them, and loopback and
// private addresses stay open: checking a service on the same machine or the
// same network is what most checks are. But nothing a check should reach is
// link-local, and a metadata service would answer one with its credentials'
// names and a status that says they're there.
package netguard

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strings"
)

// ErrForbidden is why Roost won't connect to an address.
var ErrForbidden = errors.New("Roost doesn't connect to link-local or cloud metadata addresses") //nolint:staticcheck // ST1005: it starts a sentence with a name.

// A DialFunc connects to the address on a network, such as "tcp" and
// "example.com:443".
type DialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

// metadataV6 is where AWS puts its metadata service for IPv6.
var metadataV6 = netip.MustParseAddr("fd00:ec2::254")

// metadataNames are names that cloud providers resolve to their metadata
// service, which a resolver outside the cloud doesn't know.
var metadataNames = map[string]bool{
	"metadata":                   true,
	"metadata.google.internal":   true,
	"metadata.goog":              true,
	"instance-data":              true,
	"instance-data.ec2.internal": true,
}

// ForbiddenAddr reports whether Roost won't connect to ip: link-local
// (169.254.0.0/16 and fe80::/10), including as an IPv4-mapped IPv6 address,
// and AWS's IPv6 metadata address.
func ForbiddenAddr(ip netip.Addr) bool {
	ip = ip.Unmap()
	return ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip == metadataV6
}

// ForbiddenHost reports whether host, a name or an address, is one Roost
// won't connect to. A name is only forbidden if it's one of the metadata
// services' names.
func ForbiddenHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(strings.Trim(host, "[]"), "."))
	// A zone, as in fe80::1%eth0, doesn't change which address it is.
	bare, _, _ := strings.Cut(host, "%")
	if ip, err := netip.ParseAddr(bare); err == nil {
		return ForbiddenAddr(ip)
	}
	return metadataNames[host]
}

// Dial returns a DialFunc that refuses to connect to a forbidden address or
// metadata name, and otherwise dials like dial.
//
// It doesn't look names up, so a name that an admin's own DNS points at a
// link-local address gets through. An admin can open a shell on Roost's
// machine, where they can reach it anyway: this is a guard rail that keeps a
// check from asking a metadata service about credentials by mistake, or
// showing them to the viewers who read its results.
func Dial(dial DialFunc) DialFunc {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		if host, _, err := net.SplitHostPort(addr); err == nil && ForbiddenHost(host) {
			return nil, ErrForbidden
		}
		return dial(ctx, network, addr)
	}
}
