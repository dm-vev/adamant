package entity

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

type PiglinBruteType struct{}
type PiglinBrute struct{ *Piglin }

func (PiglinBruteType) EncodeEntity() string { return "minecraft:piglin_brute" }
func (PiglinBruteType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	if d.Data == nil {
		(piglinConfig{health: 50, maxHealth: 50, main: bruteAxe()}).Apply(d)
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
	c := piglinConfig{health: 50, maxHealth: 50, target: nbtconv.String(j, "AngerTarget"), attackCooldown: int(nbtconv.Int32(j, "AttackCooldown")), main: piglinStackFromNative(eq.Mainhand)}
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
	c.homeDimension, _ = value["dimension"].(string)
	if pos, ok := value["pos"].([]int32); ok && len(pos) == 3 {
		c.home = cube.Pos{int(pos[0]), int(pos[1]), int(pos[2])}
		c.homeSet = true
	} else if pos, ok := value["pos"].([3]int32); ok {
		c.home = cube.Pos{int(pos[0]), int(pos[1]), int(pos[2])}
		c.homeSet = true
	}
	c.Apply(d)
}
func (PiglinBruteType) EncodeNBT(d *world.EntityData) map[string]any { return encodePiglinNBT(d) }
func NewPiglinBrute(o world.EntitySpawnOpts) *world.EntityHandle {
	return o.New(PiglinBruteType{}, piglinConfig{health: 50, maxHealth: 50, main: bruteAxe()})
}
func bruteAxe() item.Stack { return item.NewStack(item.Axe{Tier: item.ToolTierGold}, 1) }
func (b *PiglinBrute) SpeciesType() world.EntityType { return PiglinBruteType{} }
func (b *PiglinBrute) FinalizeSpawn(_ string, _ *java26.Random) {}

var _ world.EntityType = PiglinBruteType{}
