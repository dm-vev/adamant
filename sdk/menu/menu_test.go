package menu_test

import (
	"testing"

	"github.com/df-mc/dragonfly/sdk/menu"
	"github.com/df-mc/dragonfly/server/item"
)

func TestMenuVirtualKindsReadOnlySetSlotAndPaged(t *testing.T) {
	var changed int
	m := menu.DoubleChest("Shop",
		menu.ReadOnly(),
		menu.Slot(0, item.NewStack(item.Diamond{}, 1), menu.Locked()),
		menu.OnChange(func(_ int, _, _ item.Stack) { changed++ }),
	)
	if m.Title() != "Shop" || m.Kind() != menu.DoubleChestKind || m.Inventory().Size() != 54 {
		t.Fatalf("unexpected menu metadata: title=%q kind=%v size=%d", m.Title(), m.Kind(), m.Inventory().Size())
	}

	// Programmatic SetSlot / SetItem / ClearSlot must bypass ReadOnly validator.
	m.SetSlot(1, menu.SlotConfig{Item: item.NewStack(item.Emerald{}, 4), Locked: true})
	st, err := m.Inventory().Item(1)
	if err != nil || st.Count() != 4 {
		t.Fatalf("SetSlot on ReadOnly failed: st=%+v err=%v", st, err)
	}
	m.SetItem(1, item.NewStack(item.GoldIngot{}, 2))
	st, _ = m.Inventory().Item(1)
	if st.Count() != 2 {
		t.Fatalf("SetItem on ReadOnly failed: st=%+v", st)
	}
	m.ClearSlot(1)
	st, _ = m.Inventory().Item(1)
	if !st.Empty() {
		t.Fatalf("ClearSlot did not clear slot: %+v", st)
	}
	if changed == 0 {
		t.Fatal("OnChange never fired")
	}

	// Direct inventory writes from external callers must be rejected by ReadOnly validator.
	if m.Inventory().Accepts(2, item.NewStack(item.Stick{}, 1)) {
		t.Fatal("expected ReadOnly validator to reject placement in slot 2")
	}
	_ = m.Inventory().SetItem(2, item.NewStack(item.Stick{}, 1))
	if st2, _ := m.Inventory().Item(2); !st2.Empty() {
		t.Fatalf("expected slot 2 to remain empty under ReadOnly, got %+v", st2)
	}

	hop := menu.Hopper("Filter")
	if hop.Kind() != menu.HopperKind || hop.Inventory().Size() != 5 {
		t.Fatalf("Hopper size=%d kind=%v", hop.Inventory().Size(), hop.Kind())
	}

	items := make([]menu.PagedItem, 25)
	for i := range items {
		items[i] = menu.PagedItem{Item: item.NewStack(item.Apple{}, 1)}
	}
	pm := menu.Paged("Catalog", items)
	if pm.PageSize() != 18 || pm.PageCount() != 2 {
		t.Fatalf("Paged size=%d count=%d", pm.PageSize(), pm.PageCount())
	}
	p0 := pm.BuildPage(0)
	if p0.Title() != "Catalog (1/2)" {
		t.Fatalf("page 0 title = %q", p0.Title())
	}
	p1 := pm.BuildPage(1)
	if p1.Title() != "Catalog (2/2)" {
		t.Fatalf("page 1 title = %q", p1.Title())
	}
	// Page 1 has Previous button at slot 18, indicator at 22, no Next at 26.
	if prev, _ := p1.Inventory().Item(18); prev.Empty() {
		t.Fatal("expected Previous Page button on page 1")
	}
	if next, _ := p1.Inventory().Item(26); !next.Empty() {
		t.Fatal("unexpected Next Page button on last page")
	}
}
