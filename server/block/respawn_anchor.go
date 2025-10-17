package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

type RespawnAnchor struct {
	solid
	bass

	Charge int
}

func (RespawnAnchor) EncodeItem() (name string, meta int16) {
	return "minecraft:respawn_anchor", 0
}

func (a RespawnAnchor) EncodeBlock() (string, map[string]any) {
	return "minecraft:respawn_anchor", map[string]any{"respawn_anchor_charge": int32(a.Charge)}
}

func (a RespawnAnchor) LightEmissionLevel() uint8 {
	switch a.Charge {
	case 0:
		return 0
	case 1:
		return 3
	case 2:
		return 7
	case 3:
		return 11
	default:
		return 15
	}
}

func (RespawnAnchor) BreakInfo() BreakInfo {
	return newBreakInfo(35, func(t item.Tool) bool {
		return t.ToolType() == item.TypePickaxe && t.HarvestLevel() >= item.ToolTierDiamond.HarvestLevel
	}, pickaxeEffective, oneOf(RespawnAnchor{})).withBlastResistance(6000)
}

func (a RespawnAnchor) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, a)
	if !used {
		return false
	}
	place(tx, pos, a, user, ctx)
	return placed(ctx)
}

func (a RespawnAnchor) Activate(pos cube.Pos, _ cube.Face, tx *world.Tx, u item.User, ctx *item.UseContext) bool {
	held, _ := u.HeldItems()
	glowstone := !held.Empty()
	if glowstone {
		_, glowstone = held.Item().(Glowstone)
	}

	if tx.World().Dimension() != world.Nether {
		if glowstone {
			ctx.SubtractFromCount(1)
		}
		a.explode(pos, tx)
		return true
	}

	if glowstone {
		if a.Charge >= 4 {
			if msg, ok := u.(messager); ok {
				msg.Message("Respawn anchor is already fully charged")
			}
			return true
		}

		a.Charge++
		tx.SetBlock(pos, a, nil)
		ctx.SubtractFromCount(1)
		tx.PlaySound(pos.Vec3Centre(), sound.BlockPlace{Block: Glowstone{}})
		return true
	}

	if a.Charge == 0 {
		if msg, ok := u.(messager); ok {
			msg.Message("Respawn anchor is not charged")
		}
		return true
	}

	user, ok := u.(respawnAnchorUser)
	if !ok {
		return false
	}

	spawnPos, ok := a.SpawnPosition(pos, tx)
	if !ok {
		if msg, ok := u.(messager); ok {
			msg.Message("Respawn anchor is obstructed")
		}
		return true
	}

	user.SetRespawnPosition(spawnPos, tx.World().Dimension())
	tx.World().SetPlayerSpawn(user.UUID(), pos)
	if msg, ok := u.(messager); ok {
		msg.Message("Respawn point set")
	}
	return true
}

func (a RespawnAnchor) SpawnPosition(pos cube.Pos, tx *world.Tx) (cube.Pos, bool) {
	for _, offset := range respawnAnchorOffsets {
		candidate := pos.Add(offset)
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

func (RespawnAnchor) explode(pos cube.Pos, tx *world.Tx) {
	tx.SetBlock(pos, nil, nil)
	ExplosionConfig{Size: 5, SpawnFire: true}.Explode(tx, pos.Vec3Centre())
}

type respawnAnchorUser interface {
	UUID() uuid.UUID
	SetRespawnPosition(pos cube.Pos, dim world.Dimension)
}

type messager interface {
	Message(a ...any)
}

func allRespawnAnchors() (blocks []world.Block) {
	for charge := 0; charge <= 4; charge++ {
		blocks = append(blocks, RespawnAnchor{Charge: charge})
	}
	return
}
