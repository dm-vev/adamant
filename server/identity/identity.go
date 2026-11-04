// Package identity says how the server keys its players: by the Xbox
// account of a player that signed in with Xbox Live, or by its name on a
// server that lets players join without signing in.
package identity

import (
	"crypto/md5"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// Mode is what the server keys its players by.
type Mode uint8

const (
	// Auto keys players by XUID on a server whose logins Xbox Live
	// vouches for, and by name otherwise. The program resolves it as it
	// composes its Config; a Server given Auto keys by name.
	Auto Mode = iota
	// XUID keys players by their Xbox Live user ID.
	XUID
	// Name keys players by their names, normalised.
	Name
)

// String returns the name of the mode as config.toml writes it: "auto",
// "xuid" or "name".
func (m Mode) String() string {
	switch m {
	case XUID:
		return "xuid"
	case Name:
		return "name"
	default:
		return "auto"
	}
}

// ParseMode reads a mode as config.toml writes it, trimmed and in any case.
// It fails for any other value.
func ParseMode(s string) (Mode, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "auto":
		return Auto, nil
	case "xuid":
		return XUID, nil
	case "name":
		return Name, nil
	}
	return Auto, fmt.Errorf("unknown IdentityKey %q: use \"auto\", \"xuid\" or \"name\"", s)
}

// Key is what a list of the server holds a player by: an XUID, or a name.
// The zero Key is no key. Keys are comparable.
type Key struct {
	xuid string // decimal, no leading zeros; empty for a name
	name string // Normalize(name); empty for an XUID
}

// xuidPrefix starts the text of a key of an XUID; javaPrefix is kept for
// the keys of Java Edition accounts.
const (
	xuidPrefix = "xuid:"
	javaPrefix = "java:"
)

var (
	// ErrEmptyName is returned for a name that normalises to "".
	ErrEmptyName = errors.New("player name must not be empty")
	// ErrJavaKey is returned for a key of a Java Edition account, which no
	// adapter makes yet.
	ErrJavaKey = errors.New("java: keys are not supported: no Java Edition account can join")
)

// OfXUID returns the key of an XUID, given in decimal: 1 to 20 digits of a
// value that fits a uint64.
func OfXUID(xuid string) (Key, error) {
	if len(xuid) == 0 || len(xuid) > 20 || strings.TrimLeft(xuid, "0123456789") != "" {
		return Key{}, fmt.Errorf("invalid XUID %q: want its decimal digits", xuid)
	}
	n, err := strconv.ParseUint(xuid, 10, 64)
	if err != nil {
		return Key{}, fmt.Errorf("invalid XUID %q: %w", xuid, err)
	}
	return Key{xuid: strconv.FormatUint(n, 10)}, nil
}

// OfName returns the key of a name. It fails for a name that normalises
// to "".
func OfName(name string) (Key, error) {
	n := Normalize(name)
	if n == "" {
		return Key{}, ErrEmptyName
	}
	return Key{name: n}, nil
}

// Parse reads a key as an admin writes it: "xuid:" and the XUID in decimal,
// or a name. A text that begins with "xuid:" but has no valid XUID after it
// is a name; one that begins with "java:" is refused, as those keys are
// kept for Java Edition accounts.
func Parse(s string) (Key, error) {
	t := strings.TrimSpace(s)
	if len(t) > len(xuidPrefix) && strings.EqualFold(t[:len(xuidPrefix)], xuidPrefix) {
		if k, err := OfXUID(t[len(xuidPrefix):]); err == nil {
			return k, nil
		}
	}
	if len(t) >= len(javaPrefix) && strings.EqualFold(t[:len(javaPrefix)], javaPrefix) {
		return Key{}, ErrJavaKey
	}
	return OfName(t)
}

// String returns "xuid:<XUID>" or the normalised name: the form Parse
// reads. The zero Key returns "".
func (k Key) String() string {
	if k.xuid != "" {
		return xuidPrefix + k.xuid
	}
	return k.name
}

// XUID returns the XUID of a key of an XUID.
func (k Key) XUID() (string, bool) { return k.xuid, k.xuid != "" }

// Name returns the normalised name of a key of a name.
func (k Key) Name() (string, bool) { return k.name, k.name != "" }

// IsZero reports whether k is no key.
func (k Key) IsZero() bool { return k == Key{} }

// Normalize returns the form names are compared in: trimmed and
// lower-cased, as the whitelist has always compared them.
func Normalize(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// OfflineUUID returns the UUID of a player keyed by its name: version 3,
// of the MD5 of "OfflinePlayer:" and the normalised name, as Java Edition's
// UUID.nameUUIDFromBytes derives the UUIDs of offline players (of the name
// as typed there).
func OfflineUUID(name string) uuid.UUID {
	return nameUUIDFromBytes([]byte("OfflinePlayer:" + Normalize(name)))
}

// nameUUIDFromBytes is Java's UUID.nameUUIDFromBytes: the MD5 of b alone,
// with no namespace, as a version 3 UUID of the RFC 4122 variant.
func nameUUIDFromBytes(b []byte) uuid.UUID {
	sum := md5.Sum(b)
	sum[6] = sum[6]&0x0f | 0x30
	sum[8] = sum[8]&0x3f | 0x80
	return uuid.UUID(sum)
}
