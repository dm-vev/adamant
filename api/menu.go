package api

import (
	"slices"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/form"
	"github.com/df-mc/dragonfly/server/world"
)

// MenuSlot configures a single slot in a CustomMenu.
type MenuSlot struct {
	Item     item.Stack
	Locked   bool
	OnClick  func(p *player.Player, slot int, it item.Stack) (allowMove bool)
	OnChange func(slot int, before, after item.Stack)
}

// MenuSpec declaratively configures an inventory-backed CustomMenu.
type MenuSpec struct {
	// Size sets the inventory slot count (defaults to 27 when <= 0).
	Size int
	// ReadOnly locks all slots unless overridden on a per-slot handler.
	ReadOnly bool
	// Slots maps slot index to its initial item, lock state, and callbacks.
	Slots map[int]MenuSlot
	// OnClick runs when a player attempts to take or drop an item from a slot.
	// Return true to permit moving the item, or false to cancel the move.
	OnClick func(p *player.Player, slot int, it item.Stack) (allowMove bool)
	// OnChange runs whenever a slot's contents change.
	OnChange func(slot int, before, after item.Stack)
}

// CustomMenu wraps an *inventory.Inventory with declarative slot protection and
// click/change callbacks for block or entity container UIs.
type CustomMenu struct {
	spec MenuSpec
	inv  *inventory.Inventory
}

// NewMenu constructs a CustomMenu from spec.
func NewMenu(spec MenuSpec) *CustomMenu {
	size := spec.Size
	if size <= 0 {
		size = 27
	}
	m := &CustomMenu{spec: cloneMenuSpec(spec, size)}
	m.inv = inventory.New(size, nil)
	m.Bind(m.inv)
	return m
}

func cloneMenuSpec(spec MenuSpec, size int) MenuSpec {
	cloned := make(map[int]MenuSlot, len(spec.Slots))
	for k, v := range spec.Slots {
		if k >= 0 && k < size {
			cloned[k] = v
		}
	}
	spec.Size = size
	spec.Slots = cloned
	return spec
}

// Inventory returns the menu's underlying *inventory.Inventory.
func (m *CustomMenu) Inventory() *inventory.Inventory {
	if m == nil {
		return nil
	}
	return m.inv
}

// Bind configures inv with this CustomMenu's initial slot items, slot
// validator, slot change callback, and inventory.Handler.
func (m *CustomMenu) Bind(inv *inventory.Inventory) {
	if m == nil || inv == nil {
		return
	}
	inv.Handle(inventory.NopHandler{})
	inv.SlotValidatorFunc(func(item.Stack, int) bool { return true })
	inv.SlotFunc(func(int, item.Stack, item.Stack) {})
	for slot, cfg := range m.spec.Slots {
		if slot >= 0 && slot < inv.Size() && !cfg.Item.Empty() {
			_ = inv.SetItem(slot, cfg.Item)
		}
	}
	inv.SlotValidatorFunc(func(it item.Stack, slot int) bool {
		if it.Empty() {
			return true
		}
		if m.spec.ReadOnly {
			return false
		}
		if cfg, ok := m.spec.Slots[slot]; ok && cfg.Locked {
			return false
		}
		return true
	})
	inv.SlotFunc(func(slot int, before, after item.Stack) {
		if cfg, ok := m.spec.Slots[slot]; ok && cfg.OnChange != nil {
			cfg.OnChange(slot, before, after)
		}
		if m.spec.OnChange != nil {
			m.spec.OnChange(slot, before, after)
		}
	})
	inv.Handle(&customMenuHandler{menu: m})
}

// OpenForEntity opens the menu's inventory for p as an entity container hosted
// by host (defaulting to p if host is nil).
func (m *CustomMenu) OpenForEntity(p *player.Player, host world.Entity, tx *world.Tx) {
	if m == nil || p == nil || tx == nil || tx.Closed() {
		return
	}
	if host == nil {
		host = p
	}
	p.OpenEntityContainer(host, m.inv, entity.ContainerChestBoat, tx)
}

// OpenBlock opens the block container at pos for p in tx.
func OpenBlockMenu(p *player.Player, pos cube.Pos, tx *world.Tx) {
	if p == nil || tx == nil || tx.Closed() {
		return
	}
	p.OpenBlockContainer(pos, tx)
}

type customMenuHandler struct {
	menu *CustomMenu
}

func (h *customMenuHandler) handleClick(ctx *inventory.Context, slot int, it item.Stack) {
	p, _ := ctx.Val().(*player.Player)
	allow := !h.menu.spec.ReadOnly
	if cfg, ok := h.menu.spec.Slots[slot]; ok {
		if cfg.Locked {
			allow = false
		}
		if cfg.OnClick != nil {
			allow = cfg.OnClick(p, slot, it) && !cfg.Locked && !h.menu.spec.ReadOnly
		}
	}
	if h.menu.spec.OnClick != nil {
		if !h.menu.spec.OnClick(p, slot, it) {
			allow = false
		}
	}
	if !allow {
		ctx.Cancel()
	}
}

func (h *customMenuHandler) HandleTake(ctx *inventory.Context, slot int, it item.Stack) {
	h.handleClick(ctx, slot, it)
}

func (h *customMenuHandler) HandlePlace(ctx *inventory.Context, slot int, it item.Stack) {
	if h.menu.spec.ReadOnly {
		ctx.Cancel()
		return
	}
	if cfg, ok := h.menu.spec.Slots[slot]; ok && cfg.Locked {
		ctx.Cancel()
	}
}

func (h *customMenuHandler) HandleDrop(ctx *inventory.Context, slot int, it item.Stack) {
	h.handleClick(ctx, slot, it)
}

// FormButton defines a clickable button inside FormSpec.
type FormButton struct {
	Text    string
	Image   string
	OnClick func(p *player.Player, tx *world.Tx)
}

// FormSpec declaratively configures a Bedrock menu form.
type FormSpec struct {
	Title   string
	Body    string
	Buttons []FormButton
	OnClose func(p *player.Player, tx *world.Tx)
}

type dynamicFormMenu struct {
	buttons []form.Button
	actions []func(p *player.Player, tx *world.Tx)
	onClose func(p *player.Player, tx *world.Tx)
}

func (d dynamicFormMenu) Submit(s form.Submitter, pressed form.Button, tx *world.Tx) {
	p, _ := s.(*player.Player)
	for i, b := range d.buttons {
		if b == pressed {
			if d.actions[i] != nil {
				d.actions[i](p, tx)
			}
			return
		}
	}
}

func (d dynamicFormMenu) Close(s form.Submitter, tx *world.Tx) {
	if d.onClose != nil {
		p, _ := s.(*player.Player)
		d.onClose(p, tx)
	}
}

// NewForm creates a form.Menu from spec with closure-backed button and close handlers.
func NewForm(spec FormSpec) form.Menu {
	btns := make([]form.Button, len(spec.Buttons))
	actions := make([]func(*player.Player, *world.Tx), len(spec.Buttons))
	for i, b := range spec.Buttons {
		btns[i] = form.NewButton(b.Text, b.Image)
		actions[i] = b.OnClick
	}
	sub := dynamicFormMenu{
		buttons: slices.Clone(btns),
		actions: actions,
		onClose: spec.OnClose,
	}
	return form.NewMenu(sub, spec.Title).WithBody(spec.Body).WithButtons(btns...)
}

// SendForm displays a declarative FormSpec menu form to p.
func SendForm(p *player.Player, spec FormSpec) {
	if p == nil {
		return
	}
	p.SendForm(NewForm(spec))
}

// ModalSpec declaratively configures a two-button confirmation modal form.
type ModalSpec struct {
	Title       string
	Body        string
	ConfirmText string
	CancelText  string
	OnConfirm   func(p *player.Player, tx *world.Tx)
	OnCancel    func(p *player.Player, tx *world.Tx)
	OnClose     func(p *player.Player, tx *world.Tx)
}

type dynamicModal struct {
	Confirm   form.Button
	Cancel    form.Button
	onConfirm func(p *player.Player, tx *world.Tx)
	onCancel  func(p *player.Player, tx *world.Tx)
	onClose   func(p *player.Player, tx *world.Tx)
}

func (d dynamicModal) Submit(s form.Submitter, pressed form.Button, tx *world.Tx) {
	p, _ := s.(*player.Player)
	if pressed == d.Confirm {
		if d.onConfirm != nil {
			d.onConfirm(p, tx)
		}
		return
	}
	if d.onCancel != nil {
		d.onCancel(p, tx)
	}
}

func (d dynamicModal) Close(s form.Submitter, tx *world.Tx) {
	if d.onClose != nil {
		p, _ := s.(*player.Player)
		d.onClose(p, tx)
	}
}

// NewModal creates a form.Modal from spec.
func NewModal(spec ModalSpec) form.Modal {
	confirm := form.YesButton()
	if spec.ConfirmText != "" {
		confirm = form.NewButton(spec.ConfirmText, "")
	}
	cancel := form.NoButton()
	if spec.CancelText != "" {
		cancel = form.NewButton(spec.CancelText, "")
	}
	m := dynamicModal{
		Confirm:   confirm,
		Cancel:    cancel,
		onConfirm: spec.OnConfirm,
		onCancel:  spec.OnCancel,
		onClose:   spec.OnClose,
	}
	return form.NewModal(m, spec.Title).WithBody(spec.Body)
}

// SendModal displays a declarative ModalSpec confirmation dialog to p.
func SendModal(p *player.Player, spec ModalSpec) {
	if p == nil {
		return
	}
	p.SendForm(NewModal(spec))
}
