package player

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// CanReach reports whether the player can reach pos: it is alive, in a game
// mode that allows interaction, and its eyes are within 8 blocks of pos, or
// within 14 in a game mode with the creative inventory.
func (p *Player) CanReach(pos mgl64.Vec3) bool {
	return p.canReach(pos)
}

// CanUseGameMasterBlocks reports whether the player may use the blocks of
// game masters, such as command, structure and jigsaw blocks: it is an
// operator in a game mode with the creative inventory, as
// Player.canUseGameMasterBlocks of Java.
func (p *Player) CanUseGameMasterBlocks() bool {
	return p.GameMode().CreativeInventory() && cmd.IsOperator(p.UUID())
}

// EditJigsaw sets the names, pool, final state, joint and priorities of the
// jigsaw at pos to those of edited, as the jigsaw editor of the player sets
// them, keeping where the jigsaw faces. It fails if the jigsaw is out of the
// reach of the player or the player may not use game master blocks, and does
// nothing if no jigsaw is at pos.
func (p *Player) EditJigsaw(pos cube.Pos, edited block.Jigsaw) error {
	if !p.CanReach(pos.Vec3Middle()) {
		return fmt.Errorf("block at %v is not within reach", pos)
	}
	if !p.CanUseGameMasterBlocks() {
		return fmt.Errorf("only creative operators may edit jigsaw blocks")
	}
	j, ok := p.tx.Block(pos).(block.Jigsaw)
	if !ok {
		p.log.Debug("edit jigsaw: no jigsaw at position", "pos", pos.String())
		return nil
	}
	edited.Facing, edited.Rotation = j.Facing, j.Rotation
	p.tx.SetBlock(pos, edited, nil)
	return nil
}

// MaxSignTextLength bounds the text of a side of a sign, in bytes.
const MaxSignTextLength = 256

// EditSign writes frontText and backText on the sign at pos, as the player
// edits it: a hanging sign if hanging is set, a sign standing or on a wall
// otherwise. Each text is trimmed of its trailing newlines and must then be
// UTF-8 of at most MaxSignTextLength bytes. EditSign fails if the sign is out
// of the reach of the player or a text is not valid, and does nothing if no
// such sign is at pos.
//
// The side whose text changed is written, the front if both did, unless the
// sign is waxed or HandleSignEdit cancels the edit, in which case the player
// is shown the sign as it is.
func (p *Player) EditSign(pos cube.Pos, hanging bool, frontText, backText string) error {
	if !p.CanReach(pos.Vec3Middle()) {
		return fmt.Errorf("block at %v is not within reach", pos)
	}
	frontText, err := signText(frontText, "front")
	if err != nil {
		return err
	}
	backText, err = signText(backText, "back")
	if err != nil {
		return err
	}
	switch sign := p.tx.Block(pos).(type) {
	case block.Sign:
		if hanging {
			break
		}
		return p.editSign(pos, frontText, backText, sign.Front, sign.Back, sign.Waxed, func(front, back block.SignText) world.Block {
			sign.Front = front
			sign.Back = back
			return sign
		})
	case block.HangingSign:
		if !hanging {
			break
		}
		return p.editSign(pos, frontText, backText, sign.Front, sign.Back, sign.Waxed, func(front, back block.SignText) world.Block {
			sign.Front = front
			sign.Back = back
			return sign
		})
	}
	p.log.Debug("edit sign: no such sign at position", "pos", pos.String(), "hanging", hanging)
	return nil
}

// signText trims the text of one side of a sign of its trailing newlines and
// checks it.
func signText(text, side string) (string, error) {
	text = strings.TrimRight(text, "\n")
	if len(text) > MaxSignTextLength {
		return "", fmt.Errorf("sign text was longer than %d bytes for side %s", MaxSignTextLength, side)
	}
	if !utf8.ValidString(text) {
		return "", fmt.Errorf("sign text was not valid UTF8 for side %s", side)
	}
	return text, nil
}

// editSign applies validated sign text changes to either a regular sign or a hanging sign.
func (p *Player) editSign(
	pos cube.Pos,
	frontText, backText string,
	front, back block.SignText,
	waxed bool,
	update func(front, back block.SignText) world.Block,
) error {
	if waxed {
		return nil
	} else if frontText == front.Text && backText == back.Text {
		return nil
	}

	ctx := NewEventContext(p.tx, p)
	if frontText != front.Text {
		if p.Handler().HandleSignEdit(ctx, pos, true, front.Text, frontText); ctx.Cancelled() {
			p.resendNearbyBlock(pos)
			return nil
		}
		front.Text = frontText
		front.Owner = p.XUID()
	} else {
		if p.Handler().HandleSignEdit(ctx, pos, false, back.Text, backText); ctx.Cancelled() {
			p.resendNearbyBlock(pos)
			return nil
		}
		back.Text = backText
		back.Owner = p.XUID()
	}
	p.tx.SetBlock(pos, update(front, back), nil)
	return nil
}

// TurnLecternPage turns the book on the lectern at pos to page, as the player
// reads it. It fails if the lectern is out of the reach of the player or no
// lectern is at pos. HandleLecternPageTurn may cancel the turn or change the
// page turned to.
func (p *Player) TurnLecternPage(pos cube.Pos, page int) error {
	if !p.CanReach(pos.Vec3Middle()) {
		return fmt.Errorf("block at %v is not within reach", pos)
	}
	lectern, ok := p.tx.Block(pos).(block.Lectern)
	if !ok {
		return fmt.Errorf("edit lectern: no lectern at position %v", pos)
	}

	ctx := NewEventContext(p.tx, p)
	if p.Handler().HandleLecternPageTurn(ctx, pos, lectern.Page, &page); ctx.Cancelled() {
		return nil
	}

	return lectern.TurnPage(pos, p.tx, page)
}
