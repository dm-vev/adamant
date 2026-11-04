package menu

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/item"
)

// Weave adds pattern to the banner of the loom times times.
//
// The loom of the menu takes a banner that is not an illager banner and a
// dye, each at least times times, and the pattern item if the pattern needs
// one; it keeps the pattern item. Neither input may be locked.
func (t *Tx) Weave(pattern block.BannerPatternType, times int) error {
	// First check if there actually is a loom opened: a menu, and a loom where it was opened.
	m := t.o.Menu
	if m == nil {
		return fmt.Errorf("no loom container opened")
	}
	if _, ok := t.o.Tx.Block(m.Pos()).(block.Loom); !ok {
		return fmt.Errorf("no loom container opened")
	}
	timesCrafted := times
	if timesCrafted < 1 {
		return fmt.Errorf("times crafted must be least 1")
	}

	// Next, check if the input slot has a valid banner item.
	bannerSlot := Slot{Inv: t.o.Inv.UI, Index: UILoomBanner}
	input, _ := t.Item(bannerSlot)
	if err := ensureUnlockedForCrafting(input); err != nil {
		return err
	}
	if input.Count() < timesCrafted {
		return fmt.Errorf("input item count is less than times crafted")
	}
	b, ok := input.Item().(block.Banner)
	if !ok {
		return fmt.Errorf("input item is not a banner")
	}
	if b.Illager {
		return fmt.Errorf("input item is an illager banner")
	}

	// Do the same with the input dye.
	dyeSlot := Slot{Inv: t.o.Inv.UI, Index: UILoomDye}
	dye, _ := t.Item(dyeSlot)
	if err := ensureUnlockedForCrafting(dye); err != nil {
		return err
	}
	if dye.Count() < timesCrafted {
		return fmt.Errorf("dye item count is less than times crafted")
	}
	d, ok := dye.Item().(item.Dye)
	if !ok {
		return fmt.Errorf("dye item is not a dye")
	}

	// The caller names the pattern the player wanted to apply.
	expectedPattern := pattern

	// Some banner patterns have equivalent banner pattern items that are required to craft the pattern. If the expected
	// pattern has a pattern item, check if the player input the correct pattern item.
	patternItem, _ := t.Item(Slot{Inv: t.o.Inv.UI, Index: UILoomPattern})
	if !patternItem.Empty() {
		if err := ensureUnlockedForCrafting(patternItem); err != nil {
			return err
		}
	}
	if expectedPatternItem, hasPatternItem := expectedPattern.Item(); hasPatternItem {
		if patternItem.Empty() {
			return fmt.Errorf("pattern item is empty but the pattern is required")
		}
		p, ok := patternItem.Item().(item.BannerPattern)
		if !ok {
			return fmt.Errorf("pattern item is not a banner pattern")
		}
		if expectedPatternItem != p.Type {
			return fmt.Errorf("pattern item does not match the expected pattern")
		}
	}

	// Add a new pattern layer onto the banner, and create the result.
	b.Patterns = append(b.Patterns, block.BannerPatternLayer{
		Type:   expectedPattern,
		Colour: d.Colour,
	})
	_ = t.set(t.named(bannerSlot, RoleLoomBanner), input.Grow(-timesCrafted))
	_ = t.set(t.named(dyeSlot, RoleLoomDye), dye.Grow(-timesCrafted))
	// Only timesCrafted banners are consumed above, so only that many may be produced: input.WithItem keeps the count
	// of the whole input stack.
	return t.createResults(input.Grow(timesCrafted - input.Count()).WithItem(b))
}
