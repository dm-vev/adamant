package session

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// structureLimits are the largest sizes, and offsets in either direction, the
// Bedrock structure editor accepts along the X, Y and Z axes.
var structureLimits = [3]int{64, 384, 64}

// canUseGameMasterBlocks ports Player.canUseGameMasterBlocks: only operators
// in creative mode may edit structure, jigsaw and command blocks.
func canUseGameMasterBlocks(c Controllable) bool {
	return c.GameMode().CreativeInventory() && cmd.IsOperator(c.Name())
}

// StructureBlockUpdateHandler applies the settings of the structure block
// editor, and saves or loads the structure when the client asks to.
type StructureBlockUpdateHandler struct{}

// Handle ...
func (StructureBlockUpdateHandler) Handle(p packet.Packet, s *Session, tx *world.Tx, c Controllable) error {
	pk := p.(*packet.StructureBlockUpdate)
	if !canUseGameMasterBlocks(c) {
		return fmt.Errorf("only creative operators may edit structure blocks")
	}
	pos := blockPosFromProtocol(pk.Position)
	if !canReach(c, pos.Vec3Middle()) {
		return fmt.Errorf("structure block at %v is not within reach", pos)
	}
	if pk.StructureBlockType < 0 || pk.StructureBlockType > int32(block.StructureBlockExport) {
		return fmt.Errorf("invalid structure block mode %d", pk.StructureBlockType)
	}
	b, ok := tx.Block(pos).(block.StructureBlock)
	if !ok {
		s.conf.Log.Debug("no structure block at position of structure block update", "pos", pos.String())
		return nil
	}
	settings := pk.Settings
	b.Mode = block.StructureBlockMode(pk.StructureBlockType)
	b.Name, b.DataField = pk.StructureName, pk.DataField
	b.IncludePlayers, b.ShowBoundingBox = pk.IncludePlayers, pk.ShowBoundingBox
	b.IgnoreEntities, b.RemoveBlocks = settings.IgnoreEntities, settings.IgnoreBlocks
	for axis, limit := range structureLimits {
		b.Size[axis] = min(max(int(settings.Size[axis]), 0), limit)
		b.Offset[axis] = min(max(int(settings.Offset[axis]), -limit), limit)
	}
	b.Rotation, b.Mirror = settings.Rotation%4, settings.Mirror%4
	b.Integrity = min(max(settings.Integrity, 0), 100)
	b.Seed = int64(settings.Seed)
	b.AnimationMode, b.AnimationSeconds = settings.AnimationMode%3, max(settings.AnimationDuration, 0)
	b.RedstoneSaveMode = int32(pk.RedstoneSaveMode % 2)
	b.LastTouchedPlayerID = settings.LastEditingPlayerUniqueID
	tx.SetBlock(pos, b, nil)
	if !pk.ShouldTrigger {
		return nil
	}
	switch b.Mode {
	case block.StructureBlockSave:
		// The editor's save button always writes to the world save.
		b.Save(pos, tx, true)
	case block.StructureBlockLoad:
		b.Load(pos, tx)
	}
	return nil
}

// StructureTemplateDataRequestHandler answers the structure editor asking for
// a saved structure, to show its size, or for the blocks to export to a file.
type StructureTemplateDataRequestHandler struct{}

// Handle ...
func (StructureTemplateDataRequestHandler) Handle(p packet.Packet, s *Session, tx *world.Tx, c Controllable) error {
	pk := p.(*packet.StructureTemplateDataRequest)
	if !canUseGameMasterBlocks(c) {
		return fmt.Errorf("only creative operators may request structures")
	}
	var template *world.StructureTemplate
	responseType := byte(packet.StructureTemplateResponseExport)
	switch pk.RequestType {
	case packet.StructureTemplateRequestExportFromSave:
		pos := blockPosFromProtocol(pk.Position)
		if !canReach(c, pos.Vec3Middle()) {
			return fmt.Errorf("structure block at %v is not within reach", pos)
		}
		var size [3]int
		var offset cube.Pos
		for axis, limit := range structureLimits {
			size[axis] = min(max(int(pk.Settings.Size[axis]), 0), limit)
			offset[axis] = min(max(int(pk.Settings.Offset[axis]), -limit), limit)
		}
		template = tx.CaptureStructure(pos.Add(offset), size, !pk.Settings.IgnoreEntities)
	case packet.StructureTemplateRequestExportFromLoad, packet.StructureTemplateRequestQuerySavedStructure:
		if pk.RequestType == packet.StructureTemplateRequestQuerySavedStructure {
			responseType = packet.StructureTemplateResponseQuery
		}
		t, ok, err := tx.World().StructureTemplate(pk.StructureName)
		if err != nil {
			s.conf.Log.Error("structure template request: "+err.Error(), "name", pk.StructureName)
		}
		if ok {
			template = t
		}
	default:
		return fmt.Errorf("unknown structure template request type %d", pk.RequestType)
	}
	response := &v2193.StructureTemplateDataResponsePacket{StructureName: pk.StructureName, ResponseType: responseType}
	if template != nil {
		response.Success, response.StructureTemplate = true, template.EncodeNBT()
	}
	s.writePacket(response)
	return nil
}
