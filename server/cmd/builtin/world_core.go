package builtin

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/item"
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
	Position mgl64.Vec3                 `cmd:"pos"`
	Block    string                     `cmd:"block"`
	Mode     cmd.Optional[setBlockMode] `cmd:"mode"`
}
type fillCommand struct {
	worldOpCommand
	From  mgl64.Vec3             `cmd:"from"`
	To    mgl64.Vec3             `cmd:"to"`
	Block string                 `cmd:"block"`
	Mode  cmd.Optional[fillMode] `cmd:"mode"`
}
type fillReplaceCommand struct {
	worldOpCommand
	From    mgl64.Vec3           `cmd:"from"`
	To      mgl64.Vec3           `cmd:"to"`
	Block   string               `cmd:"block"`
	Replace cmd.SubCommand       `cmd:"replace"`
	Filter  cmd.Optional[string] `cmd:"filter"`
}
type cloneCommand struct {
	worldOpCommand
	Begin       mgl64.Vec3                   `cmd:"begin"`
	End         mgl64.Vec3                   `cmd:"end"`
	Destination mgl64.Vec3                   `cmd:"destination"`
	Mask        cmd.Optional[cloneMaskMode]  `cmd:"maskMode"`
	Clone       cmd.Optional[cloneCloneMode] `cmd:"cloneMode"`
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
	Entity   string                   `cmd:"entity"`
	Position cmd.Optional[mgl64.Vec3] `cmd:"pos"`
}
type setWorldSpawnCommand struct {
	worldOpCommand
	Position cmd.Optional[mgl64.Vec3] `cmd:"pos"`
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
	cmd.Register(cmd.New("fill", "Fills a region with a block.", nil, fillCommand{worldOpCommand: guard}, fillReplaceCommand{worldOpCommand: guard}))
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

// commandModificationBlockLimit is Java's default command_modification_block_limit.
const commandModificationBlockLimit = 32768

type setBlockMode string

func (setBlockMode) Type() string                { return "SetBlockMode" }
func (setBlockMode) Options(cmd.Source) []string { return []string{"destroy", "keep", "replace"} }

type fillMode string

func (fillMode) Type() string { return "FillMode" }
func (fillMode) Options(cmd.Source) []string {
	return []string{"destroy", "hollow", "keep", "outline", "replace"}
}

type cloneMaskMode string

func (cloneMaskMode) Type() string                { return "MaskMode" }
func (cloneMaskMode) Options(cmd.Source) []string { return []string{"replace", "masked"} }

type cloneCloneMode string

func (cloneCloneMode) Type() string                { return "CloneMode" }
func (cloneCloneMode) Options(cmd.Source) []string { return []string{"normal", "force", "move"} }

// commandBox returns the inclusive block box and rejects volumes over Java's limit.
func commandBox(a, b mgl64.Vec3, o *cmd.Output) (lo, hi cube.Pos, volume int, ok bool) {
	lo, hi = cube.Min(cube.PosFromVec3(a), cube.PosFromVec3(b)), cube.Max(cube.PosFromVec3(a), cube.PosFromVec3(b))
	volume = (hi[0] - lo[0] + 1) * (hi[1] - lo[1] + 1) * (hi[2] - lo[2] + 1)
	if volume > commandModificationBlockLimit {
		o.Errorf("Too many blocks in the specified area (maximum %d, specified %d)", commandModificationBlockLimit, volume)
		return lo, hi, volume, false
	}
	return lo, hi, volume, true
}

func isAir(b world.Block) bool {
	name, _ := b.EncodeBlock()
	return name == "minecraft:air"
}

// destroyCommandBlock breaks a block with drops, as Level.destroyBlock(pos, true).
func destroyCommandBlock(tx *world.Tx, pos cube.Pos) {
	if breakable, ok := tx.Block(pos).(block.Breakable); ok && !isAir(tx.Block(pos)) {
		for _, drop := range breakable.BreakInfo().Drops(item.ToolNone{}, nil) {
			tx.AddEntity(tx.World().EntityRegistry().Config().Item(world.EntitySpawnOpts{Position: pos.Vec3Centre()}, drop))
		}
	}
	tx.SetBlock(pos, nil, nil)
}

func (s setBlockCommand) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	b, err := commandBlock(tx.World(), s.Block)
	if err != nil {
		o.Error(err.Error())
		return
	}
	pos := cube.PosFromVec3(s.Position)
	if pos.OutOfBounds(tx.Range()) {
		o.Error("Cannot place blocks outside of the world")
		return
	}
	switch s.Mode.LoadOr("replace") {
	case "keep":
		if !isAir(tx.Block(pos)) {
			o.Error("Could not set the block")
			return
		}
	case "destroy":
		destroyCommandBlock(tx, pos)
	}
	if tx.Block(pos) == b {
		o.Error("Could not set the block")
		return
	}
	tx.SetBlock(pos, b, nil)
	o.Printf("Changed the block at %d, %d, %d", pos[0], pos[1], pos[2])
}

func (f fillCommand) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	fillBlocks(tx, o, f.From, f.To, f.Block, string(f.Mode.LoadOr("replace")), "")
}

func (f fillReplaceCommand) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	fillBlocks(tx, o, f.From, f.To, f.Block, "replace", f.Filter.LoadOr(""))
}

// fillBlocks ports FillCommand's modes: outline/hollow touch only the shell
// (hollow clears the interior), keep only fills air and destroy drops items.
func fillBlocks(tx *world.Tx, o *cmd.Output, from, to mgl64.Vec3, spec, mode, filter string) {
	b, err := commandBlock(tx.World(), spec)
	if err != nil {
		o.Error(err.Error())
		return
	}
	var match world.Block
	if filter != "" {
		if match, err = commandBlock(tx.World(), filter); err != nil {
			o.Error(err.Error())
			return
		}
	}
	lo, hi, _, ok := commandBox(from, to, o)
	if !ok {
		return
	}
	air, _ := world.BlockByName("minecraft:air", nil)
	count := 0
	for x := lo[0]; x <= hi[0]; x++ {
		for y := lo[1]; y <= hi[1]; y++ {
			for z := lo[2]; z <= hi[2]; z++ {
				pos := cube.Pos{x, y, z}
				if pos.OutOfBounds(tx.Range()) {
					continue
				}
				shell := x == lo[0] || x == hi[0] || y == lo[1] || y == hi[1] || z == lo[2] || z == hi[2]
				place := b
				switch mode {
				case "outline":
					if !shell {
						continue
					}
				case "hollow":
					if !shell {
						place = air
					}
				case "keep":
					if !isAir(tx.Block(pos)) {
						continue
					}
				case "destroy":
					destroyCommandBlock(tx, pos)
				}
				if match != nil && tx.Block(pos) != match {
					continue
				}
				if tx.Block(pos) == place {
					continue
				}
				tx.SetBlock(pos, place, nil)
				count++
			}
		}
	}
	if count == 0 {
		o.Error("No blocks were filled")
		return
	}
	o.Printf("Successfully filled %d block(s)", count)
}

// Run ports CloneCommands: masked skips air, move clears the source and normal
// mode rejects overlapping source and destination regions.
func (c cloneCommand) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	lo, hi, _, ok := commandBox(c.Begin, c.End, o)
	if !ok {
		return
	}
	dest := cube.PosFromVec3(c.Destination)
	size := hi.Sub(lo)
	destHi := dest.Add(size)
	mask, mode := c.Mask.LoadOr("replace"), c.Clone.LoadOr("normal")
	overlap := dest[0] <= hi[0] && destHi[0] >= lo[0] && dest[1] <= hi[1] && destHi[1] >= lo[1] && dest[2] <= hi[2] && destHi[2] >= lo[2]
	if mode == "normal" && overlap {
		o.Error("The source and destination can not overlap")
		return
	}
	type cell struct {
		p cube.Pos
		b world.Block
	}
	cells := make([]cell, 0, (size[0]+1)*(size[1]+1)*(size[2]+1))
	for x := lo[0]; x <= hi[0]; x++ {
		for y := lo[1]; y <= hi[1]; y++ {
			for z := lo[2]; z <= hi[2]; z++ {
				p := cube.Pos{x, y, z}
				b := tx.Block(p)
				if mask == "masked" && isAir(b) {
					continue
				}
				cells = append(cells, cell{p, b})
			}
		}
	}
	if mode == "move" {
		for _, v := range cells {
			tx.SetBlock(v.p, nil, nil)
		}
	}
	for _, v := range cells {
		tx.SetBlock(dest.Add(v.p.Sub(lo)), v.b, nil)
	}
	if len(cells) == 0 {
		o.Error("No blocks were cloned")
		return
	}
	o.Printf("Successfully cloned %d block(s)", len(cells))
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
func (s summonCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	id := s.Entity
	if !strings.Contains(id, ":") {
		id = "minecraft:" + id
	}
	spawn := tx.World().EntityRegistry().Config().SpawnMob
	if spawn == nil {
		o.Error("this world has no mob registry")
		return
	}
	pos, ok := s.Position.Load()
	if !ok {
		pos = src.Position()
	}
	h := spawn(id, world.EntitySpawnOpts{Position: pos, SpawnReason: "command"})
	if h == nil {
		o.Error("unknown or unsupported entity")
		return
	}
	tx.AddEntity(h)
	o.Printf("Summoned %s.", id)
}
func (s setWorldSpawnCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	pos, ok := s.Position.Load()
	if !ok {
		pos = src.Position()
	}
	spawn := cube.PosFromVec3(pos)
	tx.World().SetSpawn(spawn)
	o.Printf("Set the world spawn point to %d, %d, %d", spawn[0], spawn[1], spawn[2])
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
