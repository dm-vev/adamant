package versions

import (
	"strings"
	"testing"

	"github.com/dm-vev/adamant/net/bedrock/proto/convert/c2225"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2225"
)

func TestParse(t *testing.T) {
	for _, name := range []string{"1.26.50", "26.50", "1.26.51", "26.51", "1.26.52", "26.52", "2193", " 26.52 "} {
		p, err := Parse(name)
		if err != nil {
			t.Errorf("Parse(%q): %v", name, err)
			continue
		}
		if p != Default() || p.Number != 2193 {
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

// TestByNumber finds every protocol the adapter was built with, 2225 too,
// which a config may not name yet: Parse refuses it.
func TestByNumber(t *testing.T) {
	if p, ok := ByNumber(2193); !ok || p != Default() {
		t.Fatalf("ByNumber(2193) = %v, %v", p, ok)
	}
	if p, ok := ByNumber(2225); !ok || p != Model() {
		t.Fatalf("ByNumber(2225) = %v, %v", p, ok)
	}
	if p, ok := ByNumber(2250); ok {
		t.Fatalf("ByNumber(2250) = %v", p)
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
		if p.Converts() && (p.Out == nil || p.In == nil || p.Same == nil) {
			t.Errorf("%d converts without Out, In and Same", p.Number)
		}
		if p.SetShieldID == nil {
			t.Errorf("%d has no shield ID to set", p.Number)
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

// TestModelIsV2225 checks the session is compiled against the codecs of
// 2225, and that 2193, the default, reaches it through c2225.
func TestModelIsV2225(t *testing.T) {
	m := Model()
	if m.Number != v2225.Protocol || m.Version != v2225.Version || m.Range() != "26.60–26.61" {
		t.Fatalf("model %d %s %s", m.Number, m.Version, m.Range())
	}
	if pk := m.New(v2225.IDLogin); pk == nil || pk.ID() != v2225.IDLogin || !m.FromClient(v2225.IDLogin) {
		t.Fatal("the model does not decode a Login")
	}
	d := Default()
	if d.Number != v2193.Protocol || d.Version != v2193.Version || d.Range() != "26.50–26.52" {
		t.Fatalf("default %d %s %s", d.Number, d.Version, d.Range())
	}
	if _, ok := d.New(v2193.IDStartGame).(*v2193.StartGamePacket); !ok {
		t.Fatal("2193 does not decode with its own codecs")
	}
	for _, id := range []uint32{v2193.IDText, v2193.IDMoveActorDelta, v2193.IDLevelChunk, v2193.IDStartGame} {
		if d.SameLayout(id) != c2225.Same(id) {
			t.Errorf("packet %d: SameLayout %v, c2225 %v", id, d.SameLayout(id), c2225.Same(id))
		}
		if !m.SameLayout(id) {
			t.Errorf("packet %d is not of the layout of the model in the model", id)
		}
	}
	// A packet of the same layout goes as it is; one that changed is
	// converted.
	text := &v2225.TextPacket{Message: "hi"}
	if out := d.Out(text); len(out) != 1 || out[0] != text || !d.Keeps(text) {
		t.Fatalf("Out(Text) = %v", out)
	}
	if d.Keeps(&v2225.AnimatePacket{}) {
		t.Fatal("2193 keeps an Animate of the model as it is")
	}
	if out := d.Out(&v2225.AnimatePacket{ActionType: 1}); len(out) != 1 {
		t.Fatalf("Out(Animate) = %v", out)
	} else if _, ok := out[0].(*v2193.AnimatePacket); !ok {
		t.Fatalf("Out(Animate) = %T", out[0])
	}
}
