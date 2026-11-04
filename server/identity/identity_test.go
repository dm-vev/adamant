package identity

import (
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestParseAndString(t *testing.T) {
	for _, tc := range []struct {
		in, want string
		xuid     bool
	}{
		{"xuid:2535412345678901", "xuid:2535412345678901", true},
		{"  XUID:2535412345678901 ", "xuid:2535412345678901", true},
		{"xuid:0002535412345678901", "xuid:2535412345678901", true},
		{"xuid:18446744073709551615", "xuid:18446744073709551615", true},
		{"xuid:18446744073709551616", "xuid:18446744073709551616", false},
		{"xuid:123456789012345678901", "xuid:123456789012345678901", false},
		{"xuid:", "xuid:", false},
		{"xuid:12a", "xuid:12a", false},
		{"xuid:-1", "xuid:-1", false},
		{"xuid:1 Steve", "xuid:1 steve", false},
		{"Steve", "steve", false},
		{"  Alex Two  ", "alex two", false},
		{"STEVE", "steve", false},
	} {
		k, err := Parse(tc.in)
		if err != nil {
			t.Errorf("Parse(%q): %v", tc.in, err)
			continue
		}
		if k.String() != tc.want {
			t.Errorf("Parse(%q).String() = %q, want %q", tc.in, k.String(), tc.want)
		}
		if _, ok := k.XUID(); ok != tc.xuid {
			t.Errorf("Parse(%q) is a key of an XUID: %t, want %t", tc.in, ok, tc.xuid)
		}
		if _, ok := k.Name(); ok == tc.xuid {
			t.Errorf("Parse(%q) is a key of a name: %t, want %t", tc.in, ok, !tc.xuid)
		}
		if again, err := Parse(k.String()); err != nil || again != k {
			t.Errorf("Parse(%q) = %v, %v; want the key %v again", k.String(), again, err, k)
		}
	}
	for _, in := range []string{"", "   "} {
		if k, err := Parse(in); !errors.Is(err, ErrEmptyName) || !k.IsZero() {
			t.Errorf("Parse(%q) = %v, %v; want %v", in, k, err, ErrEmptyName)
		}
	}
	for _, in := range []string{"java:069a79f4-44e9-4726-a5be-fca90e38aaf5", "JAVA:x", "java:"} {
		if _, err := Parse(in); !errors.Is(err, ErrJavaKey) {
			t.Errorf("Parse(%q): %v, want %v", in, err, ErrJavaKey)
		}
	}
	if (Key{}).String() != "" || !(Key{}).IsZero() {
		t.Error("the zero Key is not empty")
	}
}

func TestOfXUIDAndOfName(t *testing.T) {
	a, err := OfXUID("2535412345678901")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Parse("xuid:2535412345678901")
	if a != b {
		t.Error("OfXUID and Parse give two keys of one XUID")
	}
	if x, ok := a.XUID(); !ok || x != "2535412345678901" {
		t.Errorf("XUID() = %q, %t", x, ok)
	}
	for _, bad := range []string{"", "x", "1.5", " 1", "+1", "123456789012345678901"} {
		if _, err := OfXUID(bad); err == nil {
			t.Errorf("OfXUID(%q) did not fail", bad)
		}
	}
	n, err := OfName(" Steve ")
	if err != nil {
		t.Fatal(err)
	}
	if name, ok := n.Name(); !ok || name != "steve" {
		t.Errorf("Name() = %q, %t", name, ok)
	}
	if _, err := OfName(" \t"); !errors.Is(err, ErrEmptyName) {
		t.Errorf("OfName of a blank name: %v", err)
	}
	// A name that reads like an XUID key is a name to OfName.
	if k, _ := OfName("xuid:1"); k == (Key{xuid: "1"}) {
		t.Error("OfName made a key of an XUID")
	}
}

func TestNormalize(t *testing.T) {
	for in, want := range map[string]string{
		"Steve":         "steve",
		"  STEVE\t":     "steve",
		"Alex Two":      "alex two",
		"":              "",
		"ÄLEX":          "älex",
		"\u0130stanbul": "istanbul",
		"\u212aelvin":   "kelvin",
	} {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestOfflineUUID checks OfflineUUID against Java's offline rule. The
// vector of "Notch" is the one Java Edition gives that name offline,
// UUID.nameUUIDFromBytes("OfflinePlayer:Notch".getBytes(UTF_8)), as
// published widely; the others were computed with the same rule by
// Python's hashlib, as no JDK was at hand. OfflineUUID normalises first,
// so names in any case share the UUID of the name lower-cased.
func TestOfflineUUID(t *testing.T) {
	for in, want := range map[string]string{
		"OfflinePlayer:Notch": "b50ad385-829d-3141-a216-7e7d7539ba7f",
		"OfflinePlayer:jeb_":  "a762f560-4fce-3236-812a-b80efff0b62b",
		"OfflinePlayer:steve": "53909932-f794-33c0-9329-948045a4c1ce",
		"OfflinePlayer:Steve": "5627dd98-e6be-3c21-b8a8-e92344183641",
	} {
		if got := nameUUIDFromBytes([]byte(in)); got != uuid.MustParse(want) {
			t.Errorf("nameUUIDFromBytes(%q) = %v, want %v", in, got, want)
		}
	}
	steve := uuid.MustParse("53909932-f794-33c0-9329-948045a4c1ce")
	for _, name := range []string{"steve", "Steve", " STEVE "} {
		if got := OfflineUUID(name); got != steve {
			t.Errorf("OfflineUUID(%q) = %v, want %v", name, got, steve)
		}
	}
	if u := OfflineUUID("Steve"); u.Version() != 3 || u.Variant() != uuid.RFC4122 {
		t.Errorf("OfflineUUID is version %d of variant %v", u.Version(), u.Variant())
	}
}

func TestParseMode(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want Mode
	}{{"auto", Auto}, {" XUID ", XUID}, {"Name", Name}} {
		in, want := tc.in, tc.want
		if got, err := ParseMode(in); err != nil || got != want {
			t.Errorf("ParseMode(%q) = %v, %v; want %v", in, got, err, want)
		}
		if got, _ := ParseMode(want.String()); got != want {
			t.Errorf("ParseMode(%q) = %v", want.String(), got)
		}
	}
	for _, in := range []string{"", "uuid", "xuids"} {
		if _, err := ParseMode(in); err == nil {
			t.Errorf("ParseMode(%q) did not fail", in)
		}
	}
	if _, err := ParseMode("uuid"); err == nil || err.Error() != `unknown IdentityKey "uuid": use "auto", "xuid" or "name"` {
		t.Errorf("ParseMode(uuid): %v", err)
	}
}
