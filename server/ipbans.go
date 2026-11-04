package server

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
)

// DefaultIPv6Prefix is how many leading bits of an IPv6 address a ban of it
// covers by default: 64, the network of one subscriber, as the listener
// counts a /64 as one source.
const DefaultIPv6Prefix = 64

// parseAddr parses an address as a client or an admin writes it, with or
// without a port, brackets or a zone, an IPv4 address in IPv6 unmapped.
func parseAddr(s string) (netip.Addr, bool) {
	s = strings.TrimSpace(s)
	if host, _, err := net.SplitHostPort(s); err == nil {
		s = host
	}
	a, err := netip.ParseAddr(strings.Trim(s, "[]"))
	if err != nil {
		return netip.Addr{}, false
	}
	return a.WithZone("").Unmap(), true
}

// keyOfPrefix returns the key of the ban of a prefix: the address alone
// for a prefix of every bit, the masked prefix otherwise.
func keyOfPrefix(p netip.Prefix) string {
	p = p.Masked()
	if p.Bits() == p.Addr().BitLen() {
		return p.Addr().String()
	}
	return p.String()
}

// banKey returns the key of a new ban of the address or the prefix typed:
// an IPv4 address alone, an IPv6 address as its network of v6Prefix bits,
// a prefix as typed, masked. It reports false for neither.
func banKey(s string, v6Prefix int) (string, bool) {
	s = strings.TrimSpace(s)
	if strings.Contains(s, "/") {
		p, err := netip.ParsePrefix(s)
		if err != nil {
			return "", false
		}
		return keyOfPrefix(netip.PrefixFrom(p.Addr().Unmap(), min(p.Bits(), p.Addr().Unmap().BitLen()))), true
	}
	a, ok := parseAddr(s)
	if !ok {
		return "", false
	}
	if a.Is6() && v6Prefix > 0 && v6Prefix < 128 {
		return keyOfPrefix(netip.PrefixFrom(a, v6Prefix)), true
	}
	return a.String(), true
}

// fileKey returns the key of a ban of an address of banned-players.json:
// an address alone, as older servers wrote it, matches that address alone;
// a prefix its network. It returns "" for neither.
func fileKey(s string) string {
	if strings.Contains(s, "/") {
		k, _ := banKey(s, 128)
		return k
	}
	if a, ok := parseAddr(s); ok {
		return a.String()
	}
	return ""
}

// keyMatches reports whether the ban of the key passed covers the address
// passed. A prefix never covers a loopback address, as of a proxy on the
// host: only a ban of the address alone does.
func keyMatches(key string, a netip.Addr) bool {
	if !strings.Contains(key, "/") {
		return key == a.String()
	}
	p, err := netip.ParsePrefix(key)
	return err == nil && !a.IsLoopback() && p.Contains(a)
}

// rebuildPrefixesLocked lists the prefixes of the bans of addresses, which
// bannedIPLocked scans.
func (b *banList) rebuildPrefixesLocked() {
	b.prefixes = b.prefixes[:0]
	for _, k := range b.ipOrder {
		if strings.Contains(k, "/") {
			b.prefixes = append(b.prefixes, k)
		}
	}
}

// errNoBanTarget is the error of /ban-ip of a target that is neither an
// address, a prefix nor a player online.
var errNoBanTarget = errors.New("want an IP address, a network such as 203.0.113.0/24, or a player online")

// BanTarget returns the key a ban of the target passed is kept by: an IPv4
// address, an IPv6 address as its network of [Bans] IPv6Prefix bits, a
// network as typed, such as 203.0.113.0/24, or the address of the player
// online of the name passed.
func (srv *Server) BanTarget(target string) (string, error) {
	prefix := DefaultIPv6Prefix
	if srv.bans != nil {
		prefix = srv.bans.v6Prefix
	}
	if k, ok := banKey(target, prefix); ok {
		return k, nil
	}
	if p, ok := srv.players.named(target); ok && p.addr != nil {
		if k, ok := banKey(p.addr.String(), prefix); ok {
			return k, nil
		}
	}
	return "", fmt.Errorf("%q: %w", target, errNoBanTarget)
}

// PlayersAt returns the names of the players online whose address the ban
// of the key passed covers, in the order they joined.
func (srv *Server) PlayersAt(key string) []string {
	var names []string
	for _, p := range srv.playersAt(key) {
		names = append(names, p.id.Name)
	}
	return names
}

// playersAt returns the players online whose address the ban of the key
// passed covers, in the order they joined.
func (srv *Server) playersAt(key string) []onlinePlayer {
	var out []onlinePlayer
	for _, p := range srv.players.snapshot() {
		if p.addr == nil {
			continue
		}
		if a, ok := parseAddr(p.addr.String()); ok && keyMatches(key, a) {
			out = append(out, p)
		}
	}
	return out
}
