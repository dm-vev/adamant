package versions

import (
	"strings"
	"testing"

	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

func TestParse(t *testing.T) {
	for _, name := range []string{"1.26.50", "26.50", "1.26.51", "26.51", "1.26.52", "26.52", "2193", " 26.52 "} {
		p, err := Parse(name)
		if err != nil {
			t.Errorf("Parse(%q): %v", name, err)
			continue
		}
		if p != Model() || p.Number != 2193 {
			t.Errorf("Parse(%q) = %d, want 2193", name, p.Number)
		}
	}
	for _, name := range []string{"", "1.26.40", "26.4", "1.26", "26.60", "1.26.60", "2225", "2192", "v1.26.50", "1.26.50.5", "0x891"} {
		p, err := Parse(name)
		if err == nil {
			t.Errorf("Parse(%q) = %d, want an error", name, p.Number)
			continue
		}
		if want := "this build supports 1.26.50 (26.50–26.52)"; !strings.Contains(err.Error(), want) {
			t.Errorf("Parse(%q): %q does not name the protocols supported (%q)", name, err, want)
		}
	}
}

func TestByNumber(t *testing.T) {
	if p, ok := ByNumber(2193); !ok || p != Model() {
		t.Fatalf("ByNumber(2193) = %v, %v", p, ok)
	}
	if p, ok := ByNumber(2225); ok {
		t.Fatalf("ByNumber(2225) = %v, want none before the codecs of 2225 are in", p)
	}
}

// TestAll checks the protocols are in order and the model is the newest:
// only the newest codec package can carry every field.
func TestAll(t *testing.T) {
	ps := All()
	if len(ps) == 0 || ps[len(ps)-1] != Model() {
		t.Fatalf("the model is not the newest protocol of %v", ps)
	}
	names := map[string]int32{}
	for i, p := range ps {
		if i > 0 && ps[i-1].Number >= p.Number {
			t.Errorf("%d comes before %d", ps[i-1].Number, p.Number)
		}
		if p.Version != p.Names[0] {
			t.Errorf("%d tells clients %s, not its first release %s", p.Number, p.Version, p.Names[0])
		}
		if p.New == nil || p.FromClient == nil {
			t.Errorf("%d has no codecs", p.Number)
		}
		if (p == Model()) == p.Converts() {
			t.Errorf("%d: Converts() = %v", p.Number, p.Converts())
		}
		for _, n := range p.Names {
			if other, ok := names[n]; ok {
				t.Errorf("%s names %d and %d", n, other, p.Number)
			}
			names[n] = p.Number
		}
	}
	ps[0] = nil
	if All()[0] == nil {
		t.Fatal("All returns the registry itself")
	}
}

func TestModelIsV2193(t *testing.T) {
	m := Model()
	if m.Number != v2193.Protocol || m.Version != v2193.Version || m.Range() != "26.50–26.52" {
		t.Fatalf("model %d %s %s", m.Number, m.Version, m.Range())
	}
	if pk := m.New(v2193.IDLogin); pk == nil || pk.ID() != v2193.IDLogin || !m.FromClient(v2193.IDLogin) {
		t.Fatal("the model does not decode a Login")
	}
}
