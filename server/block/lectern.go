package block

import (
	"fmt"
	"math"
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// Lectern is a librarian's job site block found in villages. It is used to hold books for multiple players to read in
// multiplayer.
type Lectern struct {
	bass
	sourceWaterDisplacer

	// Facing represents the direction the Lectern is facing.
	Facing cube.Direction
	// Book is the book currently held by the Lectern.
	Book item.Stack
	// Page is the page the Lectern is currently on in the book.
	Page int
	// Powered indicates if the lectern is currently emitting a redstone pulse.
	Powered bool
}

// Model ...
func (Lectern) Model() world.BlockModel {
	return model.Lectern{}
}

// FuelInfo ...
func (Lectern) FuelInfo() item.FuelInfo {
	return newFuelInfo(time.Second * 15)
}

// SideClosed ...
func (Lectern) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool {
	return false
}

// BreakInfo ...
func (l Lectern) BreakInfo() BreakInfo {
	d := []item.Stack{item.NewStack(Lectern{}, 1)}
	if !l.Book.Empty() {
		d = append(d, l.Book)
	}
	return newBreakInfo(2.5, alwaysHarvestable, axeEffective, simpleDrops(d...))
}

// UseOnBlock ...
func (l Lectern) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) (used bool) {
	pos, _, used = firstReplaceable(tx, pos, face, l)
	if !used {
		return false
	}
	l.Facing = user.Rotation().Direction().Opposite()
	place(tx, pos, l, user, ctx)
	return placed(ctx)
}

// readableBook represents a book that can be read through a lectern.
type readableBook interface {
	// TotalPages returns the total number of pages in the book.
	TotalPages() int
	// Page returns a specific page from the book and true when the page exists. It will otherwise return an empty string
	// and false.
	Page(page int) (string, bool)
}

// Activate ...
func (l Lectern) Activate(pos cube.Pos, _ cube.Face, tx *world.Tx, u item.User, ctx *item.UseContext) bool {
	if !l.Book.Empty() {
		if opener, ok := u.(ContainerOpener); ok {
			opener.OpenBlockContainer(pos, tx)
			return true
		}
		return false
	}

	held, _ := u.HeldItems()
	if _, ok := held.Item().(readableBook); !ok {
		// We can't put a non-book item on the lectern.
		return false
	}

	l.Book, l.Page = held, 0
	l.Powered = false
	tx.SetBlock(pos, l, nil)
	notifyComparatorUpdate(pos, tx)

	tx.PlaySound(pos.Vec3Centre(), sound.LecternBookPlace{})
	ctx.SubtractFromCount(1)
	return true
}

// Punch ...
func (l Lectern) Punch(pos cube.Pos, _ cube.Face, tx *world.Tx, _ item.User) {
	if l.Book.Empty() {
		// We can't remove a book from the lectern if there isn't one.
		return
	}

	dropItem(tx, l.Book, pos.Side(cube.FaceUp).Vec3Middle())

	l.Book = item.Stack{}
	l.Powered = false
	tx.SetBlock(pos, l, nil)
	notifyComparatorUpdate(pos, tx)
	tx.PlaySound(pos.Vec3Centre(), sound.Attack{})
}

// TurnPage updates the page the lectern is currently on to the page given.
func (l Lectern) TurnPage(pos cube.Pos, tx *world.Tx, page int) error {
	if page == l.Page {
		// We're already on the correct page, so we don't need to do anything.
		return nil
	}
	if l.Book.Empty() {
		return fmt.Errorf("lectern at %v is empty", pos)
	}
	if r, ok := l.Book.Item().(readableBook); ok && (page >= r.TotalPages() || page < 0) {
		return fmt.Errorf("page number %d is out of bounds", page)
	}
	l.Page = page
	l.Powered = true
	tx.SetBlock(pos, l, nil)
	notifyComparatorUpdate(pos, tx)
	tx.ScheduleBlockUpdate(pos, l, redstoneTicks(1))
	return nil
}

// ScheduledTick ...
func (l Lectern) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	if !l.Powered {
		return
	}
	l.Powered = false
	tx.SetBlock(pos, l, nil)
	notifyComparatorUpdate(pos, tx)
}

// EncodeNBT ...
func (l Lectern) EncodeNBT() map[string]any {
	m := map[string]any{
		"hasBook": boolByte(!l.Book.Empty()),
		"page":    int32(l.Page),
		"id":      "Lectern",
	}
	if r, ok := l.Book.Item().(readableBook); ok {
		m["book"] = nbtconv.WriteItem(l.Book, true)
		m["totalPages"] = int32(r.TotalPages())
	}
	return m
}

// DecodeNBT ...
func (l Lectern) DecodeNBT(m map[string]any) any {
	l.Page = int(nbtconv.Int32(m, "page"))
	l.Book = nbtconv.MapItem(m, "book")
	return l
}

// EncodeItem ...
func (Lectern) EncodeItem() (name string, meta int16) {
	return "minecraft:lectern", 0
}

// EncodeBlock ...
func (l Lectern) EncodeBlock() (string, map[string]any) {
	return "minecraft:lectern", map[string]any{
		"minecraft:cardinal_direction": l.Facing.String(),
		"powered_bit":                  boolByte(l.Powered),
	}
}

func (l Lectern) RedstonePower(cube.Pos, *world.Tx, cube.Face) int {
	if l.Powered {
		return 15
	}
	return 0
}

func (l Lectern) RedstoneStrongPower(pos cube.Pos, tx *world.Tx, face cube.Face) int {
	if face == cube.FaceDown {
		return l.RedstonePower(pos, tx, face)
	}
	return 0
}

// ComparatorOutput returns the redstone signal output for a comparator.
func (l Lectern) ComparatorOutput(*world.Tx, cube.Pos) uint8 {
	if l.Book.Empty() {
		return 0
	}
	totalPages := 1
	if r, ok := l.Book.Item().(readableBook); ok {
		totalPages = r.TotalPages()
	}
	if totalPages <= 1 {
		return 1
	}
	page := l.Page
	if page < 0 {
		page = 0
	} else if page >= totalPages {
		page = totalPages - 1
	}
	signal := int(math.Floor(float64(page) / float64(totalPages-1) * 14))
	if signal < 0 {
		signal = 0
	} else if signal > 14 {
		signal = 14
	}
	return uint8(signal + 1)
}

// allLecterns ...
func allLecterns() (lecterns []world.Block) {
	for _, f := range cube.Directions() {
		lecterns = append(lecterns, Lectern{Facing: f})
		lecterns = append(lecterns, Lectern{Facing: f, Powered: true})
	}
	return
}
