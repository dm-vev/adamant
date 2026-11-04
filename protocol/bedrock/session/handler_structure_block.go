package session

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
)

// StructureBlockUpdateHandler applies the settings of the structure block
// editor, and saves or loads the structure when the client asks to.
type StructureBlockUpdateHandler struct{}

// Handle ...
func (StructureBlockUpdateHandler) Handle(p proto.Packet, s *Session, tx *world.Tx, c Controllable) error {
	pk := p.(*mc.StructureBlockUpdatePacket)
	if !c.CanUseGameMasterBlocks() {
		return fmt.Errorf("only creative operators may edit structure blocks")
	}
	pos := blockPosFromProtocol(pk.Position)
	if !c.CanReach(pos.Vec3Middle()) {
		return fmt.Errorf("structure block at %v is not within reach", pos)
	}
	if pk.StructureBlockType < 0 || pk.StructureBlockType > int32(block.StructureBlockExport) {
		return fmt.Errorf("invalid structure block mode %d", pk.StructureBlockType)
	}
	if _, ok := tx.Block(pos).(block.StructureBlock); !ok {
		s.conf.Log.Debug("no structure block at position of structure block update", "pos", pos.String())
		return nil
	}
	settings := pk.Settings
	edit := block.StructureBlockEdit{
		Mode:                block.StructureBlockMode(pk.StructureBlockType),
		Name:                pk.StructureName,
		DataField:           pk.DataField,
		IncludePlayers:      pk.IncludePlayers,
		ShowBoundingBox:     pk.ShowBoundingBox,
		IgnoreEntities:      settings.IgnoreEntities,
		RemoveBlocks:        settings.IgnoreBlocks,
		Rotation:            settings.Rotation,
		Mirror:              settings.Mirror,
		Integrity:           settings.Integrity,
		Seed:                int64(settings.Seed),
		AnimationMode:       settings.AnimationMode,
		AnimationSeconds:    settings.AnimationDuration,
		RedstoneSaveMode:    pk.RedstoneSaveMode,
		LastTouchedPlayerID: settings.LastEditingPlayerUniqueID,
	}
	for axis := range edit.Size {
		edit.Size[axis], edit.Offset[axis] = int(settings.Size[axis]), int(settings.Offset[axis])
	}
	// The player checks again, bounds the edit, writes the block and saves
	// or loads its structure.
	return c.EditStructureBlock(pos, edit, pk.ShouldTrigger)
}

// StructureTemplateDataRequestHandler answers the structure editor asking for
// a saved structure, to show its size, or for the blocks to export to a file.
type StructureTemplateDataRequestHandler struct{}

// Handle ...
func (StructureTemplateDataRequestHandler) Handle(p proto.Packet, s *Session, tx *world.Tx, c Controllable) error {
	pk := p.(*mc.StructureTemplateDataRequestPacket)
	if !c.CanUseGameMasterBlocks() {
		return fmt.Errorf("only creative operators may request structures")
	}
	var template *world.StructureTemplate
	responseType := byte(mc.StructureTemplateResponseExport)
	switch pk.RequestType {
	case mc.StructureTemplateRequestExportFromSave:
		pos := blockPosFromProtocol(pk.Position)
		if !c.CanReach(pos.Vec3Middle()) {
			return fmt.Errorf("structure block at %v is not within reach", pos)
		}
		var size [3]int
		var offset cube.Pos
		for axis, limit := range block.StructureLimits {
			size[axis] = min(max(int(pk.Settings.Size[axis]), 0), limit)
			offset[axis] = min(max(int(pk.Settings.Offset[axis]), -limit), limit)
		}
		template = tx.CaptureStructure(pos.Add(offset), size, !pk.Settings.IgnoreEntities)
	case mc.StructureTemplateRequestExportFromLoad, mc.StructureTemplateRequestQuerySavedStructure:
		if pk.RequestType == mc.StructureTemplateRequestQuerySavedStructure {
			responseType = mc.StructureTemplateResponseQuery
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
	response := &mc.StructureTemplateDataResponsePacket{StructureName: pk.StructureName, ResponseType: responseType}
	if template != nil {
		response.Success, response.StructureTemplate = true, template.EncodeNBT()
	}
	s.writePacket(response)
	return nil
}
