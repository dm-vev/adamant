package block

import (
	"encoding/binary"
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/cube/trace"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// This file holds what vaults and trial spawners share: how BDS names items
// and players in their block actors, and how Java finds the players near
// them.

// actorItemNBT encodes a stack as BDS writes the items of block actors. An
// empty stack is an item without a name.
func actorItemNBT(s item.Stack) map[string]any {
	if s.Empty() {
		return map[string]any{"Name": "", "Count": byte(0), "Damage": int16(0), "WasPickedUp": byte(0)}
	}
	m := item.WriteNBT(s, true)
	m["WasPickedUp"] = byte(0)
	return m
}

// readActorItem decodes an item of a block actor saved by BDS, or by Java with
// its id and count keys.
func readActorItem(v any) item.Stack {
	m, ok := v.(map[string]any)
	if !ok {
		return item.Stack{}
	}
	if _, java := m["id"]; java {
		s, _ := item.StackFromStructureNBT(m)
		return s
	}
	if name, _ := m["Name"].(string); name == "" {
		return item.Stack{}
	}
	return item.ReadNBT(m, nil)
}

// actorItemsNBT encodes stacks as a list of block actor items. BDS writes an
// empty list rather than an empty compound list.
func actorItemsNBT(stacks []item.Stack) []any {
	out := make([]any, len(stacks))
	for i, s := range stacks {
		out[i] = actorItemNBT(s)
	}
	return out
}

// readActorItems decodes a list of block actor items, leaving out empty ones.
func readActorItems(v any) []item.Stack {
	raw, _ := v.([]any)
	out := make([]item.Stack, 0, len(raw))
	for _, value := range raw {
		if s := readActorItem(value); !s.Empty() {
			out = append(out, s)
		}
	}
	return out
}

// uniqueIDsNBT encodes actor unique IDs as BDS writes the players connected to
// a vault: a long array, or an empty list if there are none.
func uniqueIDsNBT(ids []int64) any {
	if len(ids) == 0 {
		return []any{}
	}
	return append([]int64(nil), ids...)
}

// uniqueIDCompoundsNBT encodes actor unique IDs as BDS writes the players and
// mobs of a trial spawner: a list of compounds with a uuid long.
func uniqueIDCompoundsNBT(ids []int64) []any {
	out := make([]any, len(ids))
	for i, id := range ids {
		out[i] = map[string]any{"uuid": id}
	}
	return out
}

// readUniqueIDs decodes actor unique IDs saved as BDS does, in a long array or
// in {uuid} compounds, or as earlier Adamant versions did, as Java UUID int
// arrays or UUID strings.
func readUniqueIDs(v any) []int64 {
	var out []int64
	add := func(id int64) {
		for _, existing := range out {
			if existing == id {
				return
			}
		}
		out = append(out, id)
	}
	switch values := v.(type) {
	case []int64:
		for _, id := range values {
			add(id)
		}
	case []any:
		for _, value := range values {
			switch id := value.(type) {
			case int64:
				add(id)
			case map[string]any:
				if n, ok := id["uuid"].(int64); ok {
					add(n)
				}
			case []int32:
				if len(id) == 4 {
					var u uuid.UUID
					for i, part := range id {
						binary.BigEndian.PutUint32(u[i*4:], uint32(part))
					}
					add(uuidUniqueID(u))
				}
			case string:
				if u, err := uuid.Parse(id); err == nil {
					add(uuidUniqueID(u))
				}
			}
		}
	}
	return out
}

// uuidUniqueID returns the actor unique ID of the entity with the UUID passed,
// as world.EntityHandle.UniqueID derives it.
func uuidUniqueID(u uuid.UUID) int64 {
	return int64(binary.LittleEndian.Uint64(u[8:]))
}

// containsID reports if ids holds id.
func containsID(ids []int64, id int64) bool {
	for _, existing := range ids {
		if existing == id {
			return true
		}
	}
	return false
}

// trialPlayersNear returns the players whose block position is closer than r
// to pos, ported from Java's PlayerDetector. Spectators are never found, and
// creative players only if creative is true. With sight set, a player must
// also see the centre of pos.
func trialPlayersNear(tx *world.Tx, pos cube.Pos, r float64, creative, sight bool) []world.Entity {
	var found []world.Entity
	for p := range tx.Players() {
		if mode, ok := p.(interface{ GameMode() world.GameMode }); ok {
			if m := mode.GameMode(); m == world.GameModeSpectator || (!creative && m == world.GameModeCreative) {
				continue
			}
		}
		if cube.PosFromVec3(p.Position()).Sub(pos).Vec3().LenSqr() >= r*r {
			continue
		}
		if sight && !trialLineOfSight(tx, trialEyePosition(p), pos) {
			continue
		}
		found = append(found, p)
	}
	return found
}

// trialEyePosition returns the position of the eyes of e.
func trialEyePosition(e world.Entity) mgl64.Vec3 {
	if eyes, ok := e.(interface{ EyeHeight() float64 }); ok {
		return e.Position().Add(mgl64.Vec3{0, eyes.EyeHeight()})
	}
	return e.Position()
}

// trialLineOfSight reports if nothing blocks the line from a position to the
// centre of the block at target, as the inLineOfSight checks of Java do:
// the first block the line hits must be target itself.
func trialLineOfSight(tx *world.Tx, from mgl64.Vec3, target cube.Pos) bool {
	to, visible := target.Vec3Centre(), true
	trace.TraverseBlocks(from, to, func(p cube.Pos) bool {
		if _, ok := trace.BlockIntercept(p, tx, tx.Block(p), from, to, trace.CollisionShape); ok {
			visible = p == target
			return false
		}
		return true
	})
	return visible
}

// ejectTrialItem drops a reward above a vault or trial spawner at pos, as
// Java's DefaultDispenseItemBehavior.spawnItem dispenses it upwards with an
// accuracy of 2.
func ejectTrialItem(tx *world.Tx, s item.Stack, pos cube.Pos) {
	triangle := func(mean, spread float64) float64 { return mean + spread*(rand.Float64()-rand.Float64()) }
	create := tx.World().EntityRegistry().Config().Item
	opts := world.EntitySpawnOpts{
		Position: mgl64.Vec3{float64(pos.X()) + 0.5, float64(pos.Y()) + 1.2 - 0.125, float64(pos.Z()) + 0.5},
		Velocity: mgl64.Vec3{triangle(0, 0.0172275*2), triangle(0.2, 0.0172275*2), triangle(0, 0.0172275*2)},
	}
	tx.AddEntity(create(opts, s))
}
