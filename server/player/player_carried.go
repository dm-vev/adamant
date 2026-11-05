package player

import (
	"bytes"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// SetEntityOnShoulder ports ServerPlayer.setEntityOnShoulder: a player on
// the ground, out of water and powder snow and riding nothing seats the
// entity saved as data on its left shoulder, or else its right one.
func (p *Player) SetEntityOnShoulder(data map[string]any) bool {
	if p.Riding() != nil || !p.OnGround() || p.touchingWater() || p.data.InPowderSnow {
		return false
	}
	switch {
	case p.shoulderLeft == nil:
		p.shoulderLeft = data
	case p.shoulderRight == nil:
		p.shoulderRight = data
	default:
		return false
	}
	p.shoulderSatTick = p.tx.CurrentTick()
	return true
}

// ShoulderEntities returns the saved native entity compounds currently riding
// the player's shoulders. The returned maps must be treated as read-only.
func (p *Player) ShoulderEntities() (left, right map[string]any) {
	return p.shoulderLeft, p.shoulderRight
}

// ReleaseShoulderParrots reintroduces saved shoulder entities into the world
// above the player, as ServerPlayer.removeEntitiesOnShoulder, at least a
// second after one last sat down. It is also called automatically for source
// dismount conditions.
func (p *Player) ReleaseShoulderParrots() {
	if p.tx == nil || p.shoulderLeft == nil && p.shoulderRight == nil || p.shoulderSatTick+20 >= p.tx.CurrentTick() {
		return
	}
	reg := p.tx.World().EntityRegistry()
	spawn := reg.Config().SpawnMobFromNBT
	if spawn == nil {
		return
	}
	release := func(data map[string]any) map[string]any {
		if data == nil {
			return nil
		}
		id, _ := data["id"].(string)
		if id == "" {
			return data
		}
		opts := world.EntitySpawnOpts{Position: p.Position().Add(mgl64.Vec3{0, .7}), Rotation: p.Rotation()}
		if h := spawn(reg, id, opts, world.CloneNativeEntityNBT(data)); h != nil {
			p.tx.AddEntity(h)
			return nil
		}
		return data
	}
	p.shoulderLeft = release(p.shoulderLeft)
	p.shoulderRight = release(p.shoulderRight)
}

func (p *Player) tickCarriedMaps(current int64) {
	var seen [64]int64
	seenCount := 0
	for _, inv := range [...]*inventory.Inventory{p.inv, p.offHand, p.armour.Inventory()} {
		for slot := range inv.Size() {
			stack, err := inv.Item(slot)
			if err == nil {
				p.tickCarriedMapStack(stack, current, &seen, &seenCount)
			}
		}
	}
}

func (p *Player) tickCarriedMapStack(stack item.Stack, current int64, seen *[64]int64, seenCount *int) {
	mapItem, ok := stack.Item().(item.FilledMap)
	if !ok {
		return
	}
	mapID := mapItem.NativeID
	if !mapItem.HasNativeID {
		if !mapItem.HasJavaID {
			return
		}
		var err error
		mapID, err = p.tx.World().MapIDByJavaID(mapItem.JavaID)
		if err != nil {
			return
		}
	}
	for _, existing := range seen[:*seenCount] {
		if existing == mapID {
			return
		}
	}
	if *seenCount < len(seen) {
		seen[*seenCount] = mapID
		*seenCount++
	}
	data, err := p.tx.UpdateMap(mapID, cube.PosFromVec3(p.Position()))
	if err != nil {
		return
	}
	if data.ColorsChanged || current%5 == 0 {
		if data.Tracking {
			data.TrackedObjects = p.trackedMapObjects(mapID)
		}
		_ = p.client().ViewMapData(data)
	}
}

// updatesMapFrames reports whether the player updates the map frames of
// its world for the players: the living player with the lowest UUID does.
func (p *Player) updatesMapFrames() bool {
	for e := range p.tx.Players() {
		if other, ok := e.(*Player); ok && !other.Dead() {
			otherID, ownID := other.UUID(), p.UUID()
			if bytes.Compare(otherID[:], ownID[:]) < 0 {
				return false
			}
		}
	}
	return true
}

func (p *Player) tickMapFrames(current int64) {
	if current%5 != 0 || !p.updatesMapFrames() {
		return
	}
	p.tx.ForEachMapFrame(func(pos cube.Pos, frame world.MapFrameInfo) {
		mapID, ok := resolveMapID(p.tx, frame.NativeID, frame.HasNativeID, frame.JavaID, frame.HasJavaID)
		if !ok {
			return
		}
		data, err := p.tx.World().MapData(mapID)
		dimensionID, ok := world.DimensionID(p.tx.World().Dimension())
		if err != nil || !ok || !data.Tracking || data.Dimension != int32(dimensionID) {
			return
		}
		data.TrackedObjects = p.trackedMapObjects(mapID)
		viewers := p.tx.Viewers(pos.Vec3Centre())
		for _, viewer := range viewers {
			if mv, ok := viewer.(client.MapViewer); ok {
				_ = mv.ViewMapData(data)
			}
		}
		p.tx.ReleaseViewers(viewers)
	})
}

func (p *Player) trackedMapObjects(mapID int64) []world.MapTrackedObject {
	objects := make([]world.MapTrackedObject, 0, 8)
	for entity := range p.tx.Entities() {
		carrier, ok := entity.(interface {
			Inventory() *inventory.Inventory
			Armour() *inventory.Armour
			HeldItems() (item.Stack, item.Stack)
		})
		if !ok {
			continue
		}
		if invisible, ok := entity.(interface{ Invisible() bool }); ok && invisible.Invisible() {
			continue
		}
		if playerCarriesMap(p.tx, carrier, mapID) {
			objects = append(objects, world.MapTrackedObject{Entity: entity.H()})
		}
	}
	p.tx.ForEachMapFrame(func(pos cube.Pos, frame world.MapFrameInfo) {
		if frameMapID, ok := resolveMapID(p.tx, frame.NativeID, frame.HasNativeID, frame.JavaID, frame.HasJavaID); ok && frameMapID == mapID {
			framePos := pos
			objects = append(objects, world.MapTrackedObject{Block: &framePos})
		}
	})
	return objects
}

func playerCarriesMap(tx *world.Tx, carrier interface {
	Inventory() *inventory.Inventory
	Armour() *inventory.Armour
	HeldItems() (item.Stack, item.Stack)
}, mapID int64) bool {
	main, offHand := carrier.HeldItems()
	if carriedMapMatches(tx, main, mapID) || carriedMapMatches(tx, offHand, mapID) {
		return true
	}
	for _, inv := range [...]*inventory.Inventory{carrier.Inventory(), carrier.Armour().Inventory()} {
		for slot := range inv.Size() {
			stack, err := inv.Item(slot)
			if err == nil && carriedMapMatches(tx, stack, mapID) {
				return true
			}
		}
	}
	return false
}

func carriedMapMatches(tx *world.Tx, stack item.Stack, mapID int64) bool {
	mapItem, ok := stack.Item().(item.FilledMap)
	if !ok {
		return false
	}
	id, ok := resolveMapID(tx, mapItem.NativeID, mapItem.HasNativeID, mapItem.JavaID, mapItem.HasJavaID)
	return ok && id == mapID
}

func resolveMapID(tx *world.Tx, nativeID int64, hasNativeID bool, javaID int32, hasJavaID bool) (int64, bool) {
	if hasNativeID {
		return nativeID, true
	}
	if !hasJavaID {
		return 0, false
	}
	id, err := tx.World().MapIDByJavaID(javaID)
	return id, err == nil
}
