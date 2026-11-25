package api

import (
	"slices"
	"time"

	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/bossbar"
	"github.com/df-mc/dragonfly/server/player/dialogue"
	"github.com/df-mc/dragonfly/server/player/scoreboard"
	"github.com/df-mc/dragonfly/server/player/title"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// ScoreboardSpec declaratively configures a player sidebar scoreboard.
type ScoreboardSpec struct {
	Title      string
	Lines      []string
	Descending bool
	NoPadding  bool
}

// NewScoreboard builds a *scoreboard.Scoreboard from spec.
func NewScoreboard(spec ScoreboardSpec) *scoreboard.Scoreboard {
	sb := scoreboard.New(spec.Title)
	if spec.NoPadding {
		sb.RemovePadding()
	}
	if spec.Descending {
		sb.SetDescending()
	}
	for i, line := range spec.Lines {
		sb.Set(i, line)
	}
	return sb
}

// SendScoreboard displays a declarative sidebar scoreboard to p.
func SendScoreboard(p *player.Player, spec ScoreboardSpec) {
	if p == nil {
		return
	}
	p.SendScoreboard(NewScoreboard(spec))
}

// RemoveScoreboard clears the sidebar scoreboard shown to p.
func RemoveScoreboard(p *player.Player) {
	if p == nil {
		return
	}
	p.RemoveScoreboard()
}

// BossBarSpec declaratively configures a Bedrock boss bar.
type BossBarSpec struct {
	// ID distinguishes simultaneous boss bars (defaults to "default").
	ID     string
	Text   string
	Health float64
	// SetHealth differentiates an explicit 0.0 health bar from the default 1.0.
	SetHealth bool
	Colour    bossbar.Colour
	HasColour bool
}

// NewBossBar constructs a bossbar.BossBar from spec.
func NewBossBar(spec BossBarSpec) bossbar.BossBar {
	bar := bossbar.New(spec.Text)
	if spec.ID != "" {
		bar = bar.WithID(spec.ID)
	}
	if spec.SetHealth || spec.Health > 0 {
		h := max(0.0, min(1.0, spec.Health))
		bar = bar.WithHealthPercentage(h)
	}
	if spec.HasColour {
		bar = bar.WithColour(spec.Colour)
	}
	return bar
}

// SendBossBar displays a declarative boss bar to p.
func SendBossBar(p *player.Player, spec BossBarSpec) {
	if p == nil {
		return
	}
	p.SendBossBar(NewBossBar(spec))
}

// RemoveBossBar removes the default boss bar from p.
func RemoveBossBar(p *player.Player) {
	if p == nil {
		return
	}
	p.RemoveBossBar()
}

// RemoveBossBarID removes the boss bar with id from p.
func RemoveBossBarID(p *player.Player, id string) {
	if p == nil || id == "" {
		return
	}
	p.RemoveBossBarID(id)
}

// TitleSpec declaratively configures a screen title, subtitle, and action text.
type TitleSpec struct {
	Title      string
	Subtitle   string
	ActionText string
	FadeIn     time.Duration
	Duration   time.Duration
	FadeOut    time.Duration
}

// NewTitle constructs a title.Title from spec.
func NewTitle(spec TitleSpec) title.Title {
	t := title.New(spec.Title)
	if spec.Subtitle != "" {
		t = t.WithSubtitle(spec.Subtitle)
	}
	if spec.ActionText != "" {
		t = t.WithActionText(spec.ActionText)
	}
	if spec.FadeIn > 0 {
		t = t.WithFadeInDuration(spec.FadeIn)
	}
	if spec.Duration > 0 {
		t = t.WithDuration(spec.Duration)
	}
	if spec.FadeOut > 0 {
		t = t.WithFadeOutDuration(spec.FadeOut)
	}
	return t
}

// SendTitle displays a screen title to p.
func SendTitle(p *player.Player, spec TitleSpec) {
	if p == nil {
		return
	}
	p.SendTitle(NewTitle(spec))
}

// SendActionBar displays text above the hotbar (Bedrock jukebox/actionbar slot).
func SendActionBar(p *player.Player, text string) {
	if p == nil {
		return
	}
	p.SendJukeboxPopup(text)
}

// SendPopup displays popup text above the hotbar to p.
func SendPopup(p *player.Player, text string) {
	if p == nil {
		return
	}
	p.SendPopup(text)
}

// SendTip displays tip text above the hotbar to p.
func SendTip(p *player.Player, text string) {
	if p == nil {
		return
	}
	p.SendTip(text)
}

// SendToast displays a top-screen Bedrock toast notification to p.
func SendToast(p *player.Player, heading, message string) {
	if p == nil {
		return
	}
	p.SendToast(heading, message)
}

// BroadcastMessage sends a chat message to every online player on app.
// Do not call from inside a world owner transaction; use BroadcastWorldMessage instead.
func BroadcastMessage(app *App, msg ...any) {
	if app == nil {
		return
	}
	for p := range app.Players() {
		p.Message(msg...)
	}
}

// BroadcastWorldMessage sends a chat message to every online player in tx's world.
func BroadcastWorldMessage(app *App, tx *world.Tx, msg ...any) {
	if app == nil || tx == nil || tx.Closed() {
		return
	}
	for p := range app.PlayersIn(tx) {
		p.Message(msg...)
	}
}

// PlaySound plays s exclusively for player p.
func PlaySound(p *player.Player, s world.Sound) {
	if p == nil || s == nil {
		return
	}
	p.PlaySound(s)
}

// PlaySoundAt plays s in tx at pos for all nearby viewers.
func PlaySoundAt(tx *world.Tx, pos mgl64.Vec3, s world.Sound) {
	if tx == nil || tx.Closed() || s == nil {
		return
	}
	tx.PlaySound(pos, s)
}

// PlayCustomSound plays a named/resource-pack sound exclusively for player p.
func PlayCustomSound(p *player.Player, name string, volume, pitch float64) {
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

// PlayCustomSoundAt plays a named/resource-pack sound in tx at pos.
func PlayCustomSoundAt(tx *world.Tx, pos mgl64.Vec3, name string, volume, pitch float64) {
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

// SpawnParticle spawns particle pt in tx at pos for all viewers of that chunk.
func SpawnParticle(tx *world.Tx, pos mgl64.Vec3, pt world.Particle) {
	if tx == nil || tx.Closed() || pt == nil {
		return
	}
	tx.AddParticle(pos, pt)
}

// DialogueButton configures one button inside DialogueSpec (up to 6 buttons).
type DialogueButton struct {
	Text    string
	OnClick func(p *player.Player, tx *world.Tx)
}

// DialogueSpec declaratively configures an NPC dialogue window.
type DialogueSpec struct {
	Title   string
	Body    string
	Display dialogue.DisplaySettings
	Buttons []DialogueButton
	OnClose func(p *player.Player, tx *world.Tx)
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

// NewDialogue builds a dialogue.Dialogue from spec with closure button handlers.
func NewDialogue(spec DialogueSpec) dialogue.Dialogue {
	btns := make([]dialogue.Button, len(spec.Buttons))
	actions := make([]func(*player.Player, *world.Tx), len(spec.Buttons))
	for i, b := range spec.Buttons {
		btns[i] = dialogue.Button{Text: b.Text}
		actions[i] = b.OnClick
	}
	d := dialogue.New(dynamicDialogue{
		buttons: slices.Clone(btns),
		actions: actions,
		onClose: spec.OnClose,
	}, spec.Title).WithBody(spec.Body).WithDisplay(spec.Display).WithButtons(btns...)
	return d
}

// SendDialogue displays a declarative NPC dialogue attached to entity e to p.
func SendDialogue(p *player.Player, e world.Entity, spec DialogueSpec) {
	if p == nil || e == nil {
		return
	}
	p.SendDialogue(NewDialogue(spec), e)
}
