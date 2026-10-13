package builtin

import (
	"fmt"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/world"
)

type tickQueryCommand struct {
	worldOpCommand
	Query cmd.SubCommand `cmd:"query"`
}
type tickRateCommand struct {
	worldOpCommand
	Rate  cmd.SubCommand `cmd:"rate"`
	Value float64        `cmd:"value"`
}
type tickFreezeCommand struct {
	worldOpCommand
	Freeze cmd.SubCommand `cmd:"freeze"`
}
type tickUnfreezeCommand struct {
	worldOpCommand
	Unfreeze cmd.SubCommand `cmd:"unfreeze"`
}
type tickStepCommand struct {
	worldOpCommand
	Step  cmd.SubCommand    `cmd:"step"`
	Ticks cmd.Optional[int] `cmd:"ticks"`
}
type tickStepStopCommand struct {
	worldOpCommand
	Step cmd.SubCommand `cmd:"step"`
	Stop cmd.SubCommand `cmd:"stop"`
}
type tickSprintCommand struct {
	worldOpCommand
	Sprint cmd.SubCommand    `cmd:"sprint"`
	Ticks  cmd.Optional[int] `cmd:"ticks"`
}
type tickSprintStopCommand struct {
	worldOpCommand
	Sprint cmd.SubCommand `cmd:"sprint"`
	Stop   cmd.SubCommand `cmd:"stop"`
}

type forceLoadAddCommand struct {
	worldOpCommand
	Add cmd.SubCommand    `cmd:"add"`
	X   int               `cmd:"x"`
	Z   int               `cmd:"z"`
	X2  cmd.Optional[int] `cmd:"x2"`
	Z2  cmd.Optional[int] `cmd:"z2"`
}
type forceLoadRemoveCommand struct {
	worldOpCommand
	Remove cmd.SubCommand    `cmd:"remove"`
	X      int               `cmd:"x"`
	Z      int               `cmd:"z"`
	X2     cmd.Optional[int] `cmd:"x2"`
	Z2     cmd.Optional[int] `cmd:"z2"`
}
type forceLoadRemoveAllCommand struct {
	worldOpCommand
	Remove cmd.SubCommand `cmd:"remove"`
	All    cmd.SubCommand `cmd:"all"`
}
type forceLoadQueryCommand struct {
	worldOpCommand
	Query cmd.SubCommand    `cmd:"query"`
	X     cmd.Optional[int] `cmd:"x"`
	Z     cmd.Optional[int] `cmd:"z"`
}

type fillBiomeCommand struct {
	worldOpCommand
	X1    int    `cmd:"x1"`
	Y1    int    `cmd:"y1"`
	Z1    int    `cmd:"z1"`
	X2    int    `cmd:"x2"`
	Y2    int    `cmd:"y2"`
	Z2    int    `cmd:"z2"`
	Biome string `cmd:"biome"`
}
type fillBiomeReplaceCommand struct {
	opGate
	fillBiomeCommand
	Replace cmd.SubCommand `cmd:"replace"`
	Filter  string         `cmd:"filter"`
}

func registerWorldTickCommands(srv serverAdapter) {
	guard := worldOpCommand{srv: srv}
	cmd.Register(cmd.New("tick", "Controls world ticking.", nil,
		tickQueryCommand{worldOpCommand: guard}, tickRateCommand{worldOpCommand: guard},
		tickFreezeCommand{worldOpCommand: guard}, tickUnfreezeCommand{worldOpCommand: guard},
		tickStepCommand{worldOpCommand: guard}, tickStepStopCommand{worldOpCommand: guard},
		tickSprintCommand{worldOpCommand: guard}, tickSprintStopCommand{worldOpCommand: guard},
	))
	cmd.Register(cmd.New("forceload", "Keeps chunks loaded and ticking.", nil,
		forceLoadAddCommand{worldOpCommand: guard}, forceLoadRemoveCommand{worldOpCommand: guard},
		forceLoadRemoveAllCommand{worldOpCommand: guard}, forceLoadQueryCommand{worldOpCommand: guard},
	))
	cmd.Register(cmd.New("fillbiome", "Fills an area with a biome.", nil, fillBiomeCommand{worldOpCommand: guard}, fillBiomeReplaceCommand{fillBiomeCommand: fillBiomeCommand{worldOpCommand: guard}}))
}

func (tickQueryCommand) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	w := tx.World()
	state := "running"
	if w.Frozen() {
		state = "frozen"
	}
	o.Printf("Tick rate: %.1f TPS (target: %.1f); %s.", w.TPS(), w.TickRate(), state)
}
func (c tickRateCommand) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	if err := tx.World().SetTickRate(c.Value); err != nil {
		o.Error(err.Error())
		return
	}
	o.Printf("Set tick rate to %.1f TPS.", c.Value)
}
func (tickFreezeCommand) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	tx.World().Freeze()
	o.Print("World frozen.")
}
func (tickUnfreezeCommand) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	tx.World().Unfreeze()
	o.Print("World unfrozen.")
}
func (c tickStepCommand) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	n, ok := c.Ticks.Load()
	if !ok {
		n = 1
	}
	if err := tx.World().StepTicks(n); err != nil {
		o.Error(err.Error())
		return
	}
	o.Printf("Stepping %d tick(s).", n)
}
func (tickStepStopCommand) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	tx.World().StopStepping()
	o.Print("Stopped stepping.")
}
func (c tickSprintCommand) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	n, ok := c.Ticks.Load()
	if !ok {
		n = 100
	}
	if err := tx.World().SprintTicks(n); err != nil {
		o.Error(err.Error())
		return
	}
	o.Printf("Sprint started for %d tick(s).", n)
}
func (tickSprintStopCommand) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	tx.World().StopSprinting()
	o.Print("Stopped sprinting.")
}

func forceLoadRange(x1, z1, x2, z2 int) ([]world.ChunkPos, error) {
	loX, hiX := min(x1, x2), max(x1, x2)
	loZ, hiZ := min(z1, z2), max(z1, z2)
	width, depth := int64(hiX)-int64(loX)+1, int64(hiZ)-int64(loZ)+1
	if width > 256 || depth > 256 || width*depth > 256 {
		return nil, fmt.Errorf("forceload area exceeds 256 chunks")
	}
	positions := make([]world.ChunkPos, 0, int(width*depth))
	for dx := int64(0); dx < width; dx++ {
		for dz := int64(0); dz < depth; dz++ {
			positions = append(positions, world.ChunkPos{int32(int64(loX) + dx), int32(int64(loZ) + dz)})
		}
	}
	return positions, nil
}
func (c forceLoadAddCommand) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	x2, hasX := c.X2.Load()
	if !hasX {
		x2 = c.X
	}
	z2, hasZ := c.Z2.Load()
	if !hasZ {
		z2 = c.Z
	}
	positions, err := forceLoadRange(c.X>>4, c.Z>>4, x2>>4, z2>>4)
	if err != nil {
		o.Error(err.Error())
		return
	}
	for _, p := range positions {
		if err = tx.ForceChunk(p, true); err != nil {
			o.Error(err.Error())
			return
		}
	}
	o.Printf("Added %d chunk(s) to the force-loaded set.", len(positions))
}
func (c forceLoadRemoveCommand) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	x2, hasX := c.X2.Load()
	if !hasX {
		x2 = c.X
	}
	z2, hasZ := c.Z2.Load()
	if !hasZ {
		z2 = c.Z
	}
	positions, err := forceLoadRange(c.X>>4, c.Z>>4, x2>>4, z2>>4)
	if err != nil {
		o.Error(err.Error())
		return
	}
	for _, p := range positions {
		if err = tx.ForceChunk(p, false); err != nil {
			o.Error(err.Error())
			return
		}
	}
	o.Printf("Removed %d chunk(s) from the force-loaded set.", len(positions))
}
func (forceLoadRemoveAllCommand) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	if err := tx.ClearForcedChunks(); err != nil {
		o.Error(err.Error())
		return
	}
	o.Print("Removed all force-loaded chunks.")
}
func (c forceLoadQueryCommand) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	chunks := tx.World().ForcedChunks()
	x, hasX := c.X.Load()
	z, hasZ := c.Z.Load()
	if hasX && hasZ {
		p := world.ChunkPos{int32(x >> 4), int32(z >> 4)}
		status := "not "
		if tx.World().IsChunkForced(p) {
			status = ""
		}
		o.Printf("Chunk %d, %d is %sforce-loaded.", p[0], p[1], status)
		return
	}
	o.Printf("%d chunk(s) are force-loaded.", len(chunks))
}

func (c fillBiomeCommand) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	fillBiome(c, "", false, o, tx)
}
func (c fillBiomeReplaceCommand) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	fillBiome(c.fillBiomeCommand, c.Filter, true, o, tx)
}
func fillBiome(c fillBiomeCommand, filter string, hasFilter bool, o *cmd.Output, tx *world.Tx) {
	biome, ok := commandBiome(c.Biome)
	if !ok {
		o.Errorf("unknown biome %q", c.Biome)
		return
	}
	var replace world.Biome
	if hasFilter {
		var found bool
		replace, found = commandBiome(filter)
		if !found {
			o.Errorf("unknown biome %q", filter)
			return
		}
	}
	lo := cube.Min(cube.Pos{c.X1, c.Y1, c.Z1}, cube.Pos{c.X2, c.Y2, c.Z2})
	hi := cube.Max(cube.Pos{c.X1, c.Y1, c.Z1}, cube.Pos{c.X2, c.Y2, c.Z2})
	count := 0
	touched := map[world.ChunkPos]struct{}{}
	for x := lo[0]; x <= hi[0]; x++ {
		for y := lo[1]; y <= hi[1]; y++ {
			for z := lo[2]; z <= hi[2]; z++ {
				p := cube.Pos{x, y, z}
				if p.OutOfBounds(tx.Range()) || hasFilter && tx.Biome(p).EncodeBiome() != replace.EncodeBiome() {
					continue
				}
				tx.SetBiome(p, biome)
				touched[world.ChunkPosition(p)] = struct{}{}
				count++
			}
		}
	}
	for p := range touched {
		tx.ResendChunk(p)
	}
	o.Printf("Filled %d biome cell(s).", count)
}

func commandBiome(name string) (world.Biome, bool) {
	return world.BiomeByName(strings.TrimPrefix(name, "minecraft:"))
}
