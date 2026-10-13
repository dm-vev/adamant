package block

import (
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
)

// SnifferEgg is an egg laid by sniffers. It cracks twice and hatches a
// snifflet, twice as fast on moss.
type SnifferEgg struct {
	transparent

	// Hatch is how far the egg has cracked, from 0 to 2.
	Hatch int
}

// Model ...
func (SnifferEgg) Model() world.BlockModel { return snifferEggModel{} }

// BreakInfo ...
func (e SnifferEgg) BreakInfo() BreakInfo {
	return newBreakInfo(0.5, alwaysHarvestable, nothingEffective, oneOf(SnifferEgg{}))
}

// Placed ports SnifferEggBlock.onPlace, run for every hatch stage: the next
// stage follows after a third of 24000 ticks, or 12000 ticks on moss, plus up
// to 300 ticks.
func (e SnifferEgg) Placed(pos cube.Pos, tx *world.Tx, _ world.Block) {
	boosted := snifferEggBoosted(pos, tx)
	hatchTime := 24000
	if boosted {
		hatchTime = 12000
		tx.AddParticle(pos.Vec3Centre(), particle.EggCrack{})
	}
	tx.EmitGameEvent(world.GameEventBlockPlace, pos.Vec3Centre(), world.GameEventContext{AffectedState: e})
	tx.ScheduleBlockUpdate(pos, e, time.Duration(hatchTime/3+rand.IntN(300))*time.Second/20)
}

// snifferEggBoosted ports SnifferEggBlock.hatchBoost over the
// #sniffer_egg_hatch_boost tag.
func snifferEggBoosted(pos cube.Pos, tx *world.Tx) bool {
	_, moss := tx.Block(pos.Side(cube.FaceDown)).(MossBlock)
	return moss
}

// ScheduledTick ports SnifferEggBlock.tick: the egg cracks further, or
// hatches a baby sniffer facing a random direction.
func (e SnifferEgg) ScheduledTick(pos cube.Pos, tx *world.Tx, r *rand.Rand) {
	pitch := 0.9 + float64(r.Float32())*0.2
	if e.Hatch < 2 {
		tx.PlaySound(pos.Vec3Centre(), sound.Custom{Name: "block.sniffer_egg.crack", Volume: 0.7, Pitch: pitch})
		e.Hatch++
		tx.SetBlock(pos, e, &world.SetOpts{DisableBlockUpdates: true})
		return
	}
	tx.PlaySound(pos.Vec3Centre(), sound.Custom{Name: "block.sniffer_egg.hatch", Volume: 0.7, Pitch: pitch})
	breakBlockNoDrops(e, pos, tx)
	opts := world.EntitySpawnOpts{Position: pos.Vec3Centre(), Rotation: cube.Rotation{r.Float64()*360 - 180, 0}, SpawnReason: "breeding"}
	h := tx.World().EntityRegistry().Config().SpawnMob("minecraft:sniffer", opts)
	if h == nil {
		return
	}
	tx.AddEntity(h)
	if spawned, ok := h.Entity(tx); ok {
		if ageable, ok := spawned.(interface{ SetAge(int) }); ok {
			ageable.SetAge(-24000)
		}
	}
}

// EncodeItem ...
func (SnifferEgg) EncodeItem() (name string, meta int16) {
	return "minecraft:sniffer_egg", 0
}

// EncodeBlock ...
func (e SnifferEgg) EncodeBlock() (string, map[string]any) {
	return "minecraft:sniffer_egg", map[string]any{"cracked_state": [...]string{"no_cracks", "cracked", "max_cracked"}[e.Hatch]}
}

// allSnifferEggs returns every sniffer egg state.
func allSnifferEggs() []world.Block {
	return []world.Block{SnifferEgg{}, SnifferEgg{Hatch: 1}, SnifferEgg{Hatch: 2}}
}

// snifferEggModel is the 14 by 12 pixel egg.
type snifferEggModel struct{}

// BBox ...
func (snifferEggModel) BBox(cube.Pos, world.BlockSource) []cube.BBox {
	return []cube.BBox{cube.Box(1.0/16, 0, 2.0/16, 15.0/16, 1, 14.0/16)}
}

// FaceSolid ...
func (snifferEggModel) FaceSolid(cube.Pos, cube.Face, world.BlockSource) bool { return false }
