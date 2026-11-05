package cmd

import (
	"fmt"
	"math"
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// selectorTarget is a named target with the state selector options read.
type selectorTarget struct {
	name  string
	pos   mgl64.Vec3
	rot   cube.Rotation
	tags  []string
	mode  world.GameMode
	level int
}

func (s *selectorTarget) Position() mgl64.Vec3      { return s.pos }
func (s *selectorTarget) Name() string              { return s.name }
func (s *selectorTarget) Rotation() cube.Rotation   { return s.rot }
func (s *selectorTarget) CommandTags() []string     { return s.tags }
func (s *selectorTarget) GameMode() world.GameMode  { return s.mode }
func (s *selectorTarget) ExperienceLevel() int      { return s.level }
func (s *selectorTarget) SendCommandOutput(*Output) {}

// MinMaxBounds.Doubles.fromReader: a single value, or a range with at most
// one open end.
func TestSelectorRanges(t *testing.T) {
	inf := math.Inf(1)
	for _, c := range []struct {
		in       string
		min, max float64
		ok       bool
	}{
		{"5", 5, 5, true},
		{"1..5", 1, 5, true},
		{"..5", -inf, 5, true},
		{"1..", 1, inf, true},
		{"-2.5..-1", -2.5, -1, true},
		{"..", 0, 0, false},
		{"", 0, 0, false},
		{"a..1", 0, 0, false},
		{"NaN", 0, 0, false},
		{"1..Inf", 0, 0, false},
	} {
		r, err := parseSelectorRange(c.in)
		if (err == nil) != c.ok || c.ok && (r.min != c.min || r.max != c.max) {
			t.Errorf("range %q = [%v, %v], %v; want [%v, %v], ok=%v", c.in, r.min, r.max, err, c.min, c.max, c.ok)
		}
	}
	// MinMaxBounds.Ints: integer ends, and a minimum above the maximum is
	// argument.range.swapped.
	for in, ok := range map[string]bool{"3": true, "1..3": true, "..0": true, "1.5": false, "1..2.5": false, "3..1": false} {
		if _, err := parseIntRange(in); (err == nil) != ok {
			t.Errorf("int range %q: err %v, want ok=%v", in, err, ok)
		}
	}
	r, _ := parseSelectorRange("1..3")
	for v, want := range map[float64]bool{0.9: false, 1: true, 3: true, 3.1: false} {
		if r.contains(v) != want {
			t.Errorf("1..3 contains %v = %v", v, !want)
		}
	}
	if !(selectorRange{}).contains(-1e9) {
		t.Error("an unset range rejected a value")
	}
}

// EntitySelectorParser rejects unknown selector kinds, malformed option lists,
// unknown options and negation of options that have none
// (EntitySelectorOptions registers `!` for type, name, tag, team, gamemode,
// nbt and predicate only), and validates each option's value.
func TestSelectorSyntax(t *testing.T) {
	valid := []string{
		"@a", "@p", "@r", "@s", "@e", "@n", "@a[]",
		"@e[x=1,y=2,z=3,dx=1,dy=1,dz=1]",
		"@e[x=-1.5, distance=..4.5]",
		"@a[limit=3,sort=furthest]",
		"@a[level=0..,tag=!a,tag=b,name=!Steve,gamemode=!creative]",
		"@e[x_rotation=-90..0,y_rotation=170..-170]",
	}
	for _, s := range valid {
		if _, err := parseSelector(s, testSource{}, nil); err != nil {
			t.Errorf("%s: %v", s, err)
		}
	}
	invalid := []string{
		"@x", "@a[", "@a]", "@a[x]", "@a[unknown=1]",
		"@a[x=!1]", "@a[limit=!2]", "@a[distance=!1]",
		"@a[x=NaN]", "@a[dx=1e2]",
		"@a[limit=0]", "@a[limit=-1]", "@a[limit=1.5]",
		"@a[sort=closest]",
		"@a[distance=-1..]", "@a[distance=3..1]",
		"@a[level=-1..]", "@a[level=1.5]", "@a[level=2..1]",
		"@a[scores=kills=1]", "@a[scores={kills}]", "@a[scores={kills=1.5}]",
		"@a[nbt={a:}]", "@a[nbt={a:1}b]",
	}
	for _, s := range invalid {
		if _, err := parseSelector(s, testSource{}, nil); err == nil {
			t.Errorf("%s was accepted", s)
		}
	}
}

func TestSplitSelectorOptions(t *testing.T) {
	for in, want := range map[string][]string{
		"":                             nil,
		"a=1":                          {"a=1"},
		"a=1,b=2":                      {"a=1", "b=2"},
		"a=1,":                         {"a=1"},
		"scores={x=1,y=2..},tag=t":     {"scores={x=1,y=2..}", "tag=t"},
		`name="a,b",nbt={L:[1,2]},c=3`: {`name="a,b"`, "nbt={L:[1,2]}", "c=3"},
	} {
		if got := splitSelectorOptions(in); !slices.Equal(got, want) {
			t.Errorf("split %q = %q, want %q", in, got, want)
		}
	}
}

// selectNames parses selector as run by src and returns the names of the
// targets it selects from the candidates, in order.
func selectNames(t *testing.T, selector string, src Source, candidates ...*selectorTarget) []string {
	t.Helper()
	s, err := parseSelector(selector, src, nil)
	if err != nil {
		t.Fatalf("%s: %v", selector, err)
	}
	var entities []Target
	var players []NamedTarget
	for _, c := range candidates {
		entities, players = append(entities, c), append(players, c)
	}
	var names []string
	for _, target := range s.selectTargets(src, entities, players) {
		names = append(names, target.(NamedTarget).Name())
	}
	return names
}

// EntitySelector.findEntities filters, then sorts and limits: @p and @n are
// nearest with limit 1 and @a and @e are arbitrary without a limit, and both
// limit and sort override those defaults (EntitySelectorOptions limit and
// sort). Distance, volume and level bounds are inclusive. A volume spans
// whole blocks: dx=0 covers the block at x. An open rotation end is 0 or 359
// degrees before wrapping (EntitySelectorParser.createRotationPredicate), so
// x_rotation=..0 matches only a pitch of 0 and x_rotation=45.. wraps to
// [45, -1] and matches pitches up to -1 too.
func TestSelectorSelection(t *testing.T) {
	near := &selectorTarget{name: "near", pos: mgl64.Vec3{1, 0, 0}, tags: []string{"red"}, mode: world.GameModeSurvival, level: 5, rot: cube.Rotation{90, -30}}
	mid := &selectorTarget{name: "mid", pos: mgl64.Vec3{0, 0, 3}, tags: []string{"red", "blue"}, mode: world.GameModeCreative, level: 30}
	far := &selectorTarget{name: "far", pos: mgl64.Vec3{-6, 0, -8}, mode: world.GameModeSpectator, rot: cube.Rotation{-170, 45}}
	all := []*selectorTarget{far, mid, near}
	origin := testSource{}

	for _, c := range []struct {
		selector string
		src      Source
		want     []string
	}{
		{"@a", origin, []string{"far", "mid", "near"}},
		{"@e", origin, []string{"far", "mid", "near"}},
		{"@p", origin, []string{"near"}},
		{"@n", origin, []string{"near"}},
		{"@p", testSource{pos: mgl64.Vec3{-5, 0, -8}}, []string{"far"}},
		{"@p[limit=2]", origin, []string{"near", "mid"}},
		{"@a[sort=nearest]", origin, []string{"near", "mid", "far"}},
		{"@a[sort=furthest,limit=1]", origin, []string{"far"}},
		{"@a[limit=2]", origin, []string{"far", "mid"}},
		{"@a[distance=..3]", origin, []string{"mid", "near"}},
		{"@a[distance=3..]", origin, []string{"far", "mid"}},
		{"@a[distance=10]", origin, []string{"far"}},
		{"@a[x=-6,y=0,z=-8,distance=..1]", origin, []string{"far"}},
		{"@a[x=0,y=0,z=0,dx=1,dy=0,dz=0]", origin, []string{"near"}},
		{"@a[x=0,y=-1,z=0,dx=0,dy=0,dz=3]", origin, []string{"mid", "near"}},
		{"@a[x=0,y=0,z=0,dx=-6,dz=-8]", origin, []string{"far", "near"}},
		{"@a[tag=red]", origin, []string{"mid", "near"}},
		{"@a[tag=red,tag=blue]", origin, []string{"mid"}},
		{"@a[tag=!blue]", origin, []string{"far", "near"}},
		{"@a[tag=]", origin, []string{"far"}},
		{"@a[tag=!]", origin, []string{"mid", "near"}},
		{"@a[name=mid]", origin, []string{"mid"}},
		{`@a[name="mid"]`, origin, []string{"mid"}},
		{"@a[name=!mid]", origin, []string{"far", "near"}},
		{"@a[gamemode=creative]", origin, []string{"mid"}},
		{"@a[gamemode=!survival]", origin, []string{"far", "mid"}},
		{"@a[gamemode=spectator]", origin, []string{"far"}},
		{"@a[level=5..30]", origin, []string{"mid", "near"}},
		{"@a[level=..4]", origin, []string{"far"}},
		{"@a[y_rotation=90]", origin, []string{"near"}},
		{"@a[x_rotation=..0]", origin, []string{"mid"}},
		{"@a[y_rotation=170..-170]", origin, []string{"far"}},
		{"@a[x_rotation=45..]", origin, []string{"far", "near"}},
		{"@s", near, []string{"near"}},
		{"@s[tag=blue]", near, nil},
	} {
		if got := selectNames(t, c.selector, c.src, all...); !slices.Equal(got, c.want) {
			t.Errorf("%s = %q, want %q", c.selector, got, c.want)
		}
	}
}

// @s selects the executor of an execute context, and nothing when execute
// has none (the console running "execute positioned ... run").
func TestSelectorSelfInExecuteContext(t *testing.T) {
	near := &selectorTarget{name: "near"}
	if got := selectNames(t, "@s", WithContext(near, ExecutionContext{}), near); len(got) != 0 {
		t.Fatalf("@s without an executor = %q", got)
	}
	s, err := parseSelector("@s", testSource{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.selectTargets(nil, nil, nil); len(got) != 0 {
		t.Fatalf("@s of a nil source = %v", got)
	}
}

// Selectors parse relative to the execution position: `@p` in
// "execute positioned 0 0 -8 run" is the player nearest that point.
func TestSelectorUsesExecutePosition(t *testing.T) {
	a := &selectorTarget{name: "a", pos: mgl64.Vec3{0, 0, 1}}
	b := &selectorTarget{name: "b", pos: mgl64.Vec3{0, 0, -7}}
	src := WithContext(testSource{}, ExecutionContext{Position: mgl64.Vec3{0, 0, -8}})
	if got := selectNames(t, "@p", src, a, b); !slices.Equal(got, []string{"b"}) {
		t.Fatalf("@p = %q, want b", got)
	}
}

// NbtUtils.compareNbt: every tag of the expected compound must be in the
// actual one with the same type and value, extra tags are allowed, every
// element of an expected list must match some element of the actual list,
// an empty expected list matches only an empty list, and arrays compare
// whole.
func TestNBTContains(t *testing.T) {
	actual := map[string]any{
		"Health":   float32(20),
		"OnGround": uint8(1),
		"Tags":     []any{"a", "b", "c"},
		"Pos":      []float64{1, 64, 2},
		"Bytes":    []uint8{1, 2},
		"Ints":     []int32{1, 2, 3},
		"Inner":    map[string]any{"Id": int32(7), "Name": "x"},
		"Empty":    []any{},
	}
	for _, c := range []struct {
		name     string
		expected any
		want     bool
	}{
		{"nil matches anything", nil, true},
		{"empty compound", map[string]any{}, true},
		{"subset", map[string]any{"Health": float32(20)}, true},
		{"wrong value", map[string]any{"Health": float32(19)}, false},
		{"wrong type", map[string]any{"Health": float64(20)}, false},
		{"missing tag", map[string]any{"Absent": int32(0)}, false},
		{"bool folds to byte", map[string]any{"OnGround": true}, true},
		{"signed byte folds to byte", map[string]any{"OnGround": int8(1)}, true},
		{"nested subset", map[string]any{"Inner": map[string]any{"Id": int32(7)}}, true},
		{"nested mismatch", map[string]any{"Inner": map[string]any{"Id": int32(8)}}, false},
		{"list subset in any order", map[string]any{"Tags": []any{"c", "a"}}, true},
		{"list element missing", map[string]any{"Tags": []any{"d"}}, false},
		{"typed list element", map[string]any{"Pos": []float64{64}}, true},
		{"empty list matches empty", map[string]any{"Empty": []any{}}, true},
		{"empty list rejects non-empty", map[string]any{"Tags": []any{}}, false},
		{"list longer than actual", map[string]any{"Pos": []float64{1, 1, 1, 1}}, false},
		{"byte array whole", map[string]any{"Bytes": []uint8{1, 2}}, true},
		{"signed byte array whole", map[string]any{"Bytes": []int8{1, 2}}, true},
		{"byte array prefix", map[string]any{"Bytes": []uint8{1}}, false},
		{"int array prefix", map[string]any{"Ints": []int32{1, 2}}, false},
		{"array is not a list", map[string]any{"Ints": []any{int32(1)}}, false},
	} {
		if got := nbtContains(c.expected, actual); got != c.want {
			t.Errorf("%s: nbtContains = %v, want %v", c.name, got, c.want)
		}
	}
	if nbtContains(map[string]any{}, nil) {
		t.Error("a compound matched a missing tag")
	}
}

// EntitySelectorOptions refuses an option that does not apply: limit and sort
// for @s, whose isCurrentEntity holds, a second x, distance or sort, a second
// positive name, gamemode, team or type, a positive one after a negated one,
// and a negative distance or level on either end. The refusals carry Java's
// texts: argument.entity.options.inapplicable, .distance.negative and so on.
func TestSelectorRefusesWhatJavaRefuses(t *testing.T) {
	const (
		inapplicable = "Option '%s' isn't applicable here"
		distance     = "Distance cannot be negative"
		level        = "Level shouldn't be negative"
	)
	for _, c := range []struct{ selector, want string }{
		{"@s[limit=1]", fmt.Sprintf(inapplicable, "limit")},
		{"@s[sort=nearest]", fmt.Sprintf(inapplicable, "sort")},
		{"@a[distance=..-1]", distance},
		{"@a[distance=-3..-1]", distance},
		{"@a[distance=-1]", distance},
		{"@a[level=..-1]", level},
		{"@a[level=-1..]", level},
		{"@a[limit=1,limit=2]", fmt.Sprintf(inapplicable, "limit")},
		{"@a[sort=nearest,sort=random]", fmt.Sprintf(inapplicable, "sort")},
		{"@a[x=1,x=2]", fmt.Sprintf(inapplicable, "x")},
		{"@a[dy=1,dy=2]", fmt.Sprintf(inapplicable, "dy")},
		{"@a[distance=..1,distance=..2]", fmt.Sprintf(inapplicable, "distance")},
		{"@a[level=1..,level=..2]", fmt.Sprintf(inapplicable, "level")},
		{"@a[x_rotation=0..1,x_rotation=0..2]", fmt.Sprintf(inapplicable, "x_rotation")},
		{"@a[scores={a=1},scores={b=1}]", fmt.Sprintf(inapplicable, "scores")},
		{"@a[name=A,name=B]", fmt.Sprintf(inapplicable, "name")},
		{"@a[name=!A,name=B]", fmt.Sprintf(inapplicable, "name")},
		{"@a[gamemode=creative,gamemode=!survival]", fmt.Sprintf(inapplicable, "gamemode")},
		{"@a[team=a,team=b]", fmt.Sprintf(inapplicable, "team")},
		{"@e[type=pig,type=cow]", fmt.Sprintf(inapplicable, "type")},
		{"@e[type=!pig,type=cow]", fmt.Sprintf(inapplicable, "type")},
		{"@e[type=#boats,type=#boats]", fmt.Sprintf(inapplicable, "type")},
		{"@a[gamemode=sneaky]", "Unknown or illegal game mode 'sneaky'"},
		{"@a[sort=closest]", "Invalid or unknown sort type 'closest'"},
		{"@a[limit=0]", "The minimum number allowed is 1"},
		{"@a[bogus=1]", "Unknown option 'bogus'"},
	} {
		_, err := parseSelector(c.selector, testSource{}, nil)
		if err == nil || err.Error() != c.want {
			t.Errorf("%s: error %v, want %q", c.selector, err, c.want)
		}
	}
	for _, accepted := range []string{
		"@p[limit=2]", "@e[limit=2,sort=random]", "@s[tag=a]", "@a[distance=0..]", "@a[level=0]",
		"@a[name=!A,name=!B]", "@e[type=!pig,type=!cow]", "@e[type=#boats,type=!pig]", "@e[type=#a,type=#b]",
		"@a[gamemode=!creative,gamemode=!survival]", "@a[tag=a,tag=b]",
	} {
		if _, err := parseSelector(accepted, testSource{}, nil); err != nil {
			t.Errorf("%s: %v", accepted, err)
		}
	}
}
