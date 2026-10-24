package entity

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
)

type PiglinBruteType struct{}
type PiglinBrute struct{ *Piglin }

func (PiglinBruteType) EncodeEntity() string { return "minecraft:piglin_brute" }
func (PiglinBruteType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	if d.Data == nil {
		(piglinConfig{brut: true, main: bruteAxe()}).Apply(d)
	}
	return &PiglinBrute{&Piglin{&Ent{tx: tx, handle: h, data: d}}}
}
func (PiglinBruteType) BBox(world.Entity) cube.BBox { return cube.Box(-.3, 0, -.3, .3, 1.95, .3) }
func (PiglinBruteType) DecodeNBT(n map[string]any, d *world.EntityData) {
	j := ReadNativeMobJavaState(n)
	if j == nil {
		j = n
	}
	eq := ReadNativeMobEquipment(n)
	c := piglinConfig{brut: true, immune: nbtconv.Bool(j, "IsImmuneToZombification"), conversion: int(nbtconv.Int32(j, "TimeInOverworld")), main: piglinStackFromNative(eq.Mainhand), off: piglinStackFromNative(eq.Offhand)}
	c.angryAt, _ = uuid.Parse(nbtconv.String(j, "AngerTarget"))
	for i := range c.armor {
		if i < len(eq.Armor) {
			c.armor[i] = hostileStackFromNative(eq.Armor[i])
		}
	}
	if x, ok := nativeMobAttribute(n, "minecraft:health"); ok {
		c.health, c.maxHealth = x.Current, x.Base
	} else if _, ok := j["Health"]; ok {
		c.health = float64(nbtconv.Float32(j, "Health"))
	}
	brain, _ := j["Brain"].(map[string]any)
	memories, _ := brain["memories"].(map[string]any)
	home, _ := memories["minecraft:home"].(map[string]any)
	value, _ := home["value"].(map[string]any)
	dim, _ := value["dimension"].(string)
	if pos, ok := value["pos"].([]int32); ok && len(pos) == 3 {
		c.home = &globalPos{dim: dimensionByName(dim), pos: cube.Pos{int(pos[0]), int(pos[1]), int(pos[2])}}
	} else if pos, ok := value["pos"].([3]int32); ok {
		c.home = &globalPos{dim: dimensionByName(dim), pos: cube.Pos{int(pos[0]), int(pos[1]), int(pos[2])}}
	}
	c.Apply(d)
}
func (PiglinBruteType) EncodeNBT(d *world.EntityData) map[string]any { return encodePiglinNBT(d) }
func NewPiglinBrute(o world.EntitySpawnOpts) *world.EntityHandle {
	return o.New(PiglinBruteType{}, piglinConfig{brut: true, main: bruteAxe()})
}
func bruteAxe() item.Stack                           { return item.NewStack(item.Axe{Tier: item.ToolTierGold}, 1) }
func (b *PiglinBrute) SpeciesType() world.EntityType { return PiglinBruteType{} }

// FinalizeSpawn ports PiglinBrute.finalizeSpawn: a brute takes up a golden
// axe and guards where it spawned, as PiglinBruteAi.initMemories.
func (b *PiglinBrute) FinalizeSpawn(_ string, r *vanilla.Random) {
	if r == nil {
		return
	}
	s := b.p()
	s.br.set(memHome, globalPos{dim: b.tx.World().Dimension(), pos: cube.PosFromVec3(b.Position())})
	s.main = bruteAxe()
}

var _ world.EntityType = PiglinBruteType{}
