package cmd

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/scoreboard"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

var updateSelectorCapture = flag.Bool("update-selector-capture", false, "rewrite testdata/selector_capture.golden to what the selector parser does now")

// The selector capture parses a table of selectors and writes down what each
// parsed to: the error text, or the fields of the selector, what each filter
// of it says about each of a set of candidates, and what it selects from them.
// The golden file is the regression test of the table of selector options.

// selectorEntityType is the type of the entities of the capture.
type selectorEntityType struct{ id string }

func (t selectorEntityType) EncodeEntity() string                      { return t.id }
func (selectorEntityType) BBox(world.Entity) cube.BBox                 { return cube.Box(0, 0, 0, 1, 1, 1) }
func (selectorEntityType) DecodeNBT(map[string]any, *world.EntityData) {}
func (selectorEntityType) EncodeNBT(*world.EntityData) map[string]any {
	return map[string]any{"Age": int16(0), "Health": float32(20), "Inner": map[string]any{"a": int32(1)}}
}
func (selectorEntityType) Open(_ *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return &selectorEntity{handle: handle, data: data}
}

type selectorEntityConfig struct{}

func (selectorEntityConfig) Apply(*world.EntityData) {}

type selectorEntity struct {
	handle *world.EntityHandle
	data   *world.EntityData
}

func (e *selectorEntity) Close() error              { return nil }
func (e *selectorEntity) H() *world.EntityHandle    { return e.handle }
func (e *selectorEntity) Position() mgl64.Vec3      { return e.data.Pos }
func (e *selectorEntity) Rotation() cube.Rotation   { return e.data.Rot }
func (e *selectorEntity) SendCommandOutput(*Output) {}

// selectorPlayer is an entity of the player type that a selector can read
// the name, tags, game mode and experience level of.
type selectorPlayer struct {
	world.Entity
	name  string
	tags  []string
	mode  world.GameMode
	level int
}

func (p *selectorPlayer) Name() string              { return p.name }
func (p *selectorPlayer) CommandTags() []string     { return p.tags }
func (p *selectorPlayer) GameMode() world.GameMode  { return p.mode }
func (p *selectorPlayer) ExperienceLevel() int      { return p.level }
func (p *selectorPlayer) SendCommandOutput(*Output) {}

// selectorCandidates are the targets a selector of the capture chooses from.
type selectorCandidates struct {
	entities []Target
	players  []NamedTarget
	labels   map[Target]string
	cow      Target
	tx       *world.Tx
}

func newSelectorCandidates(tx *world.Tx) *selectorCandidates {
	c := &selectorCandidates{labels: map[Target]string{}, tx: tx}
	spawn := func(id string, pos mgl64.Vec3, rot cube.Rotation) world.Entity {
		return tx.AddEntity(world.EntitySpawnOpts{Position: pos, Rotation: rot}.New(selectorEntityType{id}, selectorEntityConfig{}))
	}
	add := func(label string, t Target) Target {
		c.entities = append(c.entities, t)
		c.labels[t] = label
		return t
	}
	c.cow = add("cow", spawn("minecraft:cow", mgl64.Vec3{5.5, 64, 5.5}, cube.Rotation{90, 10}).(Target))
	add("cow2", spawn("minecraft:cow", mgl64.Vec3{-3.5, 64, 2.5}, cube.Rotation{0, 0}).(Target))
	zombie := add("zombie", spawn("minecraft:zombie", mgl64.Vec3{40.5, 64, 0.5}, cube.Rotation{180, -20}).(Target))
	player := func(name string, pos mgl64.Vec3, rot cube.Rotation, tags []string, mode world.GameMode, level int) {
		p := &selectorPlayer{Entity: spawn("minecraft:player", pos, rot), name: name, tags: tags, mode: mode, level: level}
		add(name, p)
		c.players = append(c.players, p)
	}
	player("Alex", mgl64.Vec3{1, 64, 1}, cube.Rotation{80, 10}, []string{"red"}, world.GameModeSurvival, 5)
	player("Steve", mgl64.Vec3{2, 64, 2}, cube.Rotation{-100, -5}, []string{"blue"}, world.GameModeCreative, 0)
	player("Eve", mgl64.Vec3{30, 70, 30}, cube.Rotation{0, 0}, nil, world.GameModeSpectator, 12)

	m := tx.World().Scoreboard()
	_ = m.AddObjective("obj", "obj", scoreboard.Dummy)
	_ = m.AddObjective("other", "other", scoreboard.Dummy)
	_ = m.SetScore("obj", "Alex", 4)
	_ = m.SetScore("obj", ScoreHolderName(zombie), 9)
	_ = m.SetScore("other", ScoreHolderName(c.cow), 7)
	_ = m.AddTeam("red", "red")
	_ = m.AddTeam("blue", "blue")
	_ = m.JoinTeam("red", "Alex", ScoreHolderName(c.cow))
	_ = m.JoinTeam("blue", "Steve")
	return c
}

// selectorCaptureLines are the selectors the capture parses: every option
// with the values that work and those that fail, negated, repeated and in
// combination.
var selectorCaptureLines = []string{
	// The kinds and the option list as a whole.
	"@a", "@p", "@r", "@s", "@e", "@n", "@x", "@ee", "@e[", "@e]", "@e[]", "@e[x]", "@e[=1]", "@e[unknown=1]",
	"@e[type=cow]junk", "@e junk", "@e[type=cow,]", "@e[,type=cow]", "@e[type=cow,,limit=1]", "@e[ type = cow , limit = 1 ]",
	"@a[predicate=x]", "@a[advancements={}]", "@e[predicate=!x]",
	// x, y, z, dx, dy, dz.
	"@e[x=5.5,y=64,z=5.5,distance=..1]", "@e[x=-3.5,z=2.5,distance=..1]", "@e[x=abc]", "@e[x=1,x=2]", "@e[y=1,y=2]", "@e[z=1,z=2]",
	"@e[x=!1]", "@e[x=]", "@e[x=NaN]", "@e[x=1e2]", "@e[x=1,y=2,z=3]",
	"@e[x=0,y=60,z=0,dx=10,dy=10,dz=10]", "@e[x=0,y=60,z=0,dx=10]", "@e[x=0,y=60,z=0,dy=10]", "@e[x=0,y=60,z=0,dz=10]",
	"@e[x=0,y=60,z=0,dx=-10,dy=-10,dz=-10]", "@e[x=0,y=63,z=0,dx=6,dy=1,dz=6]", "@e[dx=5,dy=5,dz=5]", "@e[dx=abc]", "@e[dx=1,dx=2]", "@e[dy=1,dy=2]", "@e[dz=1,dz=2]",
	"@e[dx=!1]", "@e[dx=1e2]",
	// distance.
	"@e[distance=..10]", "@e[distance=3..8]", "@e[distance=5]", "@e[distance=..]", "@e[distance=-1..]", "@e[distance=..-1]", "@e[distance=3..1]",
	"@e[distance=abc]", "@e[distance=!1]", "@e[distance=1,distance=2]", "@e[distance=0..]", "@e[distance=7.8..8]", "@e[distance=]",
	// level.
	"@a[level=5]", "@a[level=1..]", "@a[level=..4]", "@a[level=-1..]", "@a[level=1.5]", "@a[level=2..1]", "@a[level=..-1]", "@e[level=0..]",
	"@a[level=!1]", "@a[level=1,level=2]", "@a[level=abc]", "@a[level=]", "@a[level=0..0]",
	// x_rotation and y_rotation.
	"@e[y_rotation=80..100]", "@e[x_rotation=5..15]", "@e[y_rotation=170..-170]", "@e[x_rotation=-90..0]", "@e[y_rotation=..90]", "@e[y_rotation=90..]",
	"@e[x_rotation=abc]", "@e[y_rotation=1,y_rotation=2]", "@e[x_rotation=1,x_rotation=2]", "@e[y_rotation=!1]", "@e[y_rotation=]", "@e[y_rotation=-180..-90]",
	"@e[y_rotation=180]", "@e[x_rotation=10]", "@e[x_rotation=..]",
	// scores.
	"@e[scores={obj=4}]", "@e[scores={obj=1..9}]", "@e[scores={obj=..3}]", "@e[scores={obj=4,other=7}]", "@e[scores={other=7}]", "@e[scores={}]",
	"@e[scores={obj}]", "@e[scores={obj=1.5}]", "@e[scores={nothing=0}]", "@e[scores=obj=1]", "@e[scores={obj=4]", "@e[scores=obj=4}]",
	"@e[scores={ obj = 4 }]", "@e[scores={obj=4},scores={obj=4}]", "@e[scores=!{obj=4}]", "@e[scores={obj=2..1}]", "@e[scores={obj=abc}]", "@e[scores=]",
	"@e[scores={obj=9,}]", "@e[scores={obj=4},tag=red]",
	// team.
	"@e[team=red]", "@e[team=!red]", "@e[team=]", "@e[team=!]", "@e[team=blue]", "@e[team=nothing]", "@e[team=red,team=blue]", "@e[team=!red,team=!blue]",
	"@e[team=red,team=!blue]", "@e[team=!red,team=blue]", "@e[team=!red,team=red]", "@e[team=,team=!red]", "@e[team=!red,team=]",
	// nbt.
	"@e[nbt={Age:0s}]", "@e[nbt={Age:0}]", "@e[nbt={Age:1s}]", "@e[nbt=!{Age:0s}]", "@e[nbt={Health:20f}]", "@e[nbt={Health:20}]", "@e[nbt={Inner:{a:1}}]",
	"@e[nbt={Inner:{a:2}}]", "@e[nbt={Pos:[40.5d,64d,0.5d]}]", "@e[nbt={Pos:[40.5d]}]", "@e[nbt={}]", "@e[nbt={a:}]", "@e[nbt={Age:0s}b]", "@e[nbt=]",
	"@e[nbt=5]", "@e[nbt={Age:0s},nbt={Health:20f}]", "@e[nbt={Age:0s},nbt=!{Health:20f}]", "@e[nbt={Age:0s} ]", "@e[nbt={Rotation:[90f,10f]}]",
	"@e[nbt={Rotation:[90f]}]", "@e[nbt={Tags:[\"a\"]}]",
	// limit and sort.
	"@e[limit=1]", "@e[limit=2]", "@e[limit=0]", "@e[limit=-1]", "@e[limit=1.5]", "@e[limit=abc]", "@e[limit=1,limit=2]", "@e[limit=!1]", "@e[limit=]",
	"@a[limit=1]", "@s[limit=1]", "@p[limit=2]", "@r[limit=2]", "@n[limit=2]", "@e[limit=100]",
	"@e[sort=nearest]", "@e[sort=furthest]", "@e[sort=random]", "@e[sort=arbitrary]", "@e[sort=closest]", "@e[sort=]", "@e[sort=nearest,sort=furthest]",
	"@s[sort=nearest]", "@a[sort=nearest]", "@p[sort=furthest]", "@e[sort=nearest,limit=2]", "@e[sort=furthest,limit=2]", "@e[sort=random,limit=2]",
	"@e[limit=2,sort=nearest]", "@e[sort=!nearest]", "@a[sort=random]", "@r", "@r[type=cow]", "@r[type=minecraft:player]", "@p[level=1..]",
	// type.
	"@e[type=cow]", "@e[type=minecraft:cow]", "@e[type=!cow]", "@e[type=cow,type=zombie]", "@e[type=!cow,type=!zombie]", "@e[type=cow,type=!zombie]",
	"@e[type=!cow,type=zombie]", "@e[type=!cow,type=cow]", "@e[type=#minecraft:zombies]", "@e[type=#minecraft:undead]", "@e[type=!#minecraft:undead]",
	"@e[type=#bogus]", "@e[type=!#bogus]", "@e[type=#minecraft:undead,type=#minecraft:undead]", "@e[type=#minecraft:undead,type=#minecraft:zombies]",
	"@e[type=#minecraft:undead,type=!cow]", "@e[type=!cow,type=#minecraft:undead]", "@e[type=cow,type=#minecraft:undead]", "@e[type=#minecraft:undead,type=cow]",
	"@e[type=nothing]", "@e[type=!nothing]", "@e[type=player]", "@e[type=minecraft:player]", "@e[type=]", "@e[type=!]", "@e[type=#]", "@e[type=Cow]", "@e[type=zombie]",
	"@a[type=player]", "@a[type=cow]", "@e[type=minecraft:]",
	// name.
	"@e[name=Alex]", "@e[name=\"Alex\"]", "@e[name=!Alex]", "@e[name=Alex,name=Steve]", "@e[name=!Alex,name=!Steve]", "@e[name=Alex,name=!Steve]",
	"@e[name=!Alex,name=Steve]", "@e[name=alex]", "@e[name=]", "@e[name=\"\"]", "@e[name=\"A,B\"]", "@e[name=!]", "@a[name=Eve]", "@a[name=!Eve,name=!Alex]",
	// tag.
	"@e[tag=red]", "@e[tag=!red]", "@e[tag=]", "@e[tag=!]", "@e[tag=red,tag=blue]", "@e[tag=red,tag=!blue]", "@e[tag=!red,tag=!blue]", "@e[tag=nothing]",
	"@a[tag=blue]", "@a[tag=,tag=!red]", "@e[tag=red,tag=red]",
	// gamemode.
	"@a[gamemode=survival]", "@a[gamemode=creative]", "@a[gamemode=adventure]", "@a[gamemode=spectator]", "@a[gamemode=bogus]", "@a[gamemode=]",
	"@a[gamemode=!creative]", "@a[gamemode=!survival,gamemode=!creative]", "@a[gamemode=survival,gamemode=creative]", "@a[gamemode=survival,gamemode=!creative]",
	"@a[gamemode=!survival,gamemode=creative]", "@a[gamemode=!bogus]", "@e[gamemode=survival]", "@a[gamemode=!survival,gamemode=!survival]",
	"@a[gamemode=Survival]", "@a[gamemode=0]", "@a[gamemode=!survival,gamemode=survival]",
	// combinations.
	"@e[type=cow,distance=..10,sort=furthest,limit=1]", "@e[type=!player,sort=nearest,limit=2]", "@a[level=1..,gamemode=survival,tag=red,team=red,scores={obj=4},name=Alex]",
	"@a[level=1..,gamemode=survival,tag=red,team=red,scores={obj=4},name=!Alex]", "@e[x=0,y=60,z=0,dx=10,dy=10,dz=10,type=!player,team=,tag=]",
	"@e[nbt={Age:0s},type=cow,name=,tag=,team=red,scores={other=7},x_rotation=0..20,y_rotation=80..100,distance=..10,limit=1,sort=nearest]",
	"@s", "@s[type=cow]", "@s[distance=..1]", "@s[name=Alex]", "@s[tag=red]", "@s[team=red]", "@s[scores={obj=1..}]", "@s[x=1,y=2,z=3]", "@s[type=!cow]",
	"@p[gamemode=!creative]", "@p[tag=blue]", "@a[sort=furthest]x",
}

// selectorCaptureSources are the sources a selector is parsed for: a bare
// source, and one with the cow as its executor, which @s refers to.
var selectorCaptureSources = []string{"source", "cow"}

// runSelectorCapture parses every line for every source with parse and
// returns what it written down.
func runSelectorCapture(t *testing.T, parse func(text string, src Source, tx *world.Tx) (*entitySelector, error)) string {
	t.Helper()
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	var b strings.Builder
	<-w.Exec(func(tx *world.Tx) {
		c := newSelectorCandidates(tx)
		for _, line := range selectorCaptureLines {
			for _, name := range selectorCaptureSources {
				var src Source = testSource{pos: mgl64.Vec3{0, 64, 0}}
				if name == "cow" {
					src = WithContext(src, ExecutionContext{Executor: c.cow.(world.Entity), Position: c.cow.Position(), Rotation: c.cow.(world.Entity).Rotation(), World: tx.World()})
				}
				fmt.Fprintf(&b, "=== %s | %s\n", name, line)
				c.write(&b, line, src, parse)
			}
		}
	})
	return b.String()
}

// write parses text and writes down the selector it parsed to.
func (c *selectorCandidates) write(b *strings.Builder, text string, src Source, parse func(string, Source, *world.Tx) (*entitySelector, error)) {
	s, err := parse(text, src, c.tx)
	if err != nil {
		fmt.Fprintf(b, "error: %s\n", err)
		return
	}
	volume := "-"
	if s.volume != nil {
		volume = fmt.Sprint(*s.volume)
	}
	fmt.Fprintf(b, "kind=%c origin=%v volume=%s distance=%+v level=%+v limit=%d sort=%s filters=%d\n", s.kind, s.origin, volume, s.distance, s.level, s.limit, s.sort, len(s.filters))
	for i, f := range s.filters {
		fmt.Fprintf(b, "filter %d:", i)
		for _, t := range c.entities {
			fmt.Fprintf(b, " %s=%v", c.labels[t], f(t))
		}
		b.WriteString("\n")
	}
	b.WriteString("matches:")
	for _, t := range c.entities {
		fmt.Fprintf(b, " %s=%v", c.labels[t], s.matches(t))
	}
	b.WriteString("\n")
	selected := s.selectTargets(src, c.entities, c.players)
	var labels []string
	for _, t := range selected {
		label, ok := c.labels[t]
		if !ok {
			label = "?"
		}
		labels = append(labels, label)
	}
	if s.sort == "random" {
		// The order and the choice among the matches are random.
		fmt.Fprintf(b, "selected: %d\n", len(labels))
		return
	}
	fmt.Fprintf(b, "selected: %s\n", strings.Join(labels, ","))
}

// TestSelectorTableMatchesLegacy requires the table of selector options to do
// exactly what parseSelector did before it, on every selector of the capture:
// the same error texts, the same fields and the same filters.
func TestSelectorTableMatchesLegacy(t *testing.T) {
	legacy, table := runSelectorCapture(t, parseSelector), runSelectorCapture(t, parseSelectorTable)
	if legacy != table {
		reportCaptureDifference(t, legacy, table)
	}
}

// TestSelectorCapture writes down what parseSelector does for every selector
// of selectorCaptureLines.
func TestSelectorCapture(t *testing.T) {
	got := runSelectorCapture(t, parseSelector)
	path := filepath.Join("testdata", "selector_capture.golden")
	if *updateSelectorCapture {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the golden file: %v (write it with: go test ./server/cmd/ -run TestSelectorCapture -update-selector-capture)", err)
	}
	if got != string(want) {
		reportCaptureDifference(t, string(want), got)
	}
}

// reportCaptureDifference fails the test at the first line where two captures
// differ, naming the selector it belongs to.
func reportCaptureDifference(t *testing.T, want, got string) {
	t.Helper()
	wantLines, gotLines := strings.Split(want, "\n"), strings.Split(got, "\n")
	header := ""
	for i := 0; i < len(gotLines) && i < len(wantLines); i++ {
		if strings.HasPrefix(wantLines[i], "=== ") {
			header = wantLines[i]
		}
		if gotLines[i] != wantLines[i] {
			t.Fatalf("%s\n got: %s\nwant: %s\n(if the change is deliberate, rewrite the golden file with -update-selector-capture)", header, gotLines[i], wantLines[i])
		}
	}
	t.Fatalf("captures differ in length: got %d lines, want %d", len(gotLines), len(wantLines))
}
