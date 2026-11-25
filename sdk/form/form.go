// Package form provides fluent builders for Bedrock menu, modal, and custom
// forms with closure-backed callbacks and typed submission values.
package form

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"unicode/utf8"

	"github.com/df-mc/dragonfly/server/player"
	pform "github.com/df-mc/dragonfly/server/player/form"
	"github.com/df-mc/dragonfly/server/world"
)

type menuButton struct {
	text    string
	image   string
	onClick func(p *player.Player, tx *world.Tx)
}

// Menu builds a Bedrock button menu form with closure callbacks.
type Menu struct {
	title   string
	body    string
	buttons []menuButton
	onClose func(p *player.Player, tx *world.Tx)
}

// NewMenu starts a Menu form builder with title.
func NewMenu(title string) *Menu { return &Menu{title: title} }

// Body sets the descriptive body text shown above buttons.
func (m *Menu) Body(body string) *Menu {
	m.body = body
	return m
}

// Button appends a clickable button with optional image URL/path and handler.
func (m *Menu) Button(text, image string, onClick func(p *player.Player, tx *world.Tx)) *Menu {
	m.buttons = append(m.buttons, menuButton{text: text, image: image, onClick: onClick})
	return m
}

// OnClose registers a handler run if the player dismisses the form.
func (m *Menu) OnClose(fn func(p *player.Player, tx *world.Tx)) *Menu {
	m.onClose = fn
	return m
}

type dynamicMenu struct {
	buttons []pform.Button
	actions []func(p *player.Player, tx *world.Tx)
	onClose func(p *player.Player, tx *world.Tx)
}

func (d dynamicMenu) Submit(s pform.Submitter, pressed pform.Button, tx *world.Tx) {
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

func (d dynamicMenu) Close(s pform.Submitter, tx *world.Tx) {
	if d.onClose != nil {
		p, _ := s.(*player.Player)
		d.onClose(p, tx)
	}
}

// Build compiles the builder into a server/player/form.Menu.
func (m *Menu) Build() pform.Menu {
	btns := make([]pform.Button, len(m.buttons))
	actions := make([]func(*player.Player, *world.Tx), len(m.buttons))
	for i, b := range m.buttons {
		btns[i] = pform.NewButton(b.text, b.image)
		actions[i] = b.onClick
	}
	sub := dynamicMenu{
		buttons: slices.Clone(btns),
		actions: actions,
		onClose: m.onClose,
	}
	return pform.NewMenu(sub, m.title).WithBody(m.body).WithButtons(btns...)
}

// Send compiles and displays the menu form to p.
func (m *Menu) Send(p *player.Player) {
	if m == nil || p == nil {
		return
	}
	p.SendForm(m.Build())
}

// Modal builds a two-button Bedrock confirmation dialog.
type Modal struct {
	title       string
	body        string
	confirmText string
	cancelText  string
	onConfirm   func(p *player.Player, tx *world.Tx)
	onCancel    func(p *player.Player, tx *world.Tx)
	onClose     func(p *player.Player, tx *world.Tx)
}

// NewModal starts a Modal confirmation builder with title and body.
func NewModal(title, body string) *Modal {
	return &Modal{title: title, body: body}
}

// Confirm configures the primary confirmation button label and callback.
func (m *Modal) Confirm(text string, fn func(p *player.Player, tx *world.Tx)) *Modal {
	m.confirmText = text
	m.onConfirm = fn
	return m
}

// Cancel configures the secondary cancel button label and callback.
func (m *Modal) Cancel(text string, fn func(p *player.Player, tx *world.Tx)) *Modal {
	m.cancelText = text
	m.onCancel = fn
	return m
}

// OnClose registers a handler run if the player dismisses the modal.
func (m *Modal) OnClose(fn func(p *player.Player, tx *world.Tx)) *Modal {
	m.onClose = fn
	return m
}

type dynamicModal struct {
	Confirm   pform.Button
	Cancel    pform.Button
	onConfirm func(p *player.Player, tx *world.Tx)
	onCancel  func(p *player.Player, tx *world.Tx)
	onClose   func(p *player.Player, tx *world.Tx)
}

func (d dynamicModal) Submit(s pform.Submitter, pressed pform.Button, tx *world.Tx) {
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

func (d dynamicModal) Close(s pform.Submitter, tx *world.Tx) {
	if d.onClose != nil {
		p, _ := s.(*player.Player)
		d.onClose(p, tx)
	}
}

// Build compiles the builder into a server/player/form.Modal.
func (m *Modal) Build() pform.Modal {
	confirm := pform.YesButton()
	if m.confirmText != "" {
		confirm = pform.NewButton(m.confirmText, "")
	}
	cancel := pform.NoButton()
	if m.cancelText != "" {
		cancel = pform.NewButton(m.cancelText, "")
	}
	sub := dynamicModal{
		Confirm:   confirm,
		Cancel:    cancel,
		onConfirm: m.onConfirm,
		onCancel:  m.onCancel,
		onClose:   m.onClose,
	}
	return pform.NewModal(sub, m.title).WithBody(m.body)
}

// Send compiles and displays the modal form to p.
func (m *Modal) Send(p *player.Player) {
	if m == nil || p == nil {
		return
	}
	p.SendForm(m.Build())
}

// Values holds typed values submitted from a Custom form.
type Values struct {
	byKey   map[string]any
	options map[string][]string
	byIndex []any
}

// String returns the submitted string value for key (for Input, Dropdown, or
// StepSlider where it resolves the selected option label).
func (v Values) String(key string) string {
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
func (v Values) Bool(key string) bool {
	b, _ := v.byKey[key].(bool)
	return b
}

// Float returns the submitted float64 value for a Slider field.
func (v Values) Float(key string) float64 {
	f, _ := v.byKey[key].(float64)
	return f
}

// Int returns the submitted integer value for a Slider (rounded) or
// Dropdown/StepSlider index.
func (v Values) Int(key string) int {
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
func (v Values) Index(key string) int {
	idx, _ := v.byKey[key].(int)
	return idx
}

// Raw returns the raw submitted value for key, or nil if absent.
func (v Values) Raw(key string) any { return v.byKey[key] }

// At returns the raw submitted value at element index i, or nil if out of range.
func (v Values) At(i int) any {
	if i < 0 || i >= len(v.byIndex) {
		return nil
	}
	return v.byIndex[i]
}

type fieldEntry struct {
	key  string
	elem pform.Element
}

// Custom builds a Bedrock custom form with inputs, toggles, sliders,
// dropdowns, step sliders, labels, headers, and dividers without reflection.
type Custom struct {
	title    string
	fields   []fieldEntry
	onSubmit func(p *player.Player, values Values, tx *world.Tx)
	onClose  func(p *player.Player, tx *world.Tx)
}

// NewCustom starts a Custom form builder with title.
func NewCustom(title string) *Custom { return &Custom{title: title} }

// Label appends a read-only text label element.
func (c *Custom) Label(text string) *Custom {
	c.fields = append(c.fields, fieldEntry{elem: pform.NewLabel(text)})
	return c
}

// Header appends a read-only section header element.
func (c *Custom) Header(text string) *Custom {
	c.fields = append(c.fields, fieldEntry{elem: pform.NewHeader(text)})
	return c
}

// Divider appends a horizontal divider line element.
func (c *Custom) Divider() *Custom {
	c.fields = append(c.fields, fieldEntry{elem: pform.Divider{}})
	return c
}

// Input appends a text input field keyed by key.
func (c *Custom) Input(key, text, defaultVal, placeholder string) *Custom {
	c.fields = append(c.fields, fieldEntry{key: key, elem: pform.NewInput(text, defaultVal, placeholder)})
	return c
}

// Toggle appends a boolean toggle field keyed by key.
func (c *Custom) Toggle(key, text string, defaultVal bool) *Custom {
	c.fields = append(c.fields, fieldEntry{key: key, elem: pform.NewToggle(text, defaultVal)})
	return c
}

// Slider appends a numeric range slider field keyed by key.
func (c *Custom) Slider(key, text string, minVal, maxVal, step, defaultVal float64) *Custom {
	c.fields = append(c.fields, fieldEntry{key: key, elem: pform.NewSlider(text, minVal, maxVal, step, defaultVal)})
	return c
}

// Dropdown appends a dropdown option selector field keyed by key.
func (c *Custom) Dropdown(key, text string, options []string, defaultIndex int) *Custom {
	c.fields = append(c.fields, fieldEntry{key: key, elem: pform.NewDropdown(text, slices.Clone(options), defaultIndex)})
	return c
}

// StepSlider appends a step-slider option selector field keyed by key.
func (c *Custom) StepSlider(key, text string, options []string, defaultIndex int) *Custom {
	c.fields = append(c.fields, fieldEntry{key: key, elem: pform.NewStepSlider(text, slices.Clone(options), defaultIndex)})
	return c
}

// OnSubmit registers the callback receiving typed Values when submitted.
func (c *Custom) OnSubmit(fn func(p *player.Player, values Values, tx *world.Tx)) *Custom {
	c.onSubmit = fn
	return c
}

// OnClose registers a callback run if the player closes the form without submitting.
func (c *Custom) OnClose(fn func(p *player.Player, tx *world.Tx)) *Custom {
	c.onClose = fn
	return c
}

// Build compiles the builder into a server/player/form.Form.
func (c *Custom) Build() pform.Form {
	return dynamicCustom{
		title:    c.title,
		fields:   slices.Clone(c.fields),
		onSubmit: c.onSubmit,
		onClose:  c.onClose,
	}
}

// Send compiles and displays the custom form to p.
func (c *Custom) Send(p *player.Player) {
	if c == nil || p == nil {
		return
	}
	p.SendForm(c.Build())
}

type dynamicCustom struct {
	title    string
	fields   []fieldEntry
	onSubmit func(p *player.Player, values Values, tx *world.Tx)
	onClose  func(p *player.Player, tx *world.Tx)
}

func (f dynamicCustom) MarshalJSON() ([]byte, error) {
	elems := make([]pform.Element, len(f.fields))
	for i, field := range f.fields {
		elems[i] = field.elem
	}
	return json.Marshal(map[string]any{
		"type":    "custom_form",
		"title":   f.title,
		"content": elems,
	})
}

func (f dynamicCustom) SubmitJSON(b []byte, submitter pform.Submitter, tx *world.Tx) error {
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
		return fmt.Errorf("sdk/form: decode JSON array: %w", err)
	}
	if len(data) < len(f.fields) {
		return fmt.Errorf("sdk/form: expected %d values, got %d", len(f.fields), len(data))
	}
	vals := Values{
		byKey:   make(map[string]any, len(f.fields)),
		options: make(map[string][]string),
		byIndex: make([]any, len(f.fields)),
	}
	for i, field := range f.fields {
		parsed, opts, err := parseField(field.elem, data[i])
		if err != nil {
			return fmt.Errorf("sdk/form: field %d (%q): %w", i, field.key, err)
		}
		vals.byIndex[i] = parsed
		if field.key != "" {
			vals.byKey[field.key] = parsed
			if len(opts) > 0 {
				vals.options[field.key] = opts
			}
		}
	}
	if f.onSubmit != nil {
		f.onSubmit(p, vals, tx)
	}
	return nil
}

func parseField(elem pform.Element, raw any) (any, []string, error) {
	switch e := elem.(type) {
	case pform.Label, pform.Header, pform.Divider:
		return nil, nil, nil
	case pform.Input:
		s, ok := raw.(string)
		if !ok {
			return nil, nil, fmt.Errorf("expected string, got %T", raw)
		}
		if !utf8.ValidString(s) {
			return nil, nil, fmt.Errorf("invalid UTF-8 string")
		}
		return s, nil, nil
	case pform.Toggle:
		b, ok := raw.(bool)
		if !ok {
			return nil, nil, fmt.Errorf("expected bool, got %T", raw)
		}
		return b, nil, nil
	case pform.Slider:
		num, ok := raw.(json.Number)
		if !ok {
			return nil, nil, fmt.Errorf("expected number, got %T", raw)
		}
		v, err := num.Float64()
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < e.Min || v > e.Max {
			return nil, nil, fmt.Errorf("slider value %v out of bounds [%v, %v]", raw, e.Min, e.Max)
		}
		return v, nil, nil
	case pform.Dropdown:
		idx, err := parseIndex(raw, len(e.Options))
		return idx, e.Options, err
	case pform.StepSlider:
		idx, err := parseIndex(raw, len(e.Options))
		return idx, e.Options, err
	default:
		return raw, nil, nil
	}
}

func parseIndex(raw any, n int) (int, error) {
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
