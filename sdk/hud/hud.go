// Package hud provides fluent builders and helpers for player-facing HUD
// elements, audio/particles, NPC dialogues, chat broadcasts, and floating
// multi-line holograms.
package hud

import (
	"slices"
	"time"

	"github.com/df-mc/dragonfly/sdk/mob"
	"github.com/df-mc/dragonfly/sdk/plugin"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/bossbar"
	"github.com/df-mc/dragonfly/server/player/dialogue"
	"github.com/df-mc/dragonfly/server/player/scoreboard"
	"github.com/df-mc/dragonfly/server/player/title"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// Scoreboard builds a sidebar scoreboard.
type Scoreboard struct {
	title      string
	lines      []string
	descending bool
	noPadding  bool
}

// NewScoreboard starts a sidebar Scoreboard with title and optional initial lines.
func NewScoreboard(title string, lines ...string) *Scoreboard {
	return &Scoreboard{title: title, lines: slices.Clone(lines)}
}

// Line appends a single line to the scoreboard.
func (s *Scoreboard) Line(line string) *Scoreboard {
	s.lines = append(s.lines, line)
	return s
}

// Lines appends multiple lines to the scoreboard.
func (s *Scoreboard) Lines(lines ...string) *Scoreboard {
	s.lines = append(s.lines, lines...)
	return s
}

// Descending reverses line ordering.
func (s *Scoreboard) Descending() *Scoreboard {
	s.descending = true
	return s
}

// NoPadding removes automatic padding around scoreboard lines.
func (s *Scoreboard) NoPadding() *Scoreboard {
	s.noPadding = true
	return s
}

// Build constructs a *scoreboard.Scoreboard from the builder.
func (s *Scoreboard) Build() *scoreboard.Scoreboard {
	sb := scoreboard.New(s.title)
	if s.noPadding {
		sb.RemovePadding()
	}
	if s.descending {
		sb.SetDescending()
	}
	for i, line := range s.lines {
		sb.Set(i, line)
	}
	return sb
}

// Show displays the scoreboard to p.
func (s *Scoreboard) Show(p *player.Player) {
	if s == nil || p == nil {
		return
	}
	p.SendScoreboard(s.Build())
}

// HideScoreboard clears any sidebar scoreboard shown to p.
func HideScoreboard(p *player.Player) {
	if p == nil {
		return
	}
	p.RemoveScoreboard()
}

// BossBar builds a Bedrock boss bar without sentinel boolean flags.
type BossBar struct {
	id     string
	text   string
	health *float64
	colour *bossbar.Colour
}

// NewBossBar starts a BossBar builder displaying text.
func NewBossBar(text string) *BossBar { return &BossBar{text: text} }

// ID distinguishes simultaneous boss bars (defaults to "default").
func (b *BossBar) ID(id string) *BossBar {
	b.id = id
	return b
}

// Health sets the fill percentage clamped to [0, 1].
func (b *BossBar) Health(pct float64) *BossBar {
	h := max(0.0, min(1.0, pct))
	b.health = &h
	return b
}

// Colour sets the boss bar colour.
func (b *BossBar) Colour(c bossbar.Colour) *BossBar {
	b.colour = &c
	return b
}

// Build constructs a bossbar.BossBar from the builder.
func (b *BossBar) Build() bossbar.BossBar {
	bar := bossbar.New(b.text)
	if b.id != "" {
		bar = bar.WithID(b.id)
	}
	if b.health != nil {
		bar = bar.WithHealthPercentage(*b.health)
	}
	if b.colour != nil {
		bar = bar.WithColour(*b.colour)
	}
	return bar
}

// Show displays the boss bar to p.
func (b *BossBar) Show(p *player.Player) {
	if b == nil || p == nil {
		return
	}
	p.SendBossBar(b.Build())
}

// Hide removes this boss bar (by its ID if set, otherwise the default bar) from p.
func (b *BossBar) Hide(p *player.Player) {
	if b == nil || p == nil {
		return
	}
	if b.id != "" {
		p.RemoveBossBarID(b.id)
		return
	}
	p.RemoveBossBar()
}

// HideBossBar removes the default boss bar from p.
func HideBossBar(p *player.Player) {
	if p == nil {
		return
	}
	p.RemoveBossBar()
}

// HideBossBarID removes the boss bar identified by id from p.
func HideBossBarID(p *player.Player, id string) {
	if p == nil || id == "" {
		return
	}
	p.RemoveBossBarID(id)
}

// Title builds a screen title, subtitle, and action text.
type Title struct {
	heading    string
	subtitle   string
	actionText string
	fadeIn     time.Duration
	duration   time.Duration
	fadeOut    time.Duration
}

// NewTitle starts a screen Title builder with heading.
func NewTitle(heading string) *Title { return &Title{heading: heading} }

// Subtitle sets the subtitle text displayed under the heading.
func (t *Title) Subtitle(s string) *Title {
	t.subtitle = s
	return t
}

// ActionText sets the action bar text paired with the title.
func (t *Title) ActionText(s string) *Title {
	t.actionText = s
	return t
}

// FadeIn sets the fade-in duration.
func (t *Title) FadeIn(d time.Duration) *Title {
	t.fadeIn = d
	return t
}

// Duration sets the stay duration.
func (t *Title) Duration(d time.Duration) *Title {
	t.duration = d
	return t
}

// FadeOut sets the fade-out duration.
func (t *Title) FadeOut(d time.Duration) *Title {
	t.fadeOut = d
	return t
}

// Build constructs a title.Title from the builder.
func (t *Title) Build() title.Title {
	out := title.New(t.heading)
	if t.subtitle != "" {
		out = out.WithSubtitle(t.subtitle)
	}
	if t.actionText != "" {
		out = out.WithActionText(t.actionText)
	}
	if t.fadeIn > 0 {
		out = out.WithFadeInDuration(t.fadeIn)
	}
	if t.duration > 0 {
		out = out.WithDuration(t.duration)
	}
	if t.fadeOut > 0 {
		out = out.WithFadeOutDuration(t.fadeOut)
	}
	return out
}

// Show displays the title to p.
func (t *Title) Show(p *player.Player) {
	if t == nil || p == nil {
		return
	}
	p.SendTitle(t.Build())
}

// ActionBar displays text above the hotbar (Bedrock jukebox/actionbar slot).
func ActionBar(p *player.Player, text string) {
	if p != nil {
		p.SendJukeboxPopup(text)
	}
}

// Popup displays popup text above the hotbar to p.
func Popup(p *player.Player, text string) {
	if p != nil {
		p.SendPopup(text)
	}
}

// Tip displays tip text above the hotbar to p.
func Tip(p *player.Player, text string) {
	if p != nil {
		p.SendTip(text)
	}
}

// Toast displays a top-screen Bedrock toast notification to p.
func Toast(p *player.Player, heading, message string) {
	if p != nil {
		p.SendToast(heading, message)
	}
}

// Broadcast sends a chat message to every online player on app.
// Do not call from inside a world owner transaction; use BroadcastWorld instead.
func Broadcast(app *plugin.App, msg ...any) {
	if app == nil {
		return
	}
	for p := range app.Players() {
		p.Message(msg...)
	}
}

// BroadcastWorld sends a chat message to every online player in tx's world.
func BroadcastWorld(app *plugin.App, tx *world.Tx, msg ...any) {
	if app == nil || tx == nil || tx.Closed() {
		return
	}
	for p := range app.PlayersIn(tx) {
		p.Message(msg...)
	}
}

// Sound plays s exclusively for player p.
func Sound(p *player.Player, s world.Sound) {
	if p != nil && s != nil {
		p.PlaySound(s)
	}
}

// SoundAt plays s in tx at pos for all nearby viewers.
func SoundAt(tx *world.Tx, pos mgl64.Vec3, s world.Sound) {
	if tx != nil && !tx.Closed() && s != nil {
		tx.PlaySound(pos, s)
	}
}

// CustomSound plays a named/resource-pack sound exclusively for player p.
func CustomSound(p *player.Player, name string, volume, pitch float64) {
	if p == nil || name == "" {
		return
	}
	if volume <= 0 {
		volume = 1
	}
	if pitch <= 0 {
		pitch = 1
	}
	p.PlaySound(sound.Custom{Name: name, Volume: volume, Pitch: pitch})
}

// CustomSoundAt plays a named/resource-pack sound in tx at pos.
func CustomSoundAt(tx *world.Tx, pos mgl64.Vec3, name string, volume, pitch float64) {
	if tx == nil || tx.Closed() || name == "" {
		return
	}
	if volume <= 0 {
		volume = 1
	}
	if pitch <= 0 {
		pitch = 1
	}
	tx.PlaySound(pos, sound.Custom{Name: name, Volume: volume, Pitch: pitch})
}

// Particle spawns particle pt in tx at pos for all viewers of that chunk.
func Particle(tx *world.Tx, pos mgl64.Vec3, pt world.Particle) {
	if tx != nil && !tx.Closed() && pt != nil {
		tx.AddParticle(pos, pt)
	}
}

type dialogueButton struct {
	text    string
	onClick func(p *player.Player, tx *world.Tx)
}

// Dialogue builds an NPC dialogue window with closure button handlers.
type Dialogue struct {
	title   string
	body    string
	display dialogue.DisplaySettings
	buttons []dialogueButton
	onClose func(p *player.Player, tx *world.Tx)
}

// NewDialogue starts an NPC Dialogue builder with title.
func NewDialogue(title string) *Dialogue { return &Dialogue{title: title} }

// Body sets the NPC speech body text.
func (d *Dialogue) Body(body string) *Dialogue {
	d.body = body
	return d
}

// Display configures the NPC model display settings in the dialogue pane.
func (d *Dialogue) Display(settings dialogue.DisplaySettings) *Dialogue {
	d.display = settings
	return d
}

// Button appends a clickable dialogue button (up to 6 supported by Bedrock).
func (d *Dialogue) Button(text string, onClick func(p *player.Player, tx *world.Tx)) *Dialogue {
	d.buttons = append(d.buttons, dialogueButton{text: text, onClick: onClick})
	return d
}

// OnClose registers a handler run if the player dismisses the dialogue.
func (d *Dialogue) OnClose(fn func(p *player.Player, tx *world.Tx)) *Dialogue {
	d.onClose = fn
	return d
}

type dynamicDialogue struct {
	buttons []dialogue.Button
	actions []func(p *player.Player, tx *world.Tx)
	onClose func(p *player.Player, tx *world.Tx)
}

func (d dynamicDialogue) Submit(s dialogue.Submitter, pressed dialogue.Button, tx *world.Tx) {
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

func (d dynamicDialogue) Close(s dialogue.Submitter, tx *world.Tx) {
	if d.onClose != nil {
		p, _ := s.(*player.Player)
		d.onClose(p, tx)
	}
}

// Build compiles the builder into a server/player/dialogue.Dialogue.
func (d *Dialogue) Build() dialogue.Dialogue {
	btns := make([]dialogue.Button, len(d.buttons))
	actions := make([]func(*player.Player, *world.Tx), len(d.buttons))
	for i, b := range d.buttons {
		btns[i] = dialogue.Button{Text: b.text}
		actions[i] = b.onClick
	}
	return dialogue.New(dynamicDialogue{
		buttons: slices.Clone(btns),
		actions: actions,
		onClose: d.onClose,
	}, d.title).WithBody(d.body).WithDisplay(d.display).WithButtons(btns...)
}

// Show displays the NPC dialogue attached to entity e to player p.
func (d *Dialogue) Show(p *player.Player, e world.Entity) {
	if d == nil || p == nil || e == nil {
		return
	}
	p.SendDialogue(d.Build(), e)
}

const defaultHologramSpacing = 0.28

var hologramEntityType = mob.Define("adamant:hologram",
	mob.NetworkType("minecraft:armor_stand"),
	mob.BBox(cube.Box(0, 0, 0, 0, 0, 0)),
	mob.OnMeta(func(_ world.Entity, patch *world.ActorMetaPatch) {
		inv := true
		always := true
		noAI := true
		scale := float32(0.001)
		zero := float32(0)
		patch.Invisible = &inv
		patch.AlwaysShowNameTag = &always
		patch.NoAI = &noAI
		patch.Scale = &scale
		patch.Width = &zero
		patch.Height = &zero
	}),
)

// Hologram manages a vertical stack of invisible nametag entities rendering
// floating text in a world. Call mutating methods inside a world transaction.
type Hologram struct {
	pos     mgl64.Vec3
	spacing float64
	lines   []string
	handles []*world.EntityHandle
}

// NewHologram creates an unspawned Hologram positioned at pos with lines
// (top to bottom). Configure with Spacing and spawn into a world via Spawn.
func NewHologram(pos mgl64.Vec3, lines ...string) *Hologram {
	return &Hologram{
		pos:     pos,
		spacing: defaultHologramSpacing,
		lines:   slices.Clone(lines),
	}
}

// Spacing sets the vertical distance in blocks between lines.
func (h *Hologram) Spacing(blocks float64) *Hologram {
	if blocks > 0 {
		h.spacing = blocks
	}
	return h
}

// Spawn spawns the hologram's line entities into tx and returns h.
func (h *Hologram) Spawn(tx *world.Tx) *Hologram {
	if h != nil && tx != nil && !tx.Closed() {
		h.SetLines(tx, h.lines...)
	}
	return h
}

// SpawnHologram constructs and immediately spawns a multi-line hologram at pos in tx.
func SpawnHologram(tx *world.Tx, pos mgl64.Vec3, lines ...string) *Hologram {
	return NewHologram(pos, lines...).Spawn(tx)
}

// Position returns the anchor position of the bottom line.
func (h *Hologram) Position() mgl64.Vec3 {
	if h == nil {
		return mgl64.Vec3{}
	}
	return h.pos
}

// Lines returns a copy of the hologram's lines (top to bottom).
func (h *Hologram) Lines() []string {
	if h == nil {
		return nil
	}
	return slices.Clone(h.lines)
}

// Handles returns a copy of the underlying entity handles for each line.
func (h *Hologram) Handles() []*world.EntityHandle {
	if h == nil {
		return nil
	}
	return slices.Clone(h.handles)
}

func (h *Hologram) linePosition(index, total int) mgl64.Vec3 {
	offsetY := float64(total-1-index) * h.spacing
	return mgl64.Vec3{h.pos[0], h.pos[1] + offsetY, h.pos[2]}
}

// SetLines updates the displayed lines (top to bottom), reusing existing
// entities where possible and spawning/closing any difference.
func (h *Hologram) SetLines(tx *world.Tx, lines ...string) {
	if h == nil || tx == nil || tx.Closed() {
		return
	}
	total := len(lines)
	for i := total; i < len(h.handles); i++ {
		if ent, ok := h.handles[i].Entity(tx); ok {
			tx.RemoveEntity(ent)
		}
		_ = h.handles[i].Close()
	}
	if len(h.handles) > total {
		h.handles = h.handles[:total]
	}
	for i, text := range lines {
		pos := h.linePosition(i, total)
		if i < len(h.handles) {
			if ent, ok := h.handles[i].Entity(tx); ok {
				if ce, ok := ent.(*mob.Entity); ok {
					ce.SetNameTag(text)
					ce.Teleport(pos)
					continue
				}
			}
		}
		handle := hologramEntityType.Spawn(world.EntitySpawnOpts{
			Position: pos,
			NameTag:  text,
		})
		ent := tx.AddEntity(handle)
		if i < len(h.handles) {
			h.handles[i] = ent.H()
		} else {
			h.handles = append(h.handles, ent.H())
		}
	}
	h.lines = slices.Clone(lines)
}

// Move moves the entire hologram stack to pos in tx.
func (h *Hologram) Move(tx *world.Tx, pos mgl64.Vec3) {
	if h == nil || tx == nil || tx.Closed() {
		return
	}
	h.pos = pos
	total := len(h.handles)
	for i, handle := range h.handles {
		if ent, ok := handle.Entity(tx); ok {
			if ce, ok := ent.(*mob.Entity); ok {
				ce.Teleport(h.linePosition(i, total))
			}
		}
	}
}

// Close removes all hologram line entities from tx.
func (h *Hologram) Close(tx *world.Tx) {
	if h == nil {
		return
	}
	for _, handle := range h.handles {
		if tx != nil && !tx.Closed() {
			if ent, ok := handle.Entity(tx); ok {
				tx.RemoveEntity(ent)
			}
		}
		_ = handle.Close()
	}
	h.handles = nil
	h.lines = nil
}
