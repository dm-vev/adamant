package player

import (
	"fmt"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/logging"
	"github.com/df-mc/dragonfly/server/player/bossbar"
	"github.com/df-mc/dragonfly/server/player/chat"
	"github.com/df-mc/dragonfly/server/player/debug"
	"github.com/df-mc/dragonfly/server/player/dialogue"
	"github.com/df-mc/dragonfly/server/player/form"
	"github.com/df-mc/dragonfly/server/player/hud"
	"github.com/df-mc/dragonfly/server/player/input"
	"github.com/df-mc/dragonfly/server/player/scoreboard"
	"github.com/df-mc/dragonfly/server/player/title"
	"github.com/df-mc/dragonfly/server/text"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Message sends a formatted message to the player. The message is formatted following the rules of
// fmt.Sprintln, however the newline at the end is not written, and read as a legacy string, with § codes.
// A lone text.Text is sent as it is.
func (p *Player) Message(a ...any) {
	p.client().SendMessage(formatText(a))
}

// Messagef sends a formatted message using a specific format to the player. The message is formatted
// according to the fmt.Sprintf formatting rules and read as a legacy string, with § codes.
func (p *Player) Messagef(f string, a ...any) {
	p.client().SendMessage(text.ParseLegacy(fmt.Sprintf(f, a...)))
}

// Messaget sends a translatable message to a player and parameterises it using
// the arguments passed, in the language of the player. Messaget panics if an
// incorrect amount of arguments is passed.
func (p *Player) Messaget(t chat.Translation, a ...any) {
	p.client().SendMessage(t.F(a...).Text(p.locale))
}

// SendPopup sends a formatted popup to the player. The popup is shown above the hotbar of the player and
// overwrites/is overwritten by the name of the item equipped.
// The popup is formatted following the rules of fmt.Sprintln without a newline at the end and read as a
// legacy string, with § codes; a lone text.Text is sent as it is.
func (p *Player) SendPopup(a ...any) {
	p.client().SendPopup(formatText(a))
}

// SendTip sends a tip to the player. The tip is shown in the middle of the screen of the player.
// The tip is formatted following the rules of fmt.Sprintln without a newline at the end and read as a legacy
// string, with § codes; a lone text.Text is sent as it is.
func (p *Player) SendTip(a ...any) {
	p.client().SendTip(formatText(a))
}

// SendJukeboxPopup sends a formatted jukebox popup to the player. This popup is shown above the hotbar of the player.
// The popup is close to the position of an action bar message and the text has no background. It is formatted
// as SendPopup formats it.
func (p *Player) SendJukeboxPopup(a ...any) {
	p.client().SendJukeboxPopup(formatText(a))
}

// SendToast sends a toast to the player. This toast is shown at the top of the screen, similar to achievements or pack
// loading. The title and the message are each a text.Text, taken as it is, or a string or another value, formatted
// by fmt and read as a legacy string, with § codes.
func (p *Player) SendToast(title, message any) {
	p.client().SendToast(textOf(title), textOf(message))
}

// SendTitle sends a title to the player. The title may be configured to change the duration it is displayed
// and the text it shows.
// If non-empty, the subtitle is shown in a smaller font below the title. The same counts for the action text
// of the title, which is shown in a font similar to that of a tip/popup.
func (p *Player) SendTitle(t title.Title) {
	p.client().SetTitleDurations(t.FadeInDuration(), t.Duration(), t.FadeOutDuration())
	if !t.Text().IsZero() || !t.Subtitle().IsZero() {
		p.client().SendTitle(t.Text())
		if !t.Subtitle().IsZero() {
			p.client().SendSubtitle(t.Subtitle())
		}
	}
	if !t.ActionText().IsZero() {
		p.client().SendActionBarMessage(t.ActionText())
	}
}

// SendScoreboard sends a scoreboard to the player. The scoreboard will be present indefinitely until removed
// by the caller.
// SendScoreboard may be called at any time to change the scoreboard of the player.
func (p *Player) SendScoreboard(scoreboard *scoreboard.Scoreboard) {
	p.client().SendScoreboard(scoreboard)
}

// RemoveScoreboard removes any scoreboard currently present on the screen of the player. Nothing happens if
// the player has no scoreboard currently active.
func (p *Player) RemoveScoreboard() {
	p.client().RemoveScoreboard()
}

// SendBossBar sends a boss bar to the player, so that it will be shown indefinitely at the top of the
// player's screen.
// The boss bar may be removed by calling Player.RemoveBossBar().
func (p *Player) SendBossBar(bar bossbar.BossBar) {
	p.client().SendBossBarID(bar.ID(), bar.Text(), bar.Colour(), bar.HealthPercentage())
}

// RemoveBossBar removes any boss bar currently active on the player's screen. If no boss bar is currently
// present, nothing happens.
func (p *Player) RemoveBossBar() {
	p.client().RemoveBossBar()
}

// RemoveBossBarID removes only the boss bar with the identifier passed.
func (p *Player) RemoveBossBarID(id string) {
	p.client().RemoveBossBarID(id)
}

// Chat writes a message in the global chat (chat.Global). The message is prefixed with the name of the
// player and is formatted following the rules of fmt.Sprintln.
func (p *Player) Chat(msg ...any) {
	message := format(msg)
	ctx := NewEventContext(p.tx, p)
	if p.Handler().HandleChat(ctx, &message); ctx.Cancelled() {
		return
	}
	_, _ = fmt.Fprintf(chat.Global, "<%v> %v\n", p.Name(), message)
}

// ExecuteCommand executes a command passed as the player. If the command could not be found, or if the usage
// was incorrect, an error message is sent to the player. This message should start with a "/" for the command to be
// recognised.
func (p *Player) ExecuteCommand(commandLine string) {
	if p.Dead() {
		return
	}
	cmd.ExecuteLine(p, commandLine, p.tx, func(command cmd.Command, args []string) bool {
		ctx := NewEventContext(p.tx, p)
		if p.Handler().HandleCommandExecution(ctx, command, args); ctx.Cancelled() {
			return false
		}
		return true
	})
}

// SendCommandOutput sends the output of a command the player ran. It ports
// CommandSourceStack.sendSuccess: success messages reach the player only
// with send_command_feedback on, and with log_admin_commands on they are
// also shown to other operators as "[Player: message]" and logged.
func (p *Player) SendCommandOutput(output *cmd.Output) {
	if p.tx == nil {
		p.client().SendCommandOutput(client.CommandOutputOf(output, p.locale))
		return
	}
	w := p.tx.World()
	if output.MessageCount() > 0 && w.GameRuleBool("log_admin_commands") {
		for _, msg := range output.Messages() {
			logging.Sub(p.log, logging.Commands).Info(fmt.Sprintf("[%s: %s]", p.Name(), msg))
			if cmd.AdminFeedback(p, p.Name(), msg.String()) {
				// The server shows it to the operators of every world.
				continue
			}
			for e := range p.tx.Players() {
				if other, ok := e.(*Player); ok && other != p && cmd.IsOperator(other.UUID()) {
					other.Messaget(chat.MessageAdminCommand, p.Name(), msg.String())
				}
			}
		}
	}
	if !w.GameRuleBool("send_command_feedback") {
		output = output.WithoutMessages()
	}
	p.client().SendCommandOutput(client.CommandOutputOf(output, p.locale))
}

// SendDialogue sends an NPC dialogue to the player, using the entity passed as the entity that the dialogue
// is shown for. Dialogues can be sent on top of each other without the other closing, making it possible
// to have non-flashing transitions between menus compared to forms. The player can either press one of the
// buttons or close the dialogue. It is impossible for a dialogue to have any more than 6 buttons.
func (p *Player) SendDialogue(d dialogue.Dialogue, e world.Entity) {
	p.client().SendDialogue(d, e)
}

// CloseDialogue closes the player's currently open dialogue, if any. If the dialogue's Submittable implements
// dialogue.Closer, the Close method of the Submittable is called after the client acknowledges the closing
// of the dialogue.
func (p *Player) CloseDialogue() {
	p.client().CloseDialogue()
}

// SendForm sends a form to the player for the client to fill out. Once the client fills it out, the Submit
// method of the form will be called.
// Note that the client may also close the form instead of filling it out, which will result in the form not
// having its Submit method called at all. Forms should never depend on the player actually filling out the
// form.
func (p *Player) SendForm(f form.Form) {
	p.client().SendForm(f)
}

// CloseForm closes any forms that the player currently has open. If the player has no forms open, nothing
// happens.
func (p *Player) CloseForm() {
	p.client().CloseForm()
}

// ShowCoordinates enables the vanilla coordinates for the player.
func (p *Player) ShowCoordinates() {
	p.client().EnableCoordinates(true)
}

// HideCoordinates disables the vanilla coordinates for the player.
func (p *Player) HideCoordinates() {
	p.client().EnableCoordinates(false)
}

// EnableInstantRespawn enables the vanilla instant respawn for the player.
func (p *Player) EnableInstantRespawn() {
	p.client().EnableInstantRespawn(true)
}

// DisableInstantRespawn disables the vanilla instant respawn for the player.
func (p *Player) DisableInstantRespawn() {
	p.client().EnableInstantRespawn(false)
}

// SendSleepingIndicator displays a notification to the player on the amount of sleeping players in the world.
func (p *Player) SendSleepingIndicator(sleeping, max int) {
	p.client().ViewSleepingPlayers(sleeping, max)
}

// WearingCreakingDisguise reports whether the player's helmet is a carved pumpkin.
// ShowElderGuardianCurse shows the player the ghost of an elder guardian,
// as an elder guardian that gives it mining fatigue does.
func (p *Player) ShowElderGuardianCurse() {
	p.client().ViewElderGuardianCurse(p.Position())
}

// PlaySound plays a world.Sound that only this Player can hear. Unlike World.PlaySound, it is not broadcast
// to players around it.
func (p *Player) PlaySound(sound world.Sound) {
	p.client().PlaySound(sound, entity.EyePosition(p))
}

// ShowParticle shows a particle that only this Player can see. Unlike World.AddParticle, it is not broadcast
// to players around it.
func (p *Player) ShowParticle(pos mgl64.Vec3, particle world.Particle) {
	p.client().ViewParticle(pos, particle)
}

// OpenSign makes the player open the sign at the cube.Pos passed, with the specific side provided. The client will not
// show the interface if it is not aware of a sign at the position.
func (p *Player) OpenSign(pos cube.Pos, frontSide bool) {
	p.client().OpenSign(pos, frontSide)
}

// UpdateDiagnostics updates the diagnostics of the player.
func (p *Player) UpdateDiagnostics(d client.Diagnostics) {
	p.Handler().HandleDiagnostics(p, d)
}

// ShowHudElement shows a HUD element to the player if it is not already shown.
func (p *Player) ShowHudElement(e hud.Element) {
	p.client().ShowHudElement(e)
}

// HideHudElement hides a HUD element from the player if it is not already hidden.
func (p *Player) HideHudElement(e hud.Element) {
	p.client().HideHudElement(e)
}

// HudElementHidden checks if a HUD element is currently hidden from the player.
func (p *Player) HudElementHidden(e hud.Element) bool {
	return p.client().HudElementHidden(e)
}

// AddDebugShape adds a debug shape to be rendered to the player. If the shape already exists, it will be
// updated with the new information.
func (p *Player) AddDebugShape(shape debug.Shape) {
	p.client().AddDebugShape(shape)
}

// RemoveDebugShape removes a debug shape from the player by its unique identifier.
func (p *Player) RemoveDebugShape(shape debug.Shape) {
	p.client().RemoveDebugShape(shape)
}

// VisibleDebugShapes returns a slice of all debug shapes that are currently being shown to the player.
func (p *Player) VisibleDebugShapes() []debug.Shape {
	return p.client().VisibleDebugShapes()
}

// RemoveAllDebugShapes removes all rendered debug shapes from the player, as well as any shapes that have
// not yet been rendered.
func (p *Player) RemoveAllDebugShapes() {
	p.client().RemoveAllDebugShapes()
}

// LockInput applies an input lock to the player, disabling the specified input and immediately sending the
// updated lock state to the client.
func (p *Player) LockInput(l input.Lock) {
	p.client().LockInput(l)
	p.client().SendInputLocks()
}

// UnlockInput removes an input lock from the player, re-enabling the specified input and immediately sending
// the updated lock state to the client.
func (p *Player) UnlockInput(l input.Lock) {
	p.client().UnlockInput(l)
	p.client().SendInputLocks()
}

// ClearInputLocks removes all input locks from the player, re-enabling all inputs and immediately sending the
// updated lock state to the client.
func (p *Player) ClearInputLocks() {
	p.client().ClearInputLocks()
	p.client().SendInputLocks()
}

// InputLocked checks if a specific input lock is currently applied to the player.
func (p *Player) InputLocked(l input.Lock) bool {
	return p.client().InputLocked(l)
}

// format is a utility function to format a list of values to have spaces between them, but no newline at the
// end, which is typically used for sending messages, popups and tips.
func format(a []any) string {
	return strings.TrimSuffix(strings.TrimSuffix(fmt.Sprintln(a...), "\n"), "\n")
}

// formatText returns the values a as text: a lone text.Text as it is, any
// other values formatted by format and read as a legacy string, with § codes.
func formatText(a []any) text.Text {
	if len(a) == 1 {
		if t, ok := a[0].(text.Text); ok {
			return t
		}
	}
	return text.ParseLegacy(format(a))
}

// textOf returns the value v as text: a text.Text as it is, a string read as
// a legacy string, with § codes, and any other value formatted by fmt and
// read so.
func textOf(v any) text.Text {
	switch v := v.(type) {
	case text.Text:
		return v
	case string:
		return text.ParseLegacy(v)
	}
	return text.ParseLegacy(fmt.Sprint(v))
}
