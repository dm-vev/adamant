package bedrockadapter

import (
	"bytes"
	"fmt"
	"log/slog"
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
		{nil, "", []*versions.Protocol{a, b, c}, b, ""},
		{nil, "latest", []*versions.Protocol{a, b, c}, b, ""},
		{[]string{"all"}, "", []*versions.Protocol{a, b, c}, b, ""},
		{[]string{" ALL "}, " Latest ", []*versions.Protocol{a, b, c}, b, ""},
		{[]string{"all"}, "a", []*versions.Protocol{a, b, c}, a, ""},
		{[]string{"c", "all", "a"}, "", []*versions.Protocol{a, b, c}, b, ""},
		{nil, "b", []*versions.Protocol{b}, b, ""},
		{nil, "a", []*versions.Protocol{a}, a, ""},
		{[]string{"b", "a", "2.0"}, "", []*versions.Protocol{a, b}, b, ""},
		{[]string{"c", "b"}, "latest", []*versions.Protocol{b, c}, b, ""},
		{[]string{"a", "b"}, " b ", []*versions.Protocol{a, b}, b, ""},
		{[]string{"a"}, "", []*versions.Protocol{a}, a, ""},
		{[]string{"a", "x"}, "", nil, nil, `[Network] Protocols: protocol "x" is not supported`},
		{[]string{"a"}, "x", nil, nil, `[Network] PrimaryProtocol: protocol "x" is not supported`},
		{[]string{"a", "c"}, "b", nil, nil, "[Network] PrimaryProtocol 2.0 must be one of Protocols (1.0, 3.0)"},
		{[]string{"c"}, "c", nil, nil, "[Network] PrimaryProtocol 3.0 needs a build whose session speaks 3.0: this one speaks 2.0"},
		{[]string{"c"}, "", nil, nil, "[Network] Protocols (3.0): none is a protocol this build's session speaks, which is 2.0 at most"},
	} {
		got, primary, err := parseProtocols(tc.names, tc.primary, parse, []*versions.Protocol{a, b, c}, b)
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
		{nil, "latest", nil},
		{[]string{"all"}, "", nil},
		{[]string{"all"}, "a", nil},
		{[]string{"a", "b"}, "b", nil},
		{[]string{"a", "b"}, "", nil},
		{[]string{"a", "x"}, "", []string{`Network.Protocols: protocol "x" is not supported`}},
		{[]string{"a"}, "x", []string{`Network.PrimaryProtocol: protocol "x" is not supported`}},
		{[]string{"a", "c"}, "b", []string{`Network.PrimaryProtocol = "b": want one of Network.Protocols (1.0, 3.0)`}},
		{[]string{"c"}, "c", []string{`Network.PrimaryProtocol = "c": this build's session speaks 2.0`}},
		{[]string{"c"}, "", []string{"Network.Protocols: none of 3.0 is a protocol this build's session speaks, which is 2.0 at most"}},
		{[]string{"c"}, "latest", []string{"Network.Protocols: none of 3.0 is a protocol this build's session speaks, which is 2.0 at most"}},
	} {
		var p config.Problems
		checkProtocols(&p, tc.names, tc.primary, parse, []*versions.Protocol{a, b, c}, b)
		var got []string
		for _, pr := range p.List() {
			got = append(got, pr.String())
		}
		if !slices.Equal(got, tc.want) {
			t.Errorf("%q, %q: problems %q, want %q", tc.names, tc.primary, got, tc.want)
		}
	}
}

// TestDefaultProtocols checks the default config allows every protocol this
// build supports and speaks the newest one the session speaks, which a
// build that adds a newer protocol changes without a change of the config.
// (UserConfig.Config is not called: it sets whether queries are answered for
// the process.)
func TestDefaultProtocols(t *testing.T) {
	n := DefaultConfig().Network
	if !slices.Equal(n.Protocols, []string{"all"}) || n.PrimaryProtocol != "latest" {
		t.Fatalf("default keys %q, %q", n.Protocols, n.PrimaryProtocol)
	}
	ps, primary, err := parseProtocols(n.Protocols, n.PrimaryProtocol, versions.Parse, versions.Configurable(), versions.NewestPrimary())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(ps, versions.Configurable()) || primary != versions.NewestPrimary() || primary.Number != 2225 {
		t.Fatalf("protocols %v, primary %v", ps, primary)
	}
	if notes := protocolNotes(ps, primary, n.PrimaryProtocol, versions.Configurable(), versions.NewestPrimary()); len(notes) != 0 {
		t.Fatalf("the default config warns: %q", notes)
	}
}

// TestProtocolNotes checks what an old config is warned of, each note
// naming the keyword that fixes it, and a config that follows the build
// is not.
func TestProtocolNotes(t *testing.T) {
	old, now := oldProtocol(), versions.Model()
	configurable := []*versions.Protocol{old, now}
	for _, tc := range []struct {
		names   []string
		primary string
		want    []string
	}{
		{[]string{"all"}, "latest", nil},
		{nil, "", nil},
		{[]string{"1.26.60"}, "1.26.60", nil},
		{[]string{"1.26.50"}, "1.26.50", []string{`[Network] Protocols leaves out 1.26.60, newer than every protocol it lists: their clients are refused. Set Protocols = ["all"] to allow every supported protocol.`}},
		{nil, "1.26.50", []string{`[Network] Protocols leaves out 1.26.60, newer than every protocol it lists: their clients are refused. Set Protocols = ["all"] to allow every supported protocol.`}},
		{[]string{"all"}, "1.26.50", []string{`[Network] PrimaryProtocol pins 1.26.50, older than 1.26.60, the newest allowed that this build speaks: the server shows the older version and converts the packets for newer clients. Set PrimaryProtocol = "latest" to follow the build.`}},
		{[]string{"1.26.50", "1.26.60"}, "1.26.50", []string{`[Network] PrimaryProtocol pins 1.26.50, older than 1.26.60, the newest allowed that this build speaks: the server shows the older version and converts the packets for newer clients. Set PrimaryProtocol = "latest" to follow the build.`}},
	} {
		ps, primary, err := parseProtocols(tc.names, tc.primary, versions.Parse, configurable, now)
		if err != nil {
			t.Fatalf("%q, %q: %v", tc.names, tc.primary, err)
		}
		if got := protocolNotes(ps, primary, tc.primary, configurable, now); !slices.Equal(got, tc.want) {
			t.Errorf("%q, %q: notes %q, want %q", tc.names, tc.primary, got, tc.want)
		}
	}
}

// TestLogProtocols checks the server tells its protocols once as it starts,
// and warns an old config of the 1.26.50 setup of what it leaves out.
func TestLogProtocols(t *testing.T) {
	for _, tc := range []struct {
		names   []string
		primary string
		want    []string
		not     []string
	}{
		{[]string{"all"}, "latest", []string{"level=INFO", `allowed="1.26.50, 1.26.60"`, "primary=1.26.60"}, []string{"WARN"}},
		{[]string{"1.26.50"}, "1.26.50", []string{"level=INFO", `allowed=1.26.50`, "primary=1.26.50", "level=WARN", `Protocols = [\"all\"]`}, []string{`PrimaryProtocol = \"latest\"`}},
		{[]string{"all"}, "1.26.50", []string{"level=INFO", "primary=1.26.50", "level=WARN", `PrimaryProtocol = \"latest\"`}, []string{`Protocols = [\"all`}},
	} {
		var buf bytes.Buffer
		log := slog.New(slog.NewTextHandler(&buf, nil))
		uc := DefaultConfig()
		uc.Network.Protocols, uc.Network.PrimaryProtocol = tc.names, tc.primary
		ps, primary, err := parseProtocols(uc.Network.Protocols, uc.Network.PrimaryProtocol, versions.Parse, versions.Configurable(), versions.NewestPrimary())
		if err != nil {
			t.Fatal(err)
		}
		logProtocols(log, ps, primary, protocolNotes(ps, primary, tc.primary, versions.Configurable(), versions.NewestPrimary()))
		for _, w := range tc.want {
			if !strings.Contains(buf.String(), w) {
				t.Errorf("%q, %q: log lacks %q:\n%s", tc.names, tc.primary, w, &buf)
			}
		}
		for _, n := range tc.not {
			if strings.Contains(buf.String(), n) {
				t.Errorf("%q, %q: log has %q:\n%s", tc.names, tc.primary, n, &buf)
			}
		}
	}
}

// TestOldestProtocol checks the built resource pack is for the oldest
// protocol clients may join with, the oldest the build supports without any.
func TestOldestProtocol(t *testing.T) {
	a := &versions.Protocol{Codec: codec.Codec{Number: 10, Version: "1.0"}}
	b := &versions.Protocol{Codec: codec.Codec{Number: 20, Version: "2.0"}}
	if p := (Config{Protocols: []*versions.Protocol{b, a}}).oldestProtocol(); p != a {
		t.Errorf("oldest of 20 and 10: %v", p)
	}
	if p := (Config{}).oldestProtocol(); p != oldProtocol() {
		t.Errorf("oldest of none: %v", p)
	}
}
