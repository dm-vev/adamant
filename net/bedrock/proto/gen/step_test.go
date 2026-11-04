package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The toy schemas of a step: protocol a and protocol b after it.
const (
	toyFrom = `packet 1 Same
	X uint8
	S Inner
packet 2 Grows client
	X uint8
	Gone string
	N uint8
	L []Inner
	O ?Inner
packet 3 Nested
	C Changed
	R Tree
	P Pair
packet 4 Manual
	M Hand
packet 5 Removed
	X uint8
packet 6 Stack
	I itemstack
packet 7 Enum
	E enum(e) as int32
type Inner
	V varint32
type Changed
	A uint8
type Tree
	Kids []Tree
	C Changed
type Pair
	self[0] Changed
	self[1] Changed
type Hand
	manual coded by hand
type Loose
	Z uint8
enum e
	0 zero
	1 one
`
	toyTo = `packet 1 Same
	X uint8
	S Inner
packet 2 Grows client
	X uint8
	New bool
	N varuint32
	L []Inner
	O ?Inner
packet 3 Nested
	C Changed
	R Tree
	P Pair
packet 4 Manual
	M Hand
packet 6 Stack
	I itemstack
packet 7 Enum
	E enum(e) as int32
packet 8 Added
	Y uint8
type Inner
	V varint32
type Changed
	A uint8
	B string
type Tree
	Kids []Tree
	C Changed
type Pair
	self[0] Changed
	self[1] Changed
type Hand
	manual coded by hand
type Loose
	Z uint8
	Extra uint8
enum e
	0 zero
	1 one
	2 two
`
	// toyHand is what the codec packages write by hand.
	toyHand = `package PKG

import "github.com/dm-vev/adamant/net/bedrock/wire"

type ItemStack struct{ N int32 }

func writeItemStack(w *wire.Writer, x *ItemStack) { w.Varint32(x.N) }
func readItemStack(r *wire.Reader, x *ItemStack)  { x.N = r.Varint32() }

type Hand struct{ S string }

func (x *Hand) Encode(w *wire.Writer) { w.String(x.S) }
func (x *Hand) Decode(r *wire.Reader) { x.S = r.String() }
`
	// toyStepHand is what the step package writes by hand.
	toyStepHand = `package step

import (
	"toy/a"
	"toy/b"
)

var sameHand = []string{"Hand"}

// Loose is held by no packet but by what the package codes by hand.
var convertTypes = []string{"Loose"}

func upItemStack(in *a.ItemStack) b.ItemStack   { return b.ItemStack{N: in.N * 10} }
func downItemStack(in *b.ItemStack) a.ItemStack { return a.ItemStack{N: in.N / 10} }

// fixUpGrowsPacket tells b that a client of a sent Gone.
func fixUpGrowsPacket(in *a.GrowsPacket, out *b.GrowsPacket) { out.New = in.Gone != "" }
`
	// toyStepTest converts packets of the toy step both ways and codes
	// them with the codecs of their protocols.
	toyStepTest = `package step

import (
	"reflect"
	"slices"
	"testing"

	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/wire"
	"toy/a"
	"toy/b"
)

func TestSame(t *testing.T) {
	for id, want := range map[uint32]bool{1: true, 2: false, 3: false, 4: true, 5: false, 6: false, 7: false, 8: false} {
		if Same(id) != want {
			t.Errorf("Same(%d) = %v", id, !want)
		}
	}
	if !slices.Equal(changed, []uint32{2, 3, 6, 7}) || !slices.Equal(added, []uint32{8}) || !slices.Equal(removed, []uint32{5}) {
		t.Fatalf("changed %v, added %v, removed %v", changed, added, removed)
	}
}

// recode encodes a packet with the codecs of its protocol, decodes it and
// encodes it again: the bytes must not change.
func recode(t *testing.T, pk, into proto.Packet) {
	t.Helper()
	var w, again wire.Writer
	pk.Encode(&w)
	r := wire.NewReader(w.B)
	into.Decode(r)
	if r.Err() != nil || r.Len() != 0 {
		t.Fatalf("%T: %v, %d bytes left", into, r.Err(), r.Len())
	}
	if into.Encode(&again); string(again.B) != string(w.B) {
		t.Fatalf("%+v decoded as %+v", pk, into)
	}
}

func TestConvert(t *testing.T) {
	in := &a.GrowsPacket{X: 1, Gone: "g", N: 7, L: []a.Inner{{V: -3}}, O: proto.Some(a.Inner{V: 9})}
	up, ok := upPacket(in)
	want := &b.GrowsPacket{X: 1, New: true, N: 7, L: []b.Inner{{V: -3}}, O: proto.Some(b.Inner{V: 9})}
	if !ok || !reflect.DeepEqual(up, want) {
		t.Fatalf("up %+v, want %+v", up, want)
	}
	recode(t, up, &b.GrowsPacket{})
	down, ok := downPacket(up)
	if back := (&a.GrowsPacket{X: 1, N: 7, L: []a.Inner{{V: -3}}, O: proto.Some(a.Inner{V: 9})}); !ok || !reflect.DeepEqual(down, back) {
		t.Fatalf("down %+v, want %+v", down, back)
	}

	nested := &a.NestedPacket{C: a.Changed{A: 2}, R: a.Tree{Kids: []a.Tree{{C: a.Changed{A: 4}}}}, P: a.Pair{{A: 5}, {A: 6}}}
	up, _ = upPacket(nested)
	if got := up.(*b.NestedPacket); got.C.A != 2 || got.R.Kids[0].C.A != 4 || got.P[1].A != 6 {
		t.Fatalf("up %+v", got)
	}
	recode(t, up, &b.NestedPacket{})
	if down, _ := downPacket(up); !reflect.DeepEqual(down, nested) {
		t.Fatalf("down %+v, want %+v", down, nested)
	}

	if up, _ := upPacket(&a.StackPacket{I: a.ItemStack{N: 3}}); up.(*b.StackPacket).I.N != 30 {
		t.Fatalf("the hand converter of the item stack was not used: %+v", up)
	}
	if loose := upLoose(&a.Loose{Z: 3}); loose != (b.Loose{Z: 3}) || downLoose(&b.Loose{Z: 3, Extra: 1}) != (a.Loose{Z: 3}) {
		t.Fatalf("loose %+v", loose)
	}
	if up, _ := upPacket(&a.EnumPacket{E: 1}); up.(*b.EnumPacket).E != 1 {
		t.Fatalf("enum %+v", up)
	}
	for _, pk := range []proto.Packet{&a.SamePacket{}, &a.ManualPacket{}, &a.RemovedPacket{}} {
		if _, ok := upPacket(pk); ok {
			t.Errorf("up converted %T", pk)
		}
	}
	if _, ok := downPacket(&b.AddedPacket{}); ok {
		t.Error("down converted a packet a has not")
	}
}
`
)

// writeToy writes the toy step into a module of its own, with its codec
// packages generated from the toy schemas, and returns the directory of
// the module.
func writeToy(t *testing.T, stepHand string) string {
	t.Helper()
	net, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	files := map[string]string{
		"go.mod":            "module toy\n\ngo 1.27.0\n\nrequire github.com/dm-vev/adamant/net v0.0.0\n\nreplace github.com/dm-vev/adamant/net => " + net + "\n",
		"a/schema.txt":      toyFrom,
		"b/schema.txt":      toyTo,
		"a/hand.go":         strings.Replace(toyHand, "PKG", "a", 1),
		"b/hand.go":         strings.Replace(toyHand, "PKG", "b", 1),
		"step/hand.go":      stepHand,
		"step/step_test.go": toyStepTest,
	}
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, pkg := range []string{"a", "b"} {
		s, err := parse(filepath.Join(dir, pkg, "schema.txt"))
		if err != nil {
			t.Fatal(err)
		}
		out := filepath.Join(dir, pkg, "packets.go")
		if err := s.findHand(out); err != nil {
			t.Fatal(err)
		}
		s.resolveRaw()
		src, err := s.generate(pkg)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(out, src, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestStepOfToySchemas compares two toy schemas and converts their
// packets: Same holds for the packets alike through every type they hold
// and those of a type coded by hand that sameHand lists; the converters
// copy fields by name, convert integers, lists, optionals, arrays and
// recursive types, use the converters and fixes the package writes by
// hand, and give packets that code as their protocols code them.
func TestStepOfToySchemas(t *testing.T) {
	dir := writeToy(t, toyStepHand)
	if err := runStep(filepath.Join(dir, "a", "schema.txt"), filepath.Join(dir, "b", "schema.txt"), "toy/a", "toy/b", "step",
		filepath.Join(dir, "step", "same_gen.go"), filepath.Join(dir, "step", "convert_gen.go")); err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(filepath.Join(dir, "step", "convert_gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"func upGrowsPacket(in *a.GrowsPacket) *b.GrowsPacket {",
		"// New is new in b.",
		"// Gone is not in b.",
		"out.N = uint32(in.N)",
		"fixUpGrowsPacket(in, out)",
		"out = b.Inner(*in)",
	} {
		if !strings.Contains(string(src), want) {
			t.Errorf("no %q in\n%s", want, src)
		}
	}
	if strings.Contains(string(src), "func upItemStack") || strings.Contains(string(src), "func upSamePacket") {
		t.Errorf("wrote a converter written by hand or of a packet that did not change:\n%s", src)
	}
	cmd := exec.Command("go", "test", "-count=1", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the toy step does not pass its tests: %v\n%s", err, out)
	}
}

// TestStepNeedsWhatIsCodedByHand converts the toy schemas without what the
// package must write by hand: the error names the converters of what is
// coded by hand and the fixes of the fields whose type changes otherwise
// than between integers.
func TestStepNeedsWhatIsCodedByHand(t *testing.T) {
	dir := writeToy(t, "package step\n\nvar sameHand = []string{}\n")
	from := strings.Replace(toyFrom, "\tGone string\n", "\tGone string\n\tLabel string\n", 1)
	to := strings.Replace(toyTo, "\tNew bool\n", "\tNew bool\n\tLabel ?string\n", 1)
	for name, s := range map[string]string{"a": from, "b": to} {
		if err := os.WriteFile(filepath.Join(dir, name, "schema.txt"), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	st, err := newStep(filepath.Join(dir, "a", "schema.txt"), filepath.Join(dir, "b", "schema.txt"), "toy/a", "toy/b", filepath.Join(dir, "step"))
	if err != nil {
		t.Fatal(err)
	}
	if _, changed, _, _ := st.same(); !strings.Contains(joinInts(changed), "4") {
		t.Errorf("a packet of a type coded by hand that sameHand does not list is the same: changed %v", changed)
	}
	_, err = st.writeConvert("step")
	for _, want := range []string{"upItemStack", "downItemStack", "upHand", "downHand", "fixUpGrowsPacket (a string to b proto.Optional[string])", "fixDownGrowsPacket"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("error %v does not name %s", err, want)
		}
	}
}

// TestNodesEqual compares bodies of declarations: a condition, a value of
// a case or a type of a field that differs makes them differ.
func TestNodesEqual(t *testing.T) {
	body := func(schema string) []*node {
		s, err := parse(schemaFile(t, schema))
		if err != nil {
			t.Fatal(err)
		}
		return s.decls[0].body
	}
	base := "type T\n\tK uint8\n\tif K == 1\n\t\tA string\n\telse\n\t\tB uint8\n\tswitch K\n\tcase 2, 3\n\t\tC bool\n"
	if !nodesEqual(body(base), body(base)) {
		t.Fatal("a body differs from itself")
	}
	for _, other := range []string{
		strings.Replace(base, "K == 1", "K == 2", 1),
		strings.Replace(base, "case 2, 3", "case 2, 4", 1),
		strings.Replace(base, "B uint8", "B uint16", 1),
		strings.Replace(base, "\t\tA string\n", "\t\tA string\n\t\tD string\n", 1),
	} {
		if nodesEqual(body(base), body(other)) {
			t.Errorf("equal to\n%s", other)
		}
	}
}

// TestStepsAreCurrent generates the step packages from the schemas of
// their protocols: their same_gen.go and convert_gen.go must be what go
// generate wrote.
func TestStepsAreCurrent(t *testing.T) {
	for _, c := range []struct{ dir, from, to string }{
		{"../convert/c2225", "v2193", "v2225"},
	} {
		const proto = "github.com/dm-vev/adamant/net/bedrock/proto/"
		st, err := newStep("../"+c.from+"/schema.txt", "../"+c.to+"/schema.txt", proto+c.from, proto+c.to, c.dir, "same_gen.go", "convert_gen.go")
		if err != nil {
			t.Fatal(err)
		}
		pkg := filepath.Base(c.dir)
		same, err := st.writeSame(pkg)
		if err != nil {
			t.Fatal(err)
		}
		convert, err := st.writeConvert(pkg)
		if err != nil {
			t.Fatal(err)
		}
		for name, src := range map[string][]byte{"same_gen.go": same, "convert_gen.go": convert} {
			if want, err := os.ReadFile(filepath.Join(c.dir, name)); err != nil || string(want) != string(src) {
				t.Errorf("%s/%s is not what the schemas generate (%v): run go generate", pkg, name, err)
			}
		}
	}
}
