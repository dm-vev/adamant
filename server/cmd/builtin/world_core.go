package builtin

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

type worldOpCommand struct{ srv serverAdapter }

func (c worldOpCommand) Allow(src cmd.Source) bool { return operatorAllowed(c.srv, src) }

// commandServer is the adapter passed to Register; opGate consults it so
// runnables can require Java permission level 2 without carrying the adapter.
var commandServer serverAdapter

// opGate restricts a runnable to operators and non-player sources.
type opGate struct{}

func (opGate) Allow(src cmd.Source) bool { return operatorAllowed(commandServer, src) }

// operatorAllowed unwraps execute contexts to the originating source. Player
// sources need operator status; console and other sources are trusted.
func operatorAllowed(srv serverAdapter, src cmd.Source) bool {
	for {
		switch source := src.(type) {
		case *player.Player:
			return srv != nil && srv.IsOperator(source.Name())
		case cmd.ContextSource:
			src = source.Source
		default:
			return true
		}
	}
}

type setBlockCommand struct {
	worldOpCommand
	X     int    `cmd:"x"`
	Y     int    `cmd:"y"`
	Z     int    `cmd:"z"`
	Block string `cmd:"block"`
}
type fillCommand struct {
	worldOpCommand
	X1    int    `cmd:"x1"`
	Y1    int    `cmd:"y1"`
	Z1    int    `cmd:"z1"`
	X2    int    `cmd:"x2"`
	Y2    int    `cmd:"y2"`
	Z2    int    `cmd:"z2"`
	Block string `cmd:"block"`
}
type cloneCommand struct {
	worldOpCommand
	X1 int `cmd:"x1"`
	Y1 int `cmd:"y1"`
	Z1 int `cmd:"z1"`
	X2 int `cmd:"x2"`
	Y2 int `cmd:"y2"`
	Z2 int `cmd:"z2"`
	DX int `cmd:"dx"`
	DY int `cmd:"dy"`
	DZ int `cmd:"dz"`
}
type weatherCommand struct {
	worldOpCommand
	Mode     string            `cmd:"mode"`
	Duration cmd.Optional[int] `cmd:"duration"`
}
type difficultyCommand struct {
	worldOpCommand
	Difficulty cmd.Optional[string] `cmd:"difficulty"`
}
type borderCommand struct {
	worldOpCommand
	Action string      `cmd:"action"`
	Values cmd.Varargs `cmd:"values"`
}
type gameruleCommand struct {
	worldOpCommand
	Rule  string               `cmd:"rule"`
	Value cmd.Optional[string] `cmd:"value"`
}
type seedCommand struct{ worldOpCommand }
type summonCommand struct {
	worldOpCommand
	Entity string  `cmd:"entity"`
	X      float64 `cmd:"x"`
	Y      float64 `cmd:"y"`
	Z      float64 `cmd:"z"`
}
type setWorldSpawnCommand struct {
	worldOpCommand
	X int `cmd:"x"`
	Y int `cmd:"y"`
	Z int `cmd:"z"`
}
type defaultGameModeCommand struct {
	worldOpCommand
	Mode string `cmd:"mode"`
}
type locateCommand struct {
	worldOpCommand
	Kind   string `cmd:"kind"`
	Target string `cmd:"target"`
}

func registerWorldCoreCommands(srv serverAdapter) {
	guard := worldOpCommand{srv: srv}
	cmd.Register(cmd.New("setblock", "Sets a block at a position.", nil, setBlockCommand{worldOpCommand: guard}))
	cmd.Register(cmd.New("fill", "Fills a region with a block.", nil, fillCommand{worldOpCommand: guard}))
	cmd.Register(cmd.New("clone", "Copies blocks from one region to another.", nil, cloneCommand{worldOpCommand: guard}))
	cmd.Register(cmd.New("weather", "Changes the weather.", nil, weatherCommand{worldOpCommand: guard}))
	cmd.Register(cmd.New("difficulty", "Changes or queries the world difficulty.", nil, difficultyCommand{worldOpCommand: guard}))
	cmd.Register(cmd.New("worldborder", "Changes world border settings.", nil, borderCommand{worldOpCommand: guard}))
	cmd.Register(cmd.New("gamerule", "Changes or queries a world game rule.", nil, gameruleCommand{worldOpCommand: guard}))
	cmd.Register(cmd.New("seed", "Shows the world seed.", nil, seedCommand{worldOpCommand: guard}))
	cmd.Register(cmd.New("summon", "Summons an entity.", nil, summonCommand{worldOpCommand: guard}))
	cmd.Register(cmd.New("setworldspawn", "Sets the world spawn position.", nil, setWorldSpawnCommand{worldOpCommand: guard}))
	cmd.Register(cmd.New("defaultgamemode", "Sets the default game mode.", nil, defaultGameModeCommand{worldOpCommand: guard}))
	cmd.Register(cmd.New("locate", "Finds a nearby structure, biome, or point of interest.", nil, locateCommand{worldOpCommand: guard}))
}

func (s setBlockCommand) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	b, err := commandBlock(tx.World(), s.Block)
	if err != nil {
		o.Error(err.Error())
		return
	}
	tx.SetBlock(cube.Pos{s.X, s.Y, s.Z}, b, nil)
	o.Print("Block placed.")
}
func (f fillCommand) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	b, err := commandBlock(tx.World(), f.Block)
	if err != nil {
		o.Error(err.Error())
		return
	}
	lo, hi := cube.Pos{f.X1, f.Y1, f.Z1}, cube.Pos{f.X2, f.Y2, f.Z2}
	lo, hi = cube.Min(lo, hi), cube.Max(lo, hi)
	count := 0
	for x := lo[0]; x <= hi[0]; x++ {
		for y := lo[1]; y <= hi[1]; y++ {
			for z := lo[2]; z <= hi[2]; z++ {
				tx.SetBlock(cube.Pos{x, y, z}, b, nil)
				count++
			}
		}
	}
	o.Printf("Filled %d blocks.", count)
}
func (c cloneCommand) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	lo, hi := cube.Min(cube.Pos{c.X1, c.Y1, c.Z1}, cube.Pos{c.X2, c.Y2, c.Z2}), cube.Max(cube.Pos{c.X1, c.Y1, c.Z1}, cube.Pos{c.X2, c.Y2, c.Z2})
	type cell struct {
		p cube.Pos
		b world.Block
	}
	cells := make([]cell, 0)
	for x := lo[0]; x <= hi[0]; x++ {
		for y := lo[1]; y <= hi[1]; y++ {
			for z := lo[2]; z <= hi[2]; z++ {
				p := cube.Pos{x, y, z}
				cells = append(cells, cell{p, tx.Block(p)})
			}
		}
	}
	for _, v := range cells {
		dst := cube.Pos{v.p[0] - lo[0] + c.DX, v.p[1] - lo[1] + c.DY, v.p[2] - lo[2] + c.DZ}
		tx.SetBlock(dst, v.b, nil)
	}
	o.Printf("Cloned %d blocks.", len(cells))
}
func (w weatherCommand) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	dur := time.Duration(w.Duration.LoadOr(600)) * time.Second
	switch strings.ToLower(w.Mode) {
	case "clear":
		tx.World().StopRaining()
		tx.World().StopThundering()
	case "rain":
		tx.World().StartRaining(dur)
		tx.World().StopThundering()
	case "thunder":
		tx.World().StartThundering(dur)
	default:
		o.Error("weather must be clear, rain, or thunder")
		return
	}
	o.Printf("Set weather to %s.", w.Mode)
}
func (d difficultyCommand) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	value, ok := d.Difficulty.Load()
	if !ok {
		id, _ := world.DifficultyID(tx.World().Difficulty())
		o.Printf("Difficulty: %d.", id)
		return
	}
	var v world.Difficulty
	switch strings.ToLower(value) {
	case "peaceful", "0":
		v = world.DifficultyPeaceful
	case "easy", "1":
		v = world.DifficultyEasy
	case "normal", "2":
		v = world.DifficultyNormal
	case "hard", "3":
		v = world.DifficultyHard
	default:
		o.Error("difficulty must be peaceful, easy, normal, or hard")
		return
	}
	tx.World().SetDifficulty(v)
	o.Printf("Set difficulty to %s.", value)
}
func (b borderCommand) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	w := tx.World()
	old := w.Border()
	parts := strings.Fields(string(b.Values))
	parse := func(i int) (float64, error) {
		if i >= len(parts) {
			return 0, fmt.Errorf("missing value")
		}
		return strconv.ParseFloat(parts[i], 64)
	}
	transition := func(i int) (int64, bool) {
		if i >= len(parts) {
			return 0, true
		}
		seconds, err := strconv.ParseInt(parts[i], 10, 64)
		if err != nil || seconds < 0 || seconds > math.MaxInt64/20 {
			return 0, false
		}
		return seconds * 20, true
	}
	action := strings.ToLower(b.Action)
	switch action {
	case "get":
		o.Printf("World border size is %.0f blocks wide.", old.Size)
	case "set", "add":
		size, err := parse(0)
		if err != nil || action == "set" && (size < 1 || size > world.MaxBorderSize) || action == "add" && (old.Size+size < 1 || old.Size+size > world.MaxBorderSize) {
			o.Error("invalid world border size")
			return
		}
		ticks, ok := transition(1)
		if !ok {
			o.Error("invalid transition duration")
			return
		}
		if action == "set" {
			w.SetBorderSize(size, ticks)
		} else {
			w.AddBorderSize(size, ticks)
			size += old.Size
		}
		o.Printf("Set world border size to %.0f.", size)
	case "center":
		x, err := parse(0)
		z, errZ := parse(1)
		if err != nil || errZ != nil {
			o.Error("center requires x and z")
			return
		}
		w.SetBorderCenter(x, z)
		o.Print("Set world border center.")
	case "damage":
		if len(parts) < 2 {
			o.Error("damage requires amount or buffer and a value")
			return
		}
		x, err := parse(1)
		if err != nil || x < 0 {
			o.Error("invalid damage setting")
			return
		}
		switch strings.ToLower(parts[0]) {
		case "amount":
			w.SetBorderDamage(x)
		case "buffer":
			w.SetBorderSafeZone(x)
		default:
			o.Error("damage setting must be amount or buffer")
			return
		}
	case "warning":
		if len(parts) < 2 {
			o.Error("warning requires distance or time and a value")
			return
		}
		x, err := strconv.ParseInt(parts[1], 10, 32)
		if err != nil || x < 0 {
			o.Error("invalid warning setting")
			return
		}
		switch strings.ToLower(parts[0]) {
		case "distance":
			w.SetBorderWarningBlocks(int32(x))
		case "time":
			w.SetBorderWarningTime(int32(x))
		default:
			o.Error("warning setting must be distance or time")
			return
		}
	default:
		o.Error("worldborder action must be get, set, add, center, damage, or warning")
	}
}
func (g gameruleCommand) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	w := tx.World()
	name := g.Rule
	v, provided := g.Value.Load()
	if !provided {
		if value, ok := w.GameRule(name); ok {
			o.Printf("%s = %v", name, value)
		} else {
			o.Error("unknown gamerule")
		}
		return
	}
	var value any
	if n, e := strconv.ParseInt(v, 10, 64); e == nil {
		value = n
	} else if b, e := strconv.ParseBool(v); e == nil {
		value = b
	} else {
		value = v
	}
	if err := w.SetGameRule(name, value); err != nil {
		o.Error(err.Error())
		return
	}
	o.Printf("Set %s to %s.", name, v)
}
func (seedCommand) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	o.Printf("Seed: %d", tx.World().Seed())
}
func (s summonCommand) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	id := s.Entity
	if !strings.Contains(id, ":") {
		id = "minecraft:" + id
	}
	spawn := tx.World().EntityRegistry().Config().SpawnMob
	if spawn == nil {
		o.Error("this world has no mob registry")
		return
	}
	h := spawn(id, world.EntitySpawnOpts{Position: mgl64.Vec3{s.X, s.Y, s.Z}, SpawnReason: "command"})
	if h == nil {
		o.Error("unknown or unsupported entity")
		return
	}
	tx.AddEntity(h)
	o.Printf("Summoned %s.", id)
}
func (s setWorldSpawnCommand) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	tx.World().SetSpawn(cube.Pos{s.X, s.Y, s.Z})
	o.Print("Set world spawn.")
}
func (g defaultGameModeCommand) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	mode, label, ok := parseGameMode(g.Mode)
	if !ok {
		o.Error("unknown game mode")
		return
	}
	tx.World().SetDefaultGameMode(mode)
	o.Printf("Set default game mode to %s.", label)
}
func (l locateCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	w := tx.World()
	origin := w.Spawn()
	if p, ok := src.(*player.Player); ok {
		origin = cube.PosFromVec3(p.Position())
	}
	switch strings.ToLower(l.Kind) {
	case "structure":
		location, found, err := w.FindNearestMapFeature(l.Target, origin, 100, false)
		if err != nil {
			o.Error(err.Error())
			return
		}
		if !found {
			o.Error("could not find a nearby structure")
			return
		}
		o.Printf("The nearest %s is at %d %d %d.", location.ID, location.Position[0], location.Position[1], location.Position[2])
	case "biome":
		pos, found, err := w.FindNearestBiome(l.Target, origin)
		if err != nil {
			o.Error(err.Error())
			return
		}
		if !found {
			o.Error("could not find the requested biome")
			return
		}
		o.Printf("The nearest %s is at %d %d %d.", l.Target, pos[0], pos[1], pos[2])
	case "poi":
		pos, poi, found := tx.FindNearestPOIByType(origin, l.Target, 6400)
		if !found {
			o.Error("could not find a nearby point of interest")
			return
		}
		o.Printf("The nearest %s is at %d %d %d.", poi.Type, pos[0], pos[1], pos[2])
	default:
		o.Error("locate kind must be structure, biome, or poi")
	}
}
func commandBlock(w *world.World, spec string) (world.Block, error) {
	name, props, err := parseCommandBlockSpec(spec)
	if err != nil {
		return nil, err
	}
	state, err := world.ResolveState(w.BlockRegistry(), world.BlockState{Name: name, Properties: props})
	if err != nil {
		return nil, err
	}
	b, ok := w.BlockRegistry().BlockByRuntimeID(state.Block)
	if !ok {
		return nil, fmt.Errorf("unknown block %s", name)
	}
	return b, nil
}
func parseCommandBlockSpec(spec string) (string, map[string]any, error) {
	name := spec
	props := map[string]any{}
	if i := strings.IndexByte(spec, '['); i >= 0 {
		if !strings.HasSuffix(spec, "]") {
			return "", nil, fmt.Errorf("invalid block state")
		}
		name = spec[:i]
		body := spec[i+1 : len(spec)-1]
		if body != "" {
			for _, pair := range strings.Split(body, ",") {
				kv := strings.SplitN(pair, "=", 2)
				if len(kv) != 2 {
					return "", nil, fmt.Errorf("invalid block property %q", pair)
				}
				props[kv[0]] = kv[1]
			}
		}
	}
	if !strings.Contains(name, ":") {
		name = "minecraft:" + name
	}
	return name, props, nil
}
