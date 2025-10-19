package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player/chat"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/google/uuid"
)

// RespawnAnchor is a block that allows the player to set their spawn point in the Nether.
type RespawnAnchor struct {
	solid
	bassDrum

	// Charge is the Glowstone charge of the RespawnAnchor.
	Charge int
}

// LightEmissionLevel ...
func (r RespawnAnchor) LightEmissionLevel() uint8 {
	return uint8(max(0, 3+4*(r.Charge-1)))
}

// EncodeItem ...
func (r RespawnAnchor) EncodeItem() (name string, meta int16) {
	return "minecraft:respawn_anchor", 0
}

// EncodeBlock ...
func (r RespawnAnchor) EncodeBlock() (string, map[string]any) {
	return "minecraft:respawn_anchor", map[string]any{"respawn_anchor_charge": int32(r.Charge)}
}

// BreakInfo ...
func (r RespawnAnchor) BreakInfo() BreakInfo {
	return newBreakInfo(50, func(t item.Tool) bool {
		return t.ToolType() == item.TypePickaxe && t.HarvestLevel() >= item.ToolTierDiamond.HarvestLevel
	}, pickaxeEffective, oneOf(r)).withBlastResistance(6000)
}

// Activate ...
func (r RespawnAnchor) Activate(pos cube.Pos, clickedFace cube.Face, tx *world.Tx, u item.User, ctx *item.UseContext) bool {
	held, _ := u.HeldItems()
	_, usingGlowstone := held.Item().(Glowstone)

	sleeper, ok := u.(respawnSleeper)
	if !ok {
		return false
	}
	w := tx.World()
	if usingGlowstone {
		if w.Dimension() != world.Nether {
			ctx.SubtractFromCount(1)
			tx.SetBlock(pos, nil, nil)
			ExplosionConfig{
				Size:      5,
				SpawnFire: true,
			}.Explode(tx, pos.Vec3Centre())
			return true
		}
		if r.Charge < 4 {
			r.Charge++
			tx.SetBlock(pos, r, nil)
			ctx.SubtractFromCount(1)
			tx.PlaySound(pos.Vec3Centre(), sound.RespawnAnchorCharge{Charge: r.Charge})
			return true
		}
	}

	if r.Charge > 0 {
		if w.Dimension() == world.Nether {
			if _, ok := r.SafeSpawn(pos, tx); !ok {
				sleeper.Messaget(chat.MessageRespawnAnchorNotValid)
				return true
			}

			previousSpawn := w.PlayerSpawn(sleeper.UUID())
			if previousSpawn == pos {
				return false
			}
			sleeper.Messaget(chat.MessageRespawnPointSet)
			w.SetPlayerSpawn(sleeper.UUID(), pos)
			return true
		}
		tx.SetBlock(pos, nil, nil)
		ExplosionConfig{
			Size:      5,
			SpawnFire: true,
		}.Explode(tx, pos.Vec3Centre())
	}

	return false
}

// allRespawnAnchors returns all possible respawn anchors.
func allRespawnAnchors() []world.Block {
	all := make([]world.Block, 0, 5)
	for i := 0; i < 5; i++ {
		all = append(all, RespawnAnchor{Charge: i})
	}
	return all
}

// CanRespawnOn ...
func (r RespawnAnchor) CanRespawnOn() bool {
	return r.Charge > 0
}

// SafeSpawn ...
func (r RespawnAnchor) SafeSpawn(p cube.Pos, tx *world.Tx) (cube.Pos, bool) {
	for _, offset := range respawnAnchorOffsets {
		candidate := p.Add(offset)
		if respawnAnchorSafe(candidate, tx) {
			return candidate, true
		}
	}

	return cube.Pos{}, false
}

var respawnAnchorOffsets = []cube.Pos{
	{0, 1, 0},
	{-1, 1, 0}, {1, 1, 0}, {0, 1, -1}, {0, 1, 1},
	{-1, 1, -1}, {1, 1, -1}, {-1, 1, 1}, {1, 1, 1},
	{-1, 0, 0}, {1, 0, 0}, {0, 0, -1}, {0, 0, 1},
	{-1, 0, -1}, {1, 0, -1}, {-1, 0, 1}, {1, 0, 1},
}

func respawnAnchorSafe(pos cube.Pos, tx *world.Tx) bool {
	if pos.OutOfBounds(tx.Range()) {
		return false
	}

	head := pos.Add(cube.Pos{0, 1, 0})
	if head.OutOfBounds(tx.Range()) {
		return false
	}

	if len(tx.Block(pos).Model().BBox(pos, tx)) != 0 {
		return false
	}
	if len(tx.Block(head).Model().BBox(head, tx)) != 0 {
		return false
	}

	if _, ok := tx.Liquid(pos); ok {
		return false
	}
	if _, ok := tx.Liquid(head); ok {
		return false
	}

	below := pos.Side(cube.FaceDown)
	if below.OutOfBounds(tx.Range()) {
		return false
	}

	return tx.Block(below).Model().FaceSolid(below, cube.FaceUp, tx)
}

// RespawnOn ...
func (r RespawnAnchor) RespawnOn(pos cube.Pos, u item.User, w *world.Tx) {
	w.SetBlock(pos, RespawnAnchor{Charge: r.Charge - 1}, nil)
	w.PlaySound(pos.Vec3(), sound.RespawnAnchorDeplete{Charge: r.Charge - 1})
}

// respawnSleeper represents a user able to interact with respawn anchors.
type respawnSleeper interface {
	item.User
	Messaget(chat.Translation, ...any)
	UUID() uuid.UUID
}
