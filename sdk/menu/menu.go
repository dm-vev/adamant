// Package menu provides declarative, inventory-backed virtual container GUIs
// (single chest, double chest, hopper, entity container, and paginated chests)
// with per-slot lock protection and click/change/close callbacks.
package menu

import (
	"fmt"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/menu"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
)

// Kind selects how Menu.Open renders a container screen to players.
type Kind uint8

const (
	// ChestKind renders a virtual 27-slot single chest with a custom title.
	ChestKind Kind = iota
	// DoubleChestKind renders a virtual 54-slot double chest with a custom title.
	DoubleChestKind
	// HopperKind renders a virtual 5-slot hopper with a custom title.
	HopperKind
	// EntityKind renders the inventory as an entity container (ChestBoat layout).
	EntityKind
)

// ClickFunc runs when a player attempts to take or drop an item from a slot.
// Return true to permit moving the item, or false to cancel the move.
type ClickFunc func(p *player.Player, slot int, it item.Stack) (allowMove bool)

// ChangeFunc runs whenever a slot's contents change.
type ChangeFunc func(slot int, before, after item.Stack)

// CloseFunc runs when a player closes a virtual menu opened via Open.
type CloseFunc func(p *player.Player, tx *world.Tx)

// SlotConfig configures a single slot in a Menu.
type SlotConfig struct {
	Item     item.Stack
	Locked   bool
	OnClick  ClickFunc
	OnChange ChangeFunc
}

// SlotOption configures a slot passed to Slot.
type SlotOption func(*SlotConfig)

// Locked prevents players from moving or placing items in this slot.
func Locked() SlotOption { return func(s *SlotConfig) { s.Locked = true } }

// SlotClick attaches a click callback to a single slot.
func SlotClick(fn ClickFunc) SlotOption { return func(s *SlotConfig) { s.OnClick = fn } }

// SlotChange attaches a change callback to a single slot.
func SlotChange(fn ChangeFunc) SlotOption { return func(s *SlotConfig) { s.OnChange = fn } }

type menuConfig struct {
	title    string
	kind     Kind
	size     int
	readOnly bool
	slots    map[int]SlotConfig
	onClick  ClickFunc
	onChange ChangeFunc
	onClose  CloseFunc
}

// Option configures a Menu constructed by Chest, DoubleChest, Hopper, or Entity.
type Option func(*menuConfig)

// ReadOnly locks every slot in the menu against player item movement.
func ReadOnly() Option { return func(c *menuConfig) { c.readOnly = true } }

// Slot configures an initial item, lock state, and callbacks for index.
func Slot(index int, st item.Stack, opts ...SlotOption) Option {
	return func(c *menuConfig) {
		if c.slots == nil {
			c.slots = make(map[int]SlotConfig)
		}
		sc := SlotConfig{Item: st}
		for _, opt := range opts {
			if opt != nil {
				opt(&sc)
			}
		}
		c.slots[index] = sc
	}
}

// OnClick sets a menu-wide click handler.
func OnClick(fn ClickFunc) Option { return func(c *menuConfig) { c.onClick = fn } }

// OnChange sets a menu-wide slot-change handler.
func OnChange(fn ChangeFunc) Option { return func(c *menuConfig) { c.onChange = fn } }

// OnClose sets a callback run when a player closes the virtual menu.
func OnClose(fn CloseFunc) Option { return func(c *menuConfig) { c.onClose = fn } }

// Menu wraps an *inventory.Inventory with declarative slot protection, virtual
// container presentation, and click/change/close callbacks.
type Menu struct {
	mu      sync.RWMutex
	cfg     menuConfig
	inv     *inventory.Inventory
	writing atomic.Bool
}

func build(cfg menuConfig, opts ...Option) *Menu {
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if cfg.size <= 0 {
		switch cfg.kind {
		case DoubleChestKind:
			cfg.size = 54
		case HopperKind:
			cfg.size = 5
		default:
			cfg.size = 27
		}
	}
	cleaned := make(map[int]SlotConfig, len(cfg.slots))
	for k, v := range cfg.slots {
		if k >= 0 && k < cfg.size {
			cleaned[k] = v
		}
	}
	cfg.slots = cleaned
	m := &Menu{cfg: cfg}
	m.inv = inventory.New(cfg.size, nil)
	m.Bind(m.inv)
	return m
}

// Chest creates a 27-slot virtual single-chest menu with title.
func Chest(title string, opts ...Option) *Menu {
	return build(menuConfig{title: title, kind: ChestKind, size: 27}, opts...)
}

// DoubleChest creates a 54-slot virtual double-chest menu with title.
func DoubleChest(title string, opts ...Option) *Menu {
	return build(menuConfig{title: title, kind: DoubleChestKind, size: 54}, opts...)
}

// Hopper creates a 5-slot virtual hopper menu with title.
func Hopper(title string, opts ...Option) *Menu {
	return build(menuConfig{title: title, kind: HopperKind, size: 5}, opts...)
}

// Entity creates an entity-container menu with size slots (defaults to 27).
func Entity(size int, opts ...Option) *Menu {
	if size <= 0 {
		size = 27
	}
	return build(menuConfig{kind: EntityKind, size: size}, opts...)
}

// Title returns the menu's container title.
func (m *Menu) Title() string {
	if m == nil {
		return ""
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg.title
}

// Kind returns the menu's container Kind.
func (m *Menu) Kind() Kind {
	if m == nil {
		return ChestKind
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg.kind
}

// Inventory returns the menu's underlying *inventory.Inventory.
func (m *Menu) Inventory() *inventory.Inventory {
	if m == nil {
		return nil
	}
	return m.inv
}

// SetSlot updates slot configuration and item stack on a live Menu, bypassing
// ReadOnly/Locked validators for programmatic updates.
func (m *Menu) SetSlot(slot int, cfg SlotConfig) {
	if m == nil || slot < 0 || slot >= m.inv.Size() {
		return
	}
	m.mu.Lock()
	if m.cfg.slots == nil {
		m.cfg.slots = make(map[int]SlotConfig)
	}
	m.cfg.slots[slot] = cfg
	m.mu.Unlock()
	m.writing.Store(true)
	_ = m.inv.SetItem(slot, cfg.Item)
	m.writing.Store(false)
}

// SetItem updates only the item stack in slot while keeping existing slot rules.
func (m *Menu) SetItem(slot int, st item.Stack) {
	if m == nil || slot < 0 || slot >= m.inv.Size() {
		return
	}
	m.mu.Lock()
	if m.cfg.slots == nil {
		m.cfg.slots = make(map[int]SlotConfig)
	}
	sc := m.cfg.slots[slot]
	sc.Item = st
	m.cfg.slots[slot] = sc
	m.mu.Unlock()
	m.writing.Store(true)
	_ = m.inv.SetItem(slot, st)
	m.writing.Store(false)
}

// ClearSlot removes slot configuration and clears its item stack.
func (m *Menu) ClearSlot(slot int) {
	if m == nil || slot < 0 || slot >= m.inv.Size() {
		return
	}
	m.mu.Lock()
	delete(m.cfg.slots, slot)
	m.mu.Unlock()
	m.writing.Store(true)
	_ = m.inv.SetItem(slot, item.Stack{})
	m.writing.Store(false)
}

// Bind configures inv with this Menu's initial slot items, slot validator,
// slot change callback, and inventory handler.
func (m *Menu) Bind(inv *inventory.Inventory) {
	if m == nil || inv == nil {
		return
	}
	inv.Handle(inventory.NopHandler{})
	inv.SlotValidatorFunc(func(item.Stack, int) bool { return true })
	inv.SlotFunc(func(int, item.Stack, item.Stack) {})
	m.mu.RLock()
	for slot, cfg := range m.cfg.slots {
		if slot >= 0 && slot < inv.Size() && !cfg.Item.Empty() {
			_ = inv.SetItem(slot, cfg.Item)
		}
	}
	m.mu.RUnlock()
	inv.SlotValidatorFunc(func(it item.Stack, slot int) bool {
		if it.Empty() || m.writing.Load() {
			return true
		}
		m.mu.RLock()
		ro := m.cfg.readOnly
		cfg, ok := m.cfg.slots[slot]
		m.mu.RUnlock()
		if ro {
			return false
		}
		if ok && cfg.Locked {
			return false
		}
		return true
	})
	inv.SlotFunc(func(slot int, before, after item.Stack) {
		m.mu.RLock()
		cfg, ok := m.cfg.slots[slot]
		onChange := m.cfg.onChange
		m.mu.RUnlock()
		if ok && cfg.OnChange != nil {
			cfg.OnChange(slot, before, after)
		}
		if onChange != nil {
			onChange(slot, before, after)
		}
	})
	inv.Handle(&menuHandler{menu: m})
}

// Open displays the menu to p in tx using its configured Kind.
func (m *Menu) Open(p *player.Player, tx *world.Tx) {
	if m == nil || p == nil || tx == nil || tx.Closed() {
		return
	}
	m.mu.RLock()
	kind := m.cfg.kind
	title := m.cfg.title
	onClose := m.cfg.onClose
	m.mu.RUnlock()

	if kind == EntityKind {
		m.OpenForEntity(p, p, tx)
		return
	}
	var vkind menu.VirtualKind
	switch kind {
	case DoubleChestKind:
		vkind = menu.VirtualDoubleChest
	case HopperKind:
		vkind = menu.VirtualHopper
	default:
		vkind = menu.VirtualChest
	}
	var closeCB func(*world.Tx)
	if onClose != nil {
		h := p.H()
		closeCB = func(closeTx *world.Tx) {
			if closeTx != nil && !closeTx.Closed() {
				if pl, ok := h.Entity(closeTx); ok {
					if playerEnt, ok := pl.(*player.Player); ok {
						onClose(playerEnt, closeTx)
						return
					}
				}
			}
			onClose(nil, closeTx)
		}
	}
	p.OpenVirtualContainer(m.inv, vkind, title, closeCB, tx)
}

// OpenForEntity opens the menu's inventory for p as an entity container hosted
// by host (defaulting to p when host is nil).
func (m *Menu) OpenForEntity(p *player.Player, host world.Entity, tx *world.Tx) {
	if m == nil || p == nil || tx == nil || tx.Closed() {
		return
	}
	if host == nil {
		host = p
	}
	p.OpenEntityContainer(host, m.inv, entity.ContainerChestBoat, tx)
}

// OpenBlock opens the block container at pos for p in tx.
func OpenBlock(p *player.Player, pos cube.Pos, tx *world.Tx) {
	if p == nil || tx == nil || tx.Closed() {
		return
	}
	p.OpenBlockContainer(pos, tx)
}

type menuHandler struct {
	menu *Menu
}

func (h *menuHandler) handleClick(ctx *inventory.Context, slot int, it item.Stack) {
	p, _ := ctx.Val().(*player.Player)
	h.menu.mu.RLock()
	ro := h.menu.cfg.readOnly
	cfg, hasSlot := h.menu.cfg.slots[slot]
	globalClick := h.menu.cfg.onClick
	h.menu.mu.RUnlock()

	allow := !ro
	if hasSlot {
		if cfg.Locked {
			allow = false
		}
		if cfg.OnClick != nil {
			allow = cfg.OnClick(p, slot, it) && !cfg.Locked && !ro
		}
	}
	if globalClick != nil {
		if !globalClick(p, slot, it) {
			allow = false
		}
	}
	if !allow {
		ctx.Cancel()
	}
}

func (h *menuHandler) HandleTake(ctx *inventory.Context, slot int, it item.Stack) {
	h.handleClick(ctx, slot, it)
}

func (h *menuHandler) HandlePlace(ctx *inventory.Context, slot int, it item.Stack) {
	h.menu.mu.RLock()
	ro := h.menu.cfg.readOnly
	cfg, hasSlot := h.menu.cfg.slots[slot]
	h.menu.mu.RUnlock()
	if ro {
		ctx.Cancel()
		return
	}
	if hasSlot && cfg.Locked {
		ctx.Cancel()
	}
}

func (h *menuHandler) HandleDrop(ctx *inventory.Context, slot int, it item.Stack) {
	h.handleClick(ctx, slot, it)
}

// PagedItem represents one clickable entry in a PagedMenu.
type PagedItem struct {
	Item    item.Stack
	OnClick func(p *player.Player, index int, it item.Stack)
}

type pagedConfig struct {
	title         string
	formatTitle   func(page, totalPages int) string
	doubleChest   bool
	items         []PagedItem
	prevItem      item.Stack
	nextItem      item.Stack
	pageIndicator item.Stack
	onClose       CloseFunc
}

// PagedOption configures a PagedMenu created by Paged.
type PagedOption func(*pagedConfig)

// Double selects a 54-slot double chest (45 content slots + 9 nav slots)
// instead of a 27-slot single chest (18 content slots + 9 nav slots).
func Double() PagedOption { return func(c *pagedConfig) { c.doubleChest = true } }

// TitleFormat sets a custom title formatter for 1-based page numbers.
func TitleFormat(fn func(page, totalPages int) string) PagedOption {
	return func(c *pagedConfig) { c.formatTitle = fn }
}

// PrevIcon overrides the Previous Page button icon.
func PrevIcon(st item.Stack) PagedOption { return func(c *pagedConfig) { c.prevItem = st } }

// NextIcon overrides the Next Page button icon.
func NextIcon(st item.Stack) PagedOption { return func(c *pagedConfig) { c.nextItem = st } }

// PageIcon overrides the center page-indicator icon.
func PageIcon(st item.Stack) PagedOption { return func(c *pagedConfig) { c.pageIndicator = st } }

// PagedOnClose registers a callback invoked when a player closes the paged menu.
func PagedOnClose(fn CloseFunc) PagedOption { return func(c *pagedConfig) { c.onClose = fn } }

// PagedMenu manages multi-page read-only virtual chest menus with automatic
// Previous/Next navigation buttons on the bottom row.
type PagedMenu struct {
	cfg pagedConfig
}

// Paged creates a paginated virtual chest menu displaying items.
func Paged(title string, items []PagedItem, opts ...PagedOption) *PagedMenu {
	cfg := pagedConfig{
		title: title,
		items: slices.Clone(items),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return &PagedMenu{cfg: cfg}
}

// PageSize returns the number of content slots per page (18 for single chest,
// 45 for double chest).
func (pm *PagedMenu) PageSize() int {
	if pm != nil && pm.cfg.doubleChest {
		return 45
	}
	return 18
}

// PageCount returns the total number of pages (always at least 1).
func (pm *PagedMenu) PageCount() int {
	if pm == nil || len(pm.cfg.items) == 0 {
		return 1
	}
	perPage := pm.PageSize()
	return (len(pm.cfg.items) + perPage - 1) / perPage
}

// BuildPage constructs the Menu representing the 0-based page index.
func (pm *PagedMenu) BuildPage(page int) *Menu {
	total := pm.PageCount()
	page = max(0, min(page, total-1))
	perPage := pm.PageSize()
	kind := ChestKind
	size := 27
	if pm.cfg.doubleChest {
		kind = DoubleChestKind
		size = 54
	}
	title := pm.cfg.title
	if pm.cfg.formatTitle != nil {
		title = pm.cfg.formatTitle(page+1, total)
	} else if total > 1 {
		title = fmt.Sprintf("%s (%d/%d)", pm.cfg.title, page+1, total)
	}
	slots := make(map[int]SlotConfig, perPage+3)
	start := page * perPage
	end := min(start+perPage, len(pm.cfg.items))
	for i := start; i < end; i++ {
		idx := i
		entry := pm.cfg.items[i]
		slots[i-start] = SlotConfig{
			Item:   entry.Item,
			Locked: true,
			OnClick: func(p *player.Player, _ int, it item.Stack) bool {
				if entry.OnClick != nil {
					entry.OnClick(p, idx, it)
				}
				return false
			},
		}
	}
	navStart := perPage
	if page > 0 {
		prevIcon := pm.cfg.prevItem
		if prevIcon.Empty() {
			prevIcon = item.NewStack(item.Arrow{}, 1).WithCustomName("Previous Page")
		}
		targetPage := page - 1
		slots[navStart] = SlotConfig{
			Item:   prevIcon,
			Locked: true,
			OnClick: func(p *player.Player, _ int, _ item.Stack) bool {
				if p != nil && p.Tx() != nil {
					pm.OpenPage(p, targetPage, p.Tx())
				}
				return false
			},
		}
	}
	indicator := pm.cfg.pageIndicator
	if indicator.Empty() {
		indicator = item.NewStack(item.Paper{}, 1).WithCustomName(fmt.Sprintf("Page %d / %d", page+1, total))
	}
	slots[navStart+4] = SlotConfig{Item: indicator, Locked: true}
	if page+1 < total {
		nextIcon := pm.cfg.nextItem
		if nextIcon.Empty() {
			nextIcon = item.NewStack(item.Arrow{}, 1).WithCustomName("Next Page")
		}
		targetPage := page + 1
		slots[navStart+8] = SlotConfig{
			Item:   nextIcon,
			Locked: true,
			OnClick: func(p *player.Player, _ int, _ item.Stack) bool {
				if p != nil && p.Tx() != nil {
					pm.OpenPage(p, targetPage, p.Tx())
				}
				return false
			},
		}
	}
	return build(menuConfig{
		title:    title,
		kind:     kind,
		size:     size,
		readOnly: true,
		slots:    slots,
		onClose:  pm.cfg.onClose,
	})
}

// Open displays page 0 of pm to p in tx.
func (pm *PagedMenu) Open(p *player.Player, tx *world.Tx) {
	pm.OpenPage(p, 0, tx)
}

// OpenPage displays the 0-based page of pm to p in tx.
func (pm *PagedMenu) OpenPage(p *player.Player, page int, tx *world.Tx) {
	if pm == nil || p == nil || tx == nil || tx.Closed() {
		return
	}
	pm.BuildPage(page).Open(p, tx)
}
