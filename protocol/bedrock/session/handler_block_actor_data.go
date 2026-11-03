package session

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// BlockActorDataHandler handles an incoming BlockActorData packet from the client, sent for some block entities like
// signs when they are edited.
type BlockActorDataHandler struct{}

// Handle ...
func (b BlockActorDataHandler) Handle(p proto.Packet, s *Session, tx *world.Tx, c Controllable) error {
	pk := p.(*v2193.BlockActorDataPacket)
	if id, ok := pk.NBTData["id"]; ok {
		pos := blockPosFromProtocol(pk.Position)
		if !c.CanReach(pos.Vec3Middle()) {
			return fmt.Errorf("block at %v is not within reach", pos)
		}
		switch id {
		case "Sign":
			return b.handleSign(pk, pos, s, tx, c, false)
		case "HangingSign":
			return b.handleSign(pk, pos, s, tx, c, true)
		case "JigsawBlock":
			return b.handleJigsaw(pk, pos, s, tx, c)
		}
		return fmt.Errorf("unhandled block actor data ID %v", id)
	}
	return fmt.Errorf("block actor data without 'id' tag: %v", pk.NBTData)
}

// handleJigsaw ports ServerGamePacketListenerImpl.handleSetJigsawBlock: the
// jigsaw editor of an operator in creative mode sets the names, pool, final
// state, joint and priorities of a jigsaw.
func (b BlockActorDataHandler) handleJigsaw(pk *v2193.BlockActorDataPacket, pos cube.Pos, s *Session, tx *world.Tx, c Controllable) error {
	if !canUseGameMasterBlocks(c) {
		return fmt.Errorf("only creative operators may edit jigsaw blocks")
	}
	j, ok := tx.Block(pos).(block.Jigsaw)
	if !ok {
		s.conf.Log.Debug("no jigsaw at position of jigsaw block actor data", "pos", pos.String())
		return nil
	}
	tx.SetBlock(pos, j.DecodeNBT(pk.NBTData).(block.Jigsaw), nil)
	return nil
}

// handleSign handles the BlockActorData packet sent when editing a sign and validates the expected sign variant.
func (b BlockActorDataHandler) handleSign(
	pk *v2193.BlockActorDataPacket,
	pos cube.Pos,
	s *Session,
	tx *world.Tx,
	co Controllable,
	hanging bool,
) error {
	bl := tx.Block(pos)
	if hanging {
		if _, ok := bl.(block.HangingSign); !ok {
			s.conf.Log.Debug("no hanging sign at position of sign block actor data", "pos", pos.String())
			return nil
		}
	} else {
		if _, ok := bl.(block.Sign); !ok {
			s.conf.Log.Debug("no sign at position of sign block actor data", "pos", pos.String())
			return nil
		}
	}

	frontText, err := b.textFromNBTData(pk.NBTData, true)
	if err != nil {
		return err
	}
	backText, err := b.textFromNBTData(pk.NBTData, false)
	if err != nil {
		return err
	}
	// The player trims the texts, checks them and the reach again, and
	// writes the side that changed.
	return co.EditSign(pos, hanging, frontText, backText)
}

// textFromNBTData attempts to retrieve the text from the NBT data of specific sign from the BlockActorData packet.
// The player trims and checks it.
func (b BlockActorDataHandler) textFromNBTData(data map[string]any, frontSide bool) (string, error) {
	var sideData map[string]any
	var side string
	if frontSide {
		frontSide, ok := data["FrontText"].(map[string]any)
		if !ok {
			return "", fmt.Errorf("sign block actor data 'FrontText' tag was not found or was not a map: %#v", data["FrontText"])
		}
		sideData = frontSide
		side = "front"
	} else {
		backSide, ok := data["BackText"].(map[string]any)
		if !ok {
			return "", fmt.Errorf("sign block actor data 'BackText' tag was not found or was not a map: %#v", data["BackText"])
		}
		sideData = backSide
		side = "back"
	}
	pkText, ok := sideData["Text"]
	if !ok {
		return "", fmt.Errorf("sign block actor data had no 'Text' tag for side %s", side)
	}
	text, ok := pkText.(string)
	if !ok {
		return "", fmt.Errorf("sign block actor data 'Text' tag was not a string for side %s: %#v", side, pkText)
	}
	return text, nil
}
