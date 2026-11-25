package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"sync"
	"sync/atomic"
	"unicode/utf8"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/menu"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/form"
	"github.com/df-mc/dragonfly/server/world"
)

// MenuKind selects how CustomMenu.Open renders the container screen to players.
type MenuKind uint8

const (
	// MenuChest opens a virtual 27-slot single chest with a custom title.
	MenuChest MenuKind = iota
	// MenuDoubleChest opens a virtual 54-slot double chest with a custom title.
	MenuDoubleChest
	// MenuHopper opens a virtual 5-slot hopper with a custom title.
	MenuHopper
	// MenuEntity opens the inventory as an entity container (ChestBoat layout).
	MenuEntity
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
	// Title sets the custom container title displayed in virtual chest/hopper GUIs.
	Title string
	// Kind selects the container layout for Open (defaults to MenuChest; when
	// Size > 27 and Kind is zero, MenuDoubleChest is used automatically).
	Kind MenuKind
	// Size sets the inventory slot count (defaults to 27 for chest, 54 for
	// double chest, or 5 for hopper when <= 0).
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
	// OnClose runs when a player closes a virtual menu opened via Open.
	OnClose func(p *player.Player, tx *world.Tx)
}

// CustomMenu wraps an *inventory.Inventory with declarative slot protection,
// virtual chest/double-chest/hopper presentation, and click/change callbacks.
type CustomMenu struct {
	mu      sync.RWMutex
	spec    MenuSpec
	inv     *inventory.Inventory
	writing atomic.Bool
}

func resolveMenuSizeAndKind(spec MenuSpec) (int, MenuKind) {
	kind := spec.Kind
	size := spec.Size
	if kind == MenuChest && size > 27 {
		kind = MenuDoubleChest
	}
	if size <= 0 {
		switch kind {
		case MenuDoubleChest:
			size = 54
		case MenuHopper:
			size = 5
		default:
			size = 27
		}
	}
	return size, kind
}

// NewMenu constructs a CustomMenu from spec.
func NewMenu(spec MenuSpec) *CustomMenu {
	size, kind := resolveMenuSizeAndKind(spec)
	spec.Kind = kind
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

// Title returns the configured container title.
func (m *CustomMenu) Title() string {
	if m == nil {
		return ""
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.spec.Title
}

// Kind returns the configured MenuKind.
func (m *CustomMenu) Kind() MenuKind {
	if m == nil {
		return MenuChest
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.spec.Kind
}

// Inventory returns the menu's underlying *inventory.Inventory.
func (m *CustomMenu) Inventory() *inventory.Inventory {
	if m == nil {
		return nil
	}
	return m.inv
}

// SetSlot updates slot configuration and item stack on a live CustomMenu.
func (m *CustomMenu) SetSlot(slot int, cfg MenuSlot) {
	if m == nil || slot < 0 || slot >= m.inv.Size() {
		return
	}
	m.mu.Lock()
	if m.spec.Slots == nil {
		m.spec.Slots = make(map[int]MenuSlot)
	}
	m.spec.Slots[slot] = cfg
	m.mu.Unlock()
	m.writing.Store(true)
	_ = m.inv.SetItem(slot, cfg.Item)
	m.writing.Store(false)
}

// ClearSlot removes slot configuration and clears its item stack.
func (m *CustomMenu) ClearSlot(slot int) {
	if m == nil || slot < 0 || slot >= m.inv.Size() {
		return
	}
	m.mu.Lock()
	delete(m.spec.Slots, slot)
	m.mu.Unlock()
	m.writing.Store(true)
	_ = m.inv.SetItem(slot, item.Stack{})
	m.writing.Store(false)
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
	m.mu.RLock()
	for slot, cfg := range m.spec.Slots {
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
		ro := m.spec.ReadOnly
		cfg, ok := m.spec.Slots[slot]
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
		cfg, ok := m.spec.Slots[slot]
		onChange := m.spec.OnChange
		m.mu.RUnlock()
		if ok && cfg.OnChange != nil {
			cfg.OnChange(slot, before, after)
		}
		if onChange != nil {
			onChange(slot, before, after)
		}
	})
	inv.Handle(&customMenuHandler{menu: m})
}

// Open displays the menu to p in tx using its configured MenuKind (virtual
// single chest, double chest, hopper with custom title, or entity container).
func (m *CustomMenu) Open(p *player.Player, tx *world.Tx) {
	if m == nil || p == nil || tx == nil || tx.Closed() {
		return
	}
	m.mu.RLock()
	kind := m.spec.Kind
	title := m.spec.Title
	onClose := m.spec.OnClose
	m.mu.RUnlock()

	if kind == MenuEntity {
		m.OpenForEntity(p, p, tx)
		return
	}
	var vkind menu.VirtualKind
	switch kind {
	case MenuDoubleChest:
		vkind = menu.VirtualDoubleChest
	case MenuHopper:
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

// OpenBlockMenu opens the block container at pos for p in tx.
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
	h.menu.mu.RLock()
	ro := h.menu.spec.ReadOnly
	cfg, hasSlot := h.menu.spec.Slots[slot]
	globalClick := h.menu.spec.OnClick
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

func (h *customMenuHandler) HandleTake(ctx *inventory.Context, slot int, it item.Stack) {
	h.handleClick(ctx, slot, it)
}

func (h *customMenuHandler) HandlePlace(ctx *inventory.Context, slot int, it item.Stack) {
	h.menu.mu.RLock()
	ro := h.menu.spec.ReadOnly
	cfg, hasSlot := h.menu.spec.Slots[slot]
	h.menu.mu.RUnlock()
	if ro {
		ctx.Cancel()
		return
	}
	if hasSlot && cfg.Locked {
		ctx.Cancel()
	}
}

func (h *customMenuHandler) HandleDrop(ctx *inventory.Context, slot int, it item.Stack) {
	h.handleClick(ctx, slot, it)
}

// PagedMenuItem represents one clickable entry in a PagedMenu.
type PagedMenuItem struct {
	Item    item.Stack
	OnClick func(p *player.Player, index int, it item.Stack)
}

// PagedMenuSpec configures a paginated read-only virtual chest GUI with
// automatic Previous/Next navigation bar on the bottom row.
type PagedMenuSpec struct {
	// Title is the base title; " (page/total)" is appended when FormatTitle is nil.
	Title string
	// FormatTitle optionally formats the container title for a 1-based page.
	FormatTitle func(page, totalPages int) string
	// DoubleChest selects a 54-slot double chest (45 content slots + 9 nav slots);
	// when false a 27-slot chest (18 content slots + 9 nav slots) is used.
	DoubleChest bool
	// Items lists all entries across pages.
	Items []PagedMenuItem
	// PrevItem overrides the Previous Page button icon.
	PrevItem item.Stack
	// NextItem overrides the Next Page button icon.
	NextItem item.Stack
	// PageIndicator overrides the center page-indicator icon.
	PageIndicator item.Stack
	// OnClose runs when a player closes the paginated menu.
	OnClose func(p *player.Player, tx *world.Tx)
}

// PagedMenu manages multi-page virtual chest menus for players.
type PagedMenu struct {
	spec PagedMenuSpec
}

// NewPagedMenu creates a PagedMenu from spec.
func NewPagedMenu(spec PagedMenuSpec) *PagedMenu {
	spec.Items = slices.Clone(spec.Items)
	return &PagedMenu{spec: spec}
}

// PageSize returns the number of content slots per page (18 for single chest,
// 45 for double chest).
func (pm *PagedMenu) PageSize() int {
	if pm != nil && pm.spec.DoubleChest {
		return 45
	}
	return 18
}

// PageCount returns the total number of pages (always at least 1).
func (pm *PagedMenu) PageCount() int {
	if pm == nil || len(pm.spec.Items) == 0 {
		return 1
	}
	perPage := pm.PageSize()
	return (len(pm.spec.Items) + perPage - 1) / perPage
}

// BuildPage constructs the CustomMenu representing the 0-based page index.
func (pm *PagedMenu) BuildPage(page int) *CustomMenu {
	total := pm.PageCount()
	page = max(0, min(page, total-1))
	perPage := pm.PageSize()
	kind := MenuChest
	size := 27
	if pm.spec.DoubleChest {
		kind = MenuDoubleChest
		size = 54
	}
	title := pm.spec.Title
	if pm.spec.FormatTitle != nil {
		title = pm.spec.FormatTitle(page+1, total)
	} else if total > 1 {
		title = fmt.Sprintf("%s (%d/%d)", pm.spec.Title, page+1, total)
	}
	slots := make(map[int]MenuSlot, perPage+3)
	start := page * perPage
	end := min(start+perPage, len(pm.spec.Items))
	for i := start; i < end; i++ {
		idx := i
		entry := pm.spec.Items[i]
		slots[i-start] = MenuSlot{
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
		prevIcon := pm.spec.PrevItem
		if prevIcon.Empty() {
			prevIcon = item.NewStack(item.Arrow{}, 1).WithCustomName("Previous Page")
		}
		targetPage := page - 1
		slots[navStart] = MenuSlot{
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
	indicator := pm.spec.PageIndicator
	if indicator.Empty() {
		indicator = item.NewStack(item.Paper{}, 1).WithCustomName(fmt.Sprintf("Page %d / %d", page+1, total))
	}
	slots[navStart+4] = MenuSlot{Item: indicator, Locked: true}
	if page+1 < total {
		nextIcon := pm.spec.NextItem
		if nextIcon.Empty() {
			nextIcon = item.NewStack(item.Arrow{}, 1).WithCustomName("Next Page")
		}
		targetPage := page + 1
		slots[navStart+8] = MenuSlot{
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
	return NewMenu(MenuSpec{
		Title:    title,
		Kind:     kind,
		Size:     size,
		ReadOnly: true,
		Slots:    slots,
		OnClose:  pm.spec.OnClose,
	})
}

// Open opens page 0 of pm for p in tx.
func (pm *PagedMenu) Open(p *player.Player, tx *world.Tx) {
	pm.OpenPage(p, 0, tx)
}

// OpenPage opens the 0-based page of pm for p in tx.
func (pm *PagedMenu) OpenPage(p *player.Player, page int, tx *world.Tx) {
	if pm == nil || p == nil || tx == nil || tx.Closed() {
		return
	}
	pm.BuildPage(page).Open(p, tx)
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

// FormField pairs a lookup key with a Bedrock form.Element in CustomFormSpec.
type FormField struct {
	Key     string
	Element form.Element
}

// LabelField builds a read-only text label element for CustomFormSpec.
func LabelField(text string) FormField { return FormField{Element: form.NewLabel(text)} }

// HeaderField builds a read-only section header element for CustomFormSpec.
func HeaderField(text string) FormField { return FormField{Element: form.NewHeader(text)} }

// DividerField builds a horizontal divider line element for CustomFormSpec.
func DividerField() FormField { return FormField{Element: form.Divider{}} }

// InputField builds a text input field keyed by key.
func InputField(key, text, defaultVal, placeholder string) FormField {
	return FormField{Key: key, Element: form.NewInput(text, defaultVal, placeholder)}
}

// ToggleField builds a boolean toggle field keyed by key.
func ToggleField(key, text string, defaultVal bool) FormField {
	return FormField{Key: key, Element: form.NewToggle(text, defaultVal)}
}

// SliderField builds a numeric range slider field keyed by key.
func SliderField(key, text string, minVal, maxVal, step, defaultVal float64) FormField {
	return FormField{Key: key, Element: form.NewSlider(text, minVal, maxVal, step, defaultVal)}
}

// DropdownField builds a dropdown option selector field keyed by key.
func DropdownField(key, text string, options []string, defaultIndex int) FormField {
	return FormField{Key: key, Element: form.NewDropdown(text, slices.Clone(options), defaultIndex)}
}

// StepSliderField builds a step-slider option selector field keyed by key.
func StepSliderField(key, text string, options []string, defaultIndex int) FormField {
	return FormField{Key: key, Element: form.NewStepSlider(text, slices.Clone(options), defaultIndex)}
}

// FormValues holds typed submitted values returned to CustomFormSpec.OnSubmit.
type FormValues struct {
	byKey   map[string]any
	options map[string][]string
	byIndex []any
}

// String returns the submitted string value for key (for Input, Dropdown, or
// StepSlider where it resolves the selected option label).
func (v FormValues) String(key string) string {
	val, ok := v.byKey[key]
	if !ok {
		return ""
	}
	if s, ok := val.(string); ok {
		return s
	}
	if idx, ok := val.(int); ok {
		if opts := v.options[key]; idx >= 0 && idx < len(opts) {
			return opts[idx]
		}
	}
	return ""
}

// Bool returns the submitted boolean value for a Toggle field.
func (v FormValues) Bool(key string) bool {
	b, _ := v.byKey[key].(bool)
	return b
}

// Float returns the submitted float64 value for a Slider field.
func (v FormValues) Float(key string) float64 {
	f, _ := v.byKey[key].(float64)
	return f
}

// Int returns the submitted integer value for a Slider (rounded) or
// Dropdown/StepSlider index.
func (v FormValues) Int(key string) int {
	switch val := v.byKey[key].(type) {
	case int:
		return val
	case float64:
		return int(math.Round(val))
	default:
		return 0
	}
}

// Index returns the selected option index for a Dropdown or StepSlider field.
func (v FormValues) Index(key string) int {
	idx, _ := v.byKey[key].(int)
	return idx
}

// Raw returns the raw submitted value by key, or nil if absent.
func (v FormValues) Raw(key string) any { return v.byKey[key] }

// At returns the raw submitted value at element index i, or nil if out of range.
func (v FormValues) At(i int) any {
	if i < 0 || i >= len(v.byIndex) {
		return nil
	}
	return v.byIndex[i]
}

// CustomFormSpec declaratively configures a Bedrock custom form with inputs,
// toggles, sliders, dropdowns, step sliders, labels, headers, and dividers.
type CustomFormSpec struct {
	Title    string
	Fields   []FormField
	OnSubmit func(p *player.Player, values FormValues, tx *world.Tx)
	OnClose  func(p *player.Player, tx *world.Tx)
}

type dynamicCustomForm struct {
	title    string
	fields   []FormField
	onSubmit func(p *player.Player, values FormValues, tx *world.Tx)
	onClose  func(p *player.Player, tx *world.Tx)
}

// NewCustomForm compiles spec into a form.Form without reflection structs.
func NewCustomForm(spec CustomFormSpec) form.Form {
	return dynamicCustomForm{
		title:    spec.Title,
		fields:   slices.Clone(spec.Fields),
		onSubmit: spec.OnSubmit,
		onClose:  spec.OnClose,
	}
}

// SendCustomForm displays a declarative CustomFormSpec to p.
func SendCustomForm(p *player.Player, spec CustomFormSpec) {
	if p == nil {
		return
	}
	p.SendForm(NewCustomForm(spec))
}

func (f dynamicCustomForm) MarshalJSON() ([]byte, error) {
	elems := make([]form.Element, len(f.fields))
	for i, field := range f.fields {
		elems[i] = field.Element
	}
	return json.Marshal(map[string]any{
		"type":    "custom_form",
		"title":   f.title,
		"content": elems,
	})
}

func (f dynamicCustomForm) SubmitJSON(b []byte, submitter form.Submitter, tx *world.Tx) error {
	p, _ := submitter.(*player.Player)
	if b == nil {
		if f.onClose != nil {
			f.onClose(p, tx)
		}
		return nil
	}
	dec := json.NewDecoder(bytes.NewBuffer(b))
	dec.UseNumber()
	var data []any
	if err := dec.Decode(&data); err != nil {
		return fmt.Errorf("api custom form: decode JSON array: %w", err)
	}
	if len(data) < len(f.fields) {
		return fmt.Errorf("api custom form: expected %d values, got %d", len(f.fields), len(data))
	}
	vals := FormValues{
		byKey:   make(map[string]any, len(f.fields)),
		options: make(map[string][]string),
		byIndex: make([]any, len(f.fields)),
	}
	for i, field := range f.fields {
		parsed, opts, err := parseDynamicFormField(field.Element, data[i])
		if err != nil {
			return fmt.Errorf("api custom form: field %d (%q): %w", i, field.Key, err)
		}
		vals.byIndex[i] = parsed
		if field.Key != "" {
			vals.byKey[field.Key] = parsed
			if len(opts) > 0 {
				vals.options[field.Key] = opts
			}
		}
	}
	if f.onSubmit != nil {
		f.onSubmit(p, vals, tx)
	}
	return nil
}

func parseDynamicFormField(elem form.Element, raw any) (any, []string, error) {
	switch e := elem.(type) {
	case form.Label, form.Header, form.Divider:
		return nil, nil, nil
	case form.Input:
		s, ok := raw.(string)
		if !ok {
			return nil, nil, fmt.Errorf("expected string, got %T", raw)
		}
		if !utf8.ValidString(s) {
			return nil, nil, fmt.Errorf("invalid UTF-8 string")
		}
		return s, nil, nil
	case form.Toggle:
		b, ok := raw.(bool)
		if !ok {
			return nil, nil, fmt.Errorf("expected bool, got %T", raw)
		}
		return b, nil, nil
	case form.Slider:
		num, ok := raw.(json.Number)
		if !ok {
			return nil, nil, fmt.Errorf("expected number, got %T", raw)
		}
		v, err := num.Float64()
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < e.Min || v > e.Max {
			return nil, nil, fmt.Errorf("slider value %v out of bounds [%v, %v]", raw, e.Min, e.Max)
		}
		return v, nil, nil
	case form.Dropdown:
		idx, err := parseFormIndex(raw, len(e.Options))
		return idx, e.Options, err
	case form.StepSlider:
		idx, err := parseFormIndex(raw, len(e.Options))
		return idx, e.Options, err
	default:
		return raw, nil, nil
	}
}

func parseFormIndex(raw any, n int) (int, error) {
	num, ok := raw.(json.Number)
	if !ok {
		return 0, fmt.Errorf("expected integer index, got %T", raw)
	}
	v, err := num.Int64()
	if err != nil || v < 0 || int(v) >= n {
		return 0, fmt.Errorf("option index %v out of bounds [0, %d)", raw, n)
	}
	return int(v), nil
}
