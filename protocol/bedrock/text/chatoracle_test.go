package bedrocktext

import (
	"fmt"
	"strings"

	"github.com/df-mc/dragonfly/server/player/chat"
	nettext "github.com/dm-vev/adamant/net/bedrock/text"
	"golang.org/x/text/language"
)

// This file is the oracle of the tests of the translations of the chat
// package (chat_test.go): the translations as they were before they were
// built on structured text, frozen, so that the tests hold the new ones to
// the messages, parameters and fallbacks the old ones gave Bedrock clients
// and the console. It is the code of server/player/chat/translate.go and
// of the mob names of server/player/death_message.go as they were after
// the two fixes of the console that came just before ("Show translations
// passed as arguments by their text on the console" and "Name mobs in
// death messages on the console"), with its types renamed, its comments
// left out and Colourf taken from net/bedrock/text, as it was then. It is
// not to be changed: old* is the old code as it was, quirks and all, such
// as the cases of Params for a Translation and a filled in translation,
// which a TranslationString, the case before them, takes, as both resolve.

func oldTranslate(str chat.TranslationString, params int, fallback string) oldTranslation {
	return oldTranslation{str: str, params: params, fallback: fallback, format: "%v"}
}

type oldTranslation struct {
	str      chat.TranslationString
	format   string
	params   int
	fallback string
}

func (t oldTranslation) Zero() bool {
	return t.format == ""
}

func (t oldTranslation) Enc(format string) oldTranslation {
	t.format = format
	return t
}

func (t oldTranslation) Resolve(l language.Tag) string {
	return t.F().Resolve(l)
}

func (t oldTranslation) F(a ...any) oldFilled {
	if len(a) != t.params {
		panic(fmt.Sprintf("translation '%v' requires exactly %v parameters, got %v", t.format, t.params, len(a)))
	}
	return oldFilled{t: t, params: a}
}

// oldFilled is the unexported type of a filled in translation, renamed.
type oldFilled struct {
	t      oldTranslation
	params []any
}

func (t oldFilled) Resolve(l language.Tag) string {
	return nettext.Colourf(t.t.format, t.t.str.Resolve(l))
}

func (t oldFilled) Params(l language.Tag) []string {
	params := make([]string, len(t.params))
	for i, arg := range t.params {
		switch v := arg.(type) {
		case chat.TranslationString:
			params[i] = v.Resolve(l)
		case oldTranslation:
			if v.params == 0 {
				params[i] = v.Resolve(l)
			} else {
				params[i] = v.str.Resolve(l)
			}
		case oldFilled:
			if len(v.params) == 0 {
				params[i] = v.Resolve(l)
			} else {
				params[i] = v.String()
			}
		default:
			params[i] = fmt.Sprint(arg)
		}
	}
	return params
}

func (t oldFilled) String() string {
	formatted := nettext.Colourf(t.t.format, t.t.fallback)
	if len(t.params) == 0 {
		return formatted
	}
	return fmt.Sprintf(formatted, oldFallbackArgs(t.params)...)
}

func oldFallbackArgs(params []any) []any {
	args := make([]any, len(params))
	for i, a := range params {
		switch v := a.(type) {
		case oldTranslation:
			if v.params == 0 {
				args[i] = v.F().String()
				continue
			}
		case oldFilled:
			args[i] = v.String()
			continue
		}
		args[i] = a
	}
	return args
}

func (t oldFilled) Error() string {
	return t.String()
}

type oldMobName string

func (n oldMobName) Resolve(language.Tag) string { return "%entity." + string(n) + ".name" }

func (n oldMobName) String() string {
	words := strings.Split(string(n), "_")
	for i, w := range words {
		if w != "" {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

// oldStr is the type of the keys of the translations of the chat and cmd
// packages, and, with the % the death messages put before their keys, of
// the death messages.
type oldStr string

func (s oldStr) Resolve(language.Tag) string { return string(s) }

// oldRepoTranslations are the translations of the repository as they were
// defined: those of the chat and cmd packages by the name of their
// variable, and the death messages of the player package by "player." and
// their key.
var oldRepoTranslations = map[string]oldTranslation{
	"chat.MessageJoin":                     oldTranslate(oldStr("%multiplayer.player.joined"), 1, `%v joined the game`).Enc("<yellow>%v</yellow>"),
	"chat.MessageQuit":                     oldTranslate(oldStr("%multiplayer.player.left"), 1, `%v left the game`).Enc("<yellow>%v</yellow>"),
	"chat.MessageServerDisconnect":         oldTranslate(oldStr("%disconnect.disconnected"), 0, `Disconnected by Server`).Enc("<yellow>%v</yellow>"),
	"chat.MessageBedTooFar":                oldTranslate(oldStr("%tile.bed.tooFar"), 0, `Bed is too far away`).Enc("<grey>%v</grey>"),
	"chat.MessageBedObstructed":            oldTranslate(oldStr("%tile.bed.obstructed"), 0, `Bed is obstructed`).Enc("<grey>%v</grey>"),
	"chat.MessageRespawnPointSet":          oldTranslate(oldStr("%tile.bed.respawnSet"), 0, `Respawn point set`).Enc("<grey>%v</grey>"),
	"chat.MessageNoSleep":                  oldTranslate(oldStr("%tile.bed.noSleep"), 0, `You can only sleep at night and during thunderstorms`).Enc("<grey>%v</grey>"),
	"chat.MessageNoSleepMonsters":          oldTranslate(oldStr("%tile.bed.notSafe"), 0, `You may not rest now; there are monsters nearby`).Enc("<grey>%v</grey>"),
	"chat.MessageBedIsOccupied":            oldTranslate(oldStr("%tile.bed.occupied"), 0, `This bed is occupied`).Enc("<grey>%v</grey>"),
	"chat.MessageSleeping":                 oldTranslate(oldStr("%chat.type.sleeping"), 2, `%v is sleeping in a bed. To skip to dawn, %v more users need to sleep in beds at the same time.`),
	"chat.MessageRespawnAnchorNotValid":    oldTranslate(oldStr("%tile.respawn_anchor.notValid"), 0, `Your respawn anchor was out of charges, missing or obstructed`).Enc("<grey>%v</grey>"),
	"chat.MessageBedNotValid":              oldTranslate(oldStr("%tile.bed.notValid"), 0, `Your home bed was missing or obstructed`),
	"chat.MessageAdminCommand":             oldTranslate(oldStr("%chat.type.admin"), 2, `[%v: %v]`).Enc("<grey><i>%v</i></grey>"),
	"cmd.MessageSyntax":                    oldTranslate(oldStr("%commands.generic.syntax"), 3, `Syntax error: unexpected value: at "%v>>%v<<%v"`).Enc("<red>%v</red>"),
	"cmd.MessageUsage":                     oldTranslate(oldStr("%commands.generic.usage"), 1, `Usage: %v`).Enc("<red>%v</red>"),
	"cmd.MessageUnknown":                   oldTranslate(oldStr("%commands.generic.unknown"), 1, `Unknown command: "%v": Please check that the command exists and that you have permission to use it.`).Enc("<red>%v</red>"),
	"cmd.MessageNoTargets":                 oldTranslate(oldStr("%commands.generic.noTargetMatch"), 0, `No targets matched selector`).Enc("<red>%v</red>"),
	"cmd.MessageNumberInvalid":             oldTranslate(oldStr("%commands.generic.num.invalid"), 1, `'%v' is not a valid number`).Enc("<red>> %v</red>"),
	"cmd.MessageBooleanInvalid":            oldTranslate(oldStr("%commands.generic.boolean.invalid"), 1, `'%v' is not true or false`).Enc("<red>> %v</red>"),
	"cmd.MessagePlayerNotFound":            oldTranslate(oldStr("%commands.generic.player.notFound"), 0, `That player cannot be found`).Enc("<red>> %v</red>"),
	"cmd.MessageParameterInvalid":          oldTranslate(oldStr("%commands.generic.parameter.invalid"), 1, `'%v' is not a valid parameter`).Enc("<red>> %v</red>"),
	"player.death.attack.generic":          oldTranslate(oldStr("%death.attack.generic"), 1, "%v died"),
	"player.death.attack.player":           oldTranslate(oldStr("%death.attack.player"), 2, "%v was slain by %v"),
	"player.death.attack.mob":              oldTranslate(oldStr("%death.attack.mob"), 2, "%v was slain by %v"),
	"player.death.attack.arrow":            oldTranslate(oldStr("%death.attack.arrow"), 2, "%v was shot by %v"),
	"player.death.attack.trident":          oldTranslate(oldStr("%death.attack.trident"), 2, "%v was impaled to death by %v"),
	"player.death.attack.fireball":         oldTranslate(oldStr("%death.attack.fireball"), 2, "%v was fireballed by %v"),
	"player.death.attack.thrown":           oldTranslate(oldStr("%death.attack.thrown"), 2, "%v was pummeled by %v"),
	"player.death.attack.thorns":           oldTranslate(oldStr("%death.attack.thorns"), 2, "%v was killed trying to hurt %v"),
	"player.death.attack.fall":             oldTranslate(oldStr("%death.attack.fall"), 1, "%v hit the ground too hard"),
	"player.death.attack.inFire":           oldTranslate(oldStr("%death.attack.inFire"), 1, "%v went up in flames"),
	"player.death.attack.onFire":           oldTranslate(oldStr("%death.attack.onFire"), 1, "%v burned to death"),
	"player.death.attack.lava":             oldTranslate(oldStr("%death.attack.lava"), 1, "%v tried to swim in lava"),
	"player.death.attack.magma":            oldTranslate(oldStr("%death.attack.magma"), 1, "%v discovered the floor was lava"),
	"player.death.attack.inWall":           oldTranslate(oldStr("%death.attack.inWall"), 1, "%v suffocated in a wall"),
	"player.death.attack.drown":            oldTranslate(oldStr("%death.attack.drown"), 1, "%v drowned"),
	"player.death.attack.outOfWorld":       oldTranslate(oldStr("%death.attack.outOfWorld"), 1, "%v fell out of the world"),
	"player.death.attack.explosion":        oldTranslate(oldStr("%death.attack.explosion"), 1, "%v blew up"),
	"player.death.attack.explosion.player": oldTranslate(oldStr("%death.attack.explosion.player"), 2, "%v was blown up by %v"),
	"player.death.attack.fireworks":        oldTranslate(oldStr("%death.attack.fireworks"), 1, "%v went off with a bang"),
	"player.death.attack.magic":            oldTranslate(oldStr("%death.attack.magic"), 1, "%v was killed by magic"),
	"player.death.attack.wither":           oldTranslate(oldStr("%death.attack.wither"), 1, "%v withered away"),
	"player.death.attack.lightningBolt":    oldTranslate(oldStr("%death.attack.lightningBolt"), 1, "%v was struck by lightning"),
	"player.death.attack.flyIntoWall":      oldTranslate(oldStr("%death.attack.flyIntoWall"), 1, "%v experienced kinetic energy"),
	"player.death.attack.starve":           oldTranslate(oldStr("%death.attack.starve"), 1, "%v starved to death"),
	"player.death.attack.freeze":           oldTranslate(oldStr("%death.attack.freeze"), 1, "%v froze to death"),
	"player.death.attack.cactus":           oldTranslate(oldStr("%death.attack.cactus"), 1, "%v was pricked to death"),
	"player.death.attack.sweetBerry":       oldTranslate(oldStr("%death.attack.sweetBerry"), 1, "%v was poked to death by a sweet berry bush"),
	"player.death.attack.anvil":            oldTranslate(oldStr("%death.attack.anvil"), 1, "%v was squashed by a falling anvil"),
	"player.death.attack.fallingBlock":     oldTranslate(oldStr("%death.attack.fallingBlock"), 1, "%v was squashed by a falling block"),
	"player.death.attack.stalactite":       oldTranslate(oldStr("%death.attack.stalactite"), 1, "%v was skewered by a falling stalactite"),
	"player.death.attack.stalagmite":       oldTranslate(oldStr("%death.attack.stalagmite"), 1, "%v was impaled on a stalagmite"),
}
