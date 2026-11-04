package bedrockadapter

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/protocol/bedrock/internal/versions"
	"github.com/df-mc/dragonfly/server/config"
	"github.com/dm-vev/adamant/net/bedrock/proto/codec"
)

// TestParseProtocols checks the protocols and the primary the keys of
// [Network] name, against a build of three protocols whose session speaks
// the second: the order, the defaults and the refusals.
func TestParseProtocols(t *testing.T) {
	a := &versions.Protocol{Codec: codec.Codec{Number: 10, Version: "1.0"}, Names: []string{"1.0", "a"}}
	b := &versions.Protocol{Codec: codec.Codec{Number: 20, Version: "2.0"}, Names: []string{"2.0", "b"}}
	c := &versions.Protocol{Codec: codec.Codec{Number: 30, Version: "3.0"}, Names: []string{"3.0", "c"}}
	parse := func(name string) (*versions.Protocol, error) {
		for _, p := range []*versions.Protocol{a, b, c} {
			if slices.Contains(p.Names, strings.TrimSpace(name)) {
				return p, nil
			}
		}
		return nil, fmt.Errorf("protocol %q is not supported", name)
	}
	for _, tc := range []struct {
		names   []string
		primary string
		want    []*versions.Protocol
		wantP   *versions.Protocol
		err     string
	}{
		{nil, "", []*versions.Protocol{a}, a, ""},
		{nil, "b", []*versions.Protocol{b}, b, ""},
		{[]string{"b", "a", "2.0"}, "", []*versions.Protocol{a, b}, a, ""},
		{[]string{"c", "b"}, "", []*versions.Protocol{b, c}, b, ""},
		{[]string{"a", "b"}, " b ", []*versions.Protocol{a, b}, b, ""},
		{[]string{"a", "x"}, "", nil, nil, `[Network] Protocols: protocol "x" is not supported`},
		{[]string{"a"}, "x", nil, nil, `[Network] PrimaryProtocol: protocol "x" is not supported`},
		{[]string{"a", "c"}, "b", nil, nil, "[Network] PrimaryProtocol 2.0 must be one of Protocols (1.0, 3.0)"},
		{[]string{"c"}, "", nil, nil, "[Network] PrimaryProtocol 3.0 needs a build whose session speaks 3.0: this one speaks 2.0"},
	} {
		got, primary, err := parseProtocols(tc.names, tc.primary, parse, a, b)
		if tc.err != "" {
			if err == nil || err.Error() != tc.err {
				t.Errorf("%q, %q: error %v, want %q", tc.names, tc.primary, err, tc.err)
			}
			continue
		}
		if err != nil || !slices.Equal(got, tc.want) || primary != tc.wantP {
			t.Errorf("%q, %q: %v, %v, %v; want %v, %v", tc.names, tc.primary, got, primary, err, tc.want, tc.wantP)
		}
	}
}

// TestCheckProtocols checks the problems config.toml is reported for its
// protocols, each under its key, against the build of TestParseProtocols.
func TestCheckProtocols(t *testing.T) {
	a := &versions.Protocol{Codec: codec.Codec{Number: 10, Version: "1.0"}, Names: []string{"a"}}
	b := &versions.Protocol{Codec: codec.Codec{Number: 20, Version: "2.0"}, Names: []string{"b"}}
	c := &versions.Protocol{Codec: codec.Codec{Number: 30, Version: "3.0"}, Names: []string{"c"}}
	parse := func(name string) (*versions.Protocol, error) {
		for _, p := range []*versions.Protocol{a, b, c} {
			if slices.Contains(p.Names, strings.TrimSpace(name)) {
				return p, nil
			}
		}
		return nil, fmt.Errorf("protocol %q is not supported", name)
	}
	for _, tc := range []struct {
		names   []string
		primary string
		want    []string
	}{
		{nil, "", nil},
		{[]string{"a", "b"}, "b", nil},
		{[]string{"a", "x"}, "", []string{`Network.Protocols: protocol "x" is not supported`}},
		{[]string{"a"}, "x", []string{`Network.PrimaryProtocol: protocol "x" is not supported`}},
		{[]string{"a", "c"}, "b", []string{`Network.PrimaryProtocol = "b": want one of Network.Protocols (1.0, 3.0)`}},
		{[]string{"c"}, "c", []string{`Network.PrimaryProtocol = "c": this build's session speaks 2.0`}},
		{[]string{"c"}, "", []string{"Network.Protocols: the oldest, 3.0, is the primary, and this build's session speaks 2.0"}},
	} {
		var p config.Problems
		checkProtocols(&p, tc.names, tc.primary, parse, b)
		var got []string
		for _, pr := range p.List() {
			got = append(got, pr.String())
		}
		if !slices.Equal(got, tc.want) {
			t.Errorf("%q, %q: problems %q, want %q", tc.names, tc.primary, got, tc.want)
		}
	}
}

// TestDefaultProtocols checks the default config allows and speaks the
// default protocol of the build, and nothing else. (UserConfig.Config is
// not called: it sets whether queries are answered for the process.)
func TestDefaultProtocols(t *testing.T) {
	n := DefaultConfig().Network
	ps, primary, err := parseProtocols(n.Protocols, n.PrimaryProtocol, versions.Parse, versions.Default(), versions.Model())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(ps, []*versions.Protocol{versions.Default()}) || primary != versions.Default() || primary.Number != 2193 {
		t.Fatalf("protocols %v, primary %v", ps, primary)
	}
}

// TestOldestProtocol checks the built resource pack is for the oldest
// protocol clients may join with, the default of the build without any.
func TestOldestProtocol(t *testing.T) {
	a := &versions.Protocol{Codec: codec.Codec{Number: 10, Version: "1.0"}}
	b := &versions.Protocol{Codec: codec.Codec{Number: 20, Version: "2.0"}}
	if p := (Config{Protocols: []*versions.Protocol{b, a}}).oldestProtocol(); p != a {
		t.Errorf("oldest of 20 and 10: %v", p)
	}
	if p := (Config{}).oldestProtocol(); p != versions.Default() {
		t.Errorf("oldest of none: %v", p)
	}
}
