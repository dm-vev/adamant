package session_test

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/bossbar"
	"github.com/df-mc/dragonfly/server/player/chat"
	"github.com/df-mc/dragonfly/server/player/dialogue"
	"github.com/df-mc/dragonfly/server/player/form"
	"github.com/df-mc/dragonfly/server/player/hud"
	"github.com/df-mc/dragonfly/server/player/scoreboard"
	"github.com/df-mc/dragonfly/server/player/title"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/text"
	"github.com/go-gl/mathgl/mgl64"
	"golang.org/x/text/language"
)

// goldenTranslation is a translation of the game with two parameters, in a
// format of colour tags.
var goldenTranslation = chat.Translate(goldenTranslationString("%commands.give.success"), 3, "Gave %v * %v to %v").Enc("<diamond>%v</diamond>")

type goldenTranslationString string

func (s goldenTranslationString) Resolve(language.Tag) string { return string(s) }

// goldenMenu is the menu form of the text scenario.
type goldenMenu struct{}

func (goldenMenu) Submit(form.Submitter, form.Button, *world.Tx) {}

// goldenModal is the modal form of the text scenario.
type goldenModal struct {
	Yes, No form.Button
}

func (goldenModal) Submit(form.Submitter, form.Button, *world.Tx) {}

// goldenCustom is the custom form of the text scenario.
type goldenCustom struct {
	Name   form.Input
	Agree  form.Toggle
	Amount form.Slider
}

func (goldenCustom) Submit(form.Submitter, *world.Tx) {}

// goldenDialogue is the dialogue of the text scenario.
type goldenDialogue struct {
	Yes dialogue.Button
}

func (goldenDialogue) Submit(dialogue.Submitter, dialogue.Button, *world.Tx) {}

// TestGoldenText records the text and HUD a client is sent: messages,
// translations, tips, popups, toasts, titles, boss bars, scoreboards, name
// and score tags of another entity, command output, forms, a dialogue and
// HUD elements, in colour tags of text.Colourf and in raw § codes,
// including the material colours of Bedrock (§s, §m, §n and the others).
func TestGoldenText(t *testing.T) {
	s := newScenario(t, "text", scenarioConfig{})

	s.step("message: plain, colour tags, raw codes", func(_ *world.Tx, p *player.Player) {
		p.Message("plain message")
		p.Message(text.Colourf("<red>red</red> <bold>bold <italic>both</italic></bold> <diamond>diamond</diamond> <redstone>redstone</redstone> <copper>copper</copper>"))
		p.Message("§sdiamond §mredstone §ncopper §hquartz §iiron §jnetherite §pgold §qemerald §tlapis §uamethyst §vresin §gdark yellow §kobfuscated§r §lbold§r §oitalic")
		p.Messagef("formatted %d %s %v", 42, "§anamed", true)
		p.Message("")
		p.Message("multi\nline\n§cred line")
	})
	s.step("translations", func(_ *world.Tx, p *player.Player) {
		p.Messaget(chat.MessageJoin, "§bGolden")
		p.Messaget(chat.MessageBedTooFar)
		p.Messaget(goldenTranslation, "§sdiamond", 3, chat.MessageServerDisconnect)
	})
	s.step("tip, popup, jukebox popup, toast", func(_ *world.Tx, p *player.Player) {
		p.SendTip(text.Colourf("<emerald>tip</emerald> §ltext"))
		p.SendPopup(text.Colourf("<gold>popup</gold> §mredstone"))
		p.SendJukeboxPopup("§nNow playing: copper")
		p.SendToast(text.Colourf("<amethyst>Toast</amethyst>"), "§ttoast message")
	})
	s.step("title, subtitle and action bar with durations", func(_ *world.Tx, p *player.Player) {
		p.SendTitle(title.New(text.Colourf("<lapis>Title</lapis>")).
			WithSubtitle("§qsubtitle").
			WithActionText(text.Colourf("<netherite>action</netherite>")).
			WithFadeInDuration(500 * time.Millisecond).
			WithDuration(3 * time.Second).
			WithFadeOutDuration(time.Second))
		p.SendTitle(title.New().WithActionText("§sonly an action bar"))
	})
	s.step("boss bar added", func(_ *world.Tx, p *player.Player) {
		p.SendBossBar(bossbar.New(text.Colourf("<red>Boss</red> §mredstone")).WithHealthPercentage(0.5).WithColour(bossbar.Red()))
	})
	s.step("boss bar updated", func(_ *world.Tx, p *player.Player) {
		p.SendBossBar(bossbar.New(text.Colourf("<red>Boss</red> §mredstone")).WithHealthPercentage(0.25).WithColour(bossbar.Purple()))
	})
	s.step("second boss bar by ID", func(_ *world.Tx, p *player.Player) {
		p.SendBossBar(bossbar.New("§nsecond").WithID("second").WithColour(bossbar.Green()))
	})
	s.step("boss bars removed", func(_ *world.Tx, p *player.Player) {
		p.RemoveBossBarID("second")
		p.RemoveBossBar()
	})
	s.step("scoreboard set", func(_ *world.Tx, p *player.Player) {
		sb := scoreboard.New(text.Colourf("<amethyst>Board</amethyst>"))
		sb.Set(0, "§ncopper line")
		sb.Set(1, text.Colourf("<diamond>diamond</diamond> line"))
		sb.Set(3, "§kobfuscated")
		p.SendScoreboard(sb)
	})
	s.step("scoreboard changed", func(_ *world.Tx, p *player.Player) {
		sb := scoreboard.New(text.Colourf("<amethyst>Board</amethyst>"))
		sb.Set(0, "§ncopper line")
		sb.Set(1, "§schanged")
		sb.SetDescending()
		p.SendScoreboard(sb)
	})
	s.step("scoreboard removed", func(_ *world.Tx, p *player.Player) {
		p.RemoveScoreboard()
	})

	var other *world.EntityHandle
	s.step("another entity spawns", func(tx *world.Tx, p *player.Player) {
		other = entity.NewText(text.Colourf("<gold>floating</gold> §ntext"), p.Position().Add(mgl64.Vec3{2, 1, 0}))
		tx.AddEntity(other)
	})
	s.step("its name tag changes", func(tx *world.Tx, _ *player.Player) {
		e, _ := other.Entity(tx)
		e.(*entity.Ent).SetNameTag("§mrenamed")
	})
	s.step("name tag and score tag of it for this player", func(tx *world.Tx, p *player.Player) {
		e, _ := other.Entity(tx)
		p.ViewNameTag(e, text.Colourf("<iron>own name</iron>"))
		p.ViewScoreTag(e, "§u10 points")
		p.ViewAlwaysShowNameTag(e, false)
	})
	s.step("public name tag and score tag again", func(tx *world.Tx, p *player.Player) {
		e, _ := other.Entity(tx)
		p.ViewPublicNameTag(e)
		p.ViewPublicScoreTag(e)
		p.ViewPublicAlwaysShowNameTag(e)
	})
	s.step("score tag of the player", func(_ *world.Tx, p *player.Player) {
		p.SetScoreTag("§s", 12, " hearts")
		p.SetNameTag(text.Colourf("<green>Golden</green>"))
	})

	s.step("command output", func(_ *world.Tx, p *player.Player) {
		o := &cmd.Output{}
		o.Print("§aplain output")
		o.Printf("formatted %d", 7)
		o.Printt(goldenTranslation, "a", "b", "c")
		o.Errorf("§cerror %s", "output")
		p.SendCommandOutput(o)
	})
	s.step("menu form", func(_ *world.Tx, p *player.Player) {
		p.SendForm(form.NewMenu(goldenMenu{}, text.Colourf("<diamond>Menu</diamond>")).
			WithBody("§mbody").
			WithButtons(form.NewButton("§nfirst", ""), form.NewButton("second", "textures/items/apple")))
	})
	s.step("modal form", func(_ *world.Tx, p *player.Player) {
		p.SendForm(form.NewModal(goldenModal{Yes: form.NewButton("§ayes", ""), No: form.NewButton("§cno", "")}, "Modal").WithBody("§squestion?"))
	})
	s.step("custom form", func(_ *world.Tx, p *player.Player) {
		p.SendForm(form.New(goldenCustom{
			Name:   form.NewInput("§lName", "Golden", "your name"),
			Agree:  form.NewToggle("Agree", true),
			Amount: form.NewSlider("Amount", 0, 10, 0.5, 2.5),
		}, "§uCustom"))
	})
	s.step("dialogue of the other entity", func(tx *world.Tx, p *player.Player) {
		e, _ := other.Entity(tx)
		p.SendDialogue(dialogue.New(goldenDialogue{Yes: dialogue.Button{Text: "§ayes"}}, "§pDialogue").WithBody(text.Colourf("<emerald>body</emerald>")), e)
	})
	s.step("dialogue closed", func(_ *world.Tx, p *player.Player) {
		p.CloseDialogue()
	})

	s.step("HUD: hide health and hunger", func(_ *world.Tx, p *player.Player) {
		p.HideHudElement(hud.Health())
		p.HideHudElement(hud.Hunger())
	})
	s.tick("HUD sent in the tick", 1)
	s.step("HUD: show health", func(_ *world.Tx, p *player.Player) {
		p.ShowHudElement(hud.Health())
	})
	s.tick("HUD sent in the tick", 1)
	s.snapshot()
}
