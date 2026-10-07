package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// SculkShrieker detects player vibrations and retains the source warning level.
type SculkShrieker struct {
	solid
	WarningLevel int
	Cooldown int
	CanSummon bool
	Shrieking bool
}

func (s SculkShrieker) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, s)
	if !used { return false }
	place(tx, pos, s, user, ctx)
	return placed(ctx)
}
func (s SculkShrieker) Hash() (uint64, uint64) {
	return hashSculkShrieker, uint64(boolByte(s.CanSummon)) | uint64(boolByte(s.Shrieking))<<1
}
func (SculkShrieker) EncodeItem() (string, int16) { return "minecraft:sculk_shrieker", 0 }
func (s SculkShrieker) EncodeBlock() (string, map[string]any) {
	return "minecraft:sculk_shrieker", map[string]any{"active": boolByte(s.Shrieking), "can_summon": boolByte(s.CanSummon)}
}
func (s SculkShrieker) BreakInfo() BreakInfo { return newBreakInfo(3, alwaysHarvestable, pickaxeEffective, silkTouchOnlyDrop(s)) }
func (s SculkShrieker) EntityStepOn(pos cube.Pos, tx *world.Tx, e world.Entity) {
	if s.Cooldown > 0 || e.H().Type().EncodeEntity() != "minecraft:player" { return }
	if handler := tx.World().EntityRegistry().Config().WardenShrieker; handler != nil {
		s.Shrieking = true
		tx.SetBlock(pos, s, nil)
		handler(tx, pos)
	}
}
func (s SculkShrieker) Tick(_ int64, pos cube.Pos, tx *world.Tx) {
	if s.Cooldown == 0 && !s.Shrieking { return }
	if s.Cooldown > 0 { s.Cooldown-- }
	if s.Cooldown == 0 { s.Shrieking = false }
	tx.SetBlock(pos, s, nil)
}
func (s SculkShrieker) DecodeNBT(m map[string]any) any {
	s.WarningLevel = int(nbtconv.Int32(m, "adamant:warning_level"))
	s.Cooldown = int(nbtconv.Int32(m, "adamant:cooldown"))
	return s
}
func (s SculkShrieker) EncodeNBT() map[string]any {
	return map[string]any{"id": "SculkShrieker", "adamant:warning_level": int32(s.WarningLevel), "adamant:cooldown": int32(s.Cooldown)}
}

var hashSculkShrieker = NextHash()

var _ EntityStepper = SculkShrieker{}
var _ world.TickerBlock = SculkShrieker{}
var _ world.NBTer = SculkShrieker{}
