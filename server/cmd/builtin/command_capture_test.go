package builtin

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/scoreboard"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

var updateCommandCaptures = flag.Bool("update-command-captures", false, "rewrite the golden files of the command captures in testdata to the output of the commands now")

// The command captures run a table of command lines against a world that is
// built afresh for every line, and write down the output text of the command
// and the effects it had: what its nested commands saw, the blocks, the
// entities, the scores, the boss bar and the storage of the world. They are
// the regression tests of the tables of subcommand functions: what the code
// before them did is in the golden file, line for line.

// captureWorlds are the three worlds of a capture case.
type captureWorlds struct {
	over, nether, end *world.World
	names             map[*world.World]string
	labels            map[string]string
	log               []string
}

var activeCapture *captureWorlds

// captureProbe is the command a capture runs in place of the real work of a
// nested command: it writes down the execution context it got and returns 7 as
// its result. A capture does not rely on the order that the probes of the
// branches of one line run in, so it sorts the probes it saw.
type captureProbe struct{}

func (captureProbe) Run(src cmd.Source, out *cmd.Output, tx *world.Tx) {
	c := activeCapture
	ctx, ok := cmd.ExecutionContextOf(src)
	if !ok {
		c.log = append(c.log, fmt.Sprintf("probe without context at %v in %s", src.Position(), c.names[tx.World()]))
	} else {
		c.log = append(c.log, fmt.Sprintf("probe executor=%s position=%s rotation=%.4f,%.4f anchor=%d context world=%s tx world=%s",
			c.entityLabel(ctx.Executor), vecString(ctx.Position), ctx.Rotation[0], ctx.Rotation[1], ctx.Anchor, c.names[ctx.World], c.names[tx.World()]))
	}
	out.SetResult(7)
	out.Printf("probe")
}

func (c *captureWorlds) entityLabel(e world.Entity) string {
	if e == nil {
		return "-"
	}
	id := e.H().UUID().String()
	if label, ok := c.labels[id]; ok {
		return label
	}
	// Entities that a command made are named by their type: the order that
	// they are met in does not matter to a capture.
	return strings.TrimPrefix(e.H().Type().EncodeEntity(), "minecraft:")
}

func vecString(v mgl64.Vec3) string { return fmt.Sprintf("(%.4f,%.4f,%.4f)", v[0], v[1], v[2]) }

var uuidPattern = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

// text replaces the UUIDs in s by the labels of the entities they name.
func (c *captureWorlds) text(s string) string {
	return uuidPattern.ReplaceAllStringFunc(s, func(id string) string {
		if label, ok := c.labels[id]; ok {
			return label
		}
		return "<uuid>"
	})
}

// captureSources are the sources of a capture case: the console, and a cow
// that runs the command with the execution context of an entity.
var captureSources = []string{"console", "cow"}

// newCaptureWorlds builds the worlds of a case: stone and dirt in the
// overworld, two cows and a zombie, scores, a boss bar and storage.
func newCaptureWorlds(t *testing.T) *captureWorlds {
	t.Helper()
	conf := func(dim world.Dimension) world.Config {
		return world.Config{Dim: dim, Synchronous: true, RandomTickSpeed: -1, Entities: entity.DefaultRegistry, Provider: world.NopProvider{WorldData: &world.WorldDataMemory{}}}
	}
	c := &captureWorlds{over: conf(world.Overworld).New(), nether: conf(world.Nether).New(), end: conf(world.End).New(), labels: map[string]string{}}
	c.names = map[*world.World]string{c.over: "overworld", c.nether: "nether", c.end: "end"}
	t.Cleanup(func() { c.over.Close(); c.nether.Close(); c.end.Close() })
	return c
}

// seed fills the overworld of a case and returns the cow that runs commands.
func (c *captureWorlds) seed(tx *world.Tx) world.Entity {
	stone, _ := world.BlockByName("minecraft:stone", nil)
	dirt, _ := world.BlockByName("minecraft:dirt", nil)
	tx.SetBlock(cube.Pos{0, 64, 0}, stone, nil)
	tx.SetBlock(cube.Pos{1, 64, 0}, stone, nil)
	tx.SetBlock(cube.Pos{0, 64, 1}, dirt, nil)
	tx.SetBlock(cube.Pos{0, 66, 0}, stone, nil)
	tx.SetBlock(cube.Pos{1, 66, 0}, stone, nil)
	tx.SetBlock(cube.Pos{0, 68, 0}, dirt, nil)
	tx.SetBlock(cube.Pos{1, 68, 0}, stone, nil)
	tx.SetBlock(cube.Pos{3, 64, 3}, block.NewChest(), nil)

	cow := tx.AddEntity(entity.NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{5.5, 64, 5.5}, Rotation: cube.Rotation{90, 10}}))
	other := tx.AddEntity(entity.NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{-3.5, 64, 2.5}}))
	zombie := tx.AddEntity(entity.NewZombie(world.EntitySpawnOpts{Position: mgl64.Vec3{40.5, 64, 0.5}}))
	c.labels[cow.H().UUID().String()] = "cow"
	c.labels[other.H().UUID().String()] = "cow2"
	c.labels[zombie.H().UUID().String()] = "zombie"

	m := tx.World().Scoreboard()
	_ = m.AddObjective("obj", "obj", scoreboard.Dummy)
	_ = m.AddObjective("other", "other", scoreboard.Dummy)
	_ = m.SetScore("obj", "Fake", 5)
	_ = m.SetScore("obj", "Other", 5)
	_ = m.SetScore("other", "Fake", 7)
	_ = m.SetScore("obj", cow.H().UUID().String(), 3)
	_ = m.AddBossBar(scoreboard.BossBar{ID: "bar", Name: "bar", Value: 10, Max: 100, Color: "white", Visible: true})
	_ = tx.World().SaveWorldData("java:storage:st", []byte(`{"a":{"b":1},"s":"text","l":[1,2,3]}`))
	return cow
}

// snapshot writes down the effects of a case: the blocks, entities, scores,
// boss bar and storage.
func (c *captureWorlds) snapshot(tx *world.Tx) []string {
	var lines []string
	for _, line := range c.log {
		lines = append(lines, c.text(line))
	}
	slices.Sort(lines)
	for _, pos := range []cube.Pos{{0, 64, 0}, {1, 64, 0}, {0, 64, 1}, {2, 64, 2}, {1, 64, 1}, {0, 66, 0}, {0, 400, 0}, {3, 64, 3}} {
		name, props := tx.Block(pos).EncodeBlock()
		lines = append(lines, fmt.Sprintf("block %v %s %v", pos, name, props))
	}
	for _, w := range []*world.World{c.over, c.nether, c.end} {
		var entities []string
		w.Exec(func(t *world.Tx) {
			for e := range t.Entities() {
				entities = append(entities, fmt.Sprintf("entity %s %s %s", c.names[w], c.entityLabel(e), vecString(e.Position())))
			}
		})
		slices.Sort(entities)
		lines = append(lines, entities...)
	}
	m := tx.World().Scoreboard()
	var scores []string
	for _, holder := range m.Holders() {
		held := m.HolderScores(holder)
		for _, objective := range m.ObjectiveNames() {
			if v, ok := held[objective]; ok {
				scores = append(scores, fmt.Sprintf("score %s %s %d", c.text(holder), objective, v))
			}
		}
	}
	slices.Sort(scores)
	lines = append(lines, scores...)
	if bar, ok := m.BossBar("bar"); ok {
		lines = append(lines, fmt.Sprintf("bossbar bar value=%v max=%v", bar.Value, bar.Max))
	}
	for _, key := range []string{"st", "new"} {
		if data, err := tx.World().LoadWorldData("java:storage:" + key); err == nil {
			lines = append(lines, fmt.Sprintf("storage %s %s", key, data))
		}
	}
	return lines
}

// runCapture runs every line of lines as the arguments of the command name
// for every source against worlds built afresh, with runner as the command
// registered under name, and returns the written down output and effects.
// extras write down more effects of a case than the snapshot does.
func runCapture(t *testing.T, lines []string, name string, runner cmd.Runnable, extras ...func(c *captureWorlds, tx *world.Tx) []string) string {
	t.Helper()
	previous := map[string]cmd.Command{}
	for _, name := range []string{"_capture_probe_", "setblock", "time", "data", "execute"} {
		if c, ok := cmd.ByAlias(name); ok {
			previous[name] = c
		}
	}
	previousServer := commandServer
	defer func() {
		commandServer = previousServer
		for name, c := range previous {
			cmd.Register(c)
			_ = name
		}
	}()
	cmd.Register(cmd.New("_capture_probe_", "test probe", nil, captureProbe{}))
	cmd.Register(cmd.New("setblock", "", nil, setBlockCommand{}))
	cmd.Register(cmd.New("time", "", nil, timeQueryCommand{}))
	cmd.Register(cmd.New("data", "", nil, dataCommand{}))
	cmd.Register(cmd.New("execute", "", nil, executeCommand{}))
	command := cmd.New(name, "", nil, runner)
	cmd.Register(command)

	var b strings.Builder
	for _, entry := range lines {
		// A line may start with limit=N, the max_command_forks to run it with.
		limit, line := 10000, entry
		if strings.HasPrefix(entry, "limit=") {
			var rest string
			_, rest, _ = strings.Cut(entry, " ")
			fmt.Sscanf(entry, "limit=%d", &limit)
			line = rest
		}
		for _, source := range captureSources {
			c := newCaptureWorlds(t)
			activeCapture = c
			commandServer = executeDimensionAdapter{world: c.over, nether: c.nether, end: c.end}
			fmt.Fprintf(&b, "=== %s | %s\n", source, entry)
			<-c.over.Exec(func(tx *world.Tx) {
				cow := c.seed(tx)
				if err := tx.World().SetGameRule("max_command_forks", limit); err != nil {
					t.Fatal(err)
				}
				out := &cmd.Output{}
				var src cmd.Source = consoleTestSource{pos: mgl64.Vec3{0, 64, 0}, out: out}
				if source == "cow" {
					src = cmd.WithContext(src, cmd.ExecutionContext{Executor: cow, Position: cow.Position(), Rotation: cow.Rotation(), World: tx.World(), Anchor: cmd.Feet})
				}
				command.Execute(line, src, tx)
				for _, err := range out.Errors() {
					fmt.Fprintf(&b, "error: %s\n", c.text(err.Error()))
				}
				for _, m := range out.Messages() {
					fmt.Fprintf(&b, "message: %s\n", c.text(m.String()))
				}
				fmt.Fprintf(&b, "result=%d success=%v\n", out.Result(), out.Success())
				for _, l := range c.snapshot(tx) {
					fmt.Fprintf(&b, "%s\n", l)
				}
				for _, extra := range extras {
					for _, l := range extra(c, tx) {
						fmt.Fprintf(&b, "%s\n", l)
					}
				}
			})
			activeCapture = nil
		}
	}
	return b.String()
}

// checkGolden compares got with the golden file name in testdata.
func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *updateCommandCaptures {
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
		t.Fatalf("read the golden file: %v (write it with: go test ./server/cmd/builtin/ -run %s -update-command-captures)", err, t.Name())
	}
	if got != string(want) {
		checkEqualCaptures(t, string(want), got)
	}
}

// checkEqualCaptures fails the test at the first line where two captures
// differ, naming the command line it belongs to.
func checkEqualCaptures(t *testing.T, want, got string) {
	t.Helper()
	wantLines, gotLines := strings.Split(want, "\n"), strings.Split(got, "\n")
	header := ""
	for i := 0; i < len(gotLines) && i < len(wantLines); i++ {
		if strings.HasPrefix(wantLines[i], "=== ") {
			header = wantLines[i]
		}
		if gotLines[i] != wantLines[i] {
			t.Fatalf("%s\n got: %s\nwant: %s", header, gotLines[i], wantLines[i])
		}
	}
	t.Fatalf("captures differ in length: got %d lines, want %d", len(gotLines), len(wantLines))
}
