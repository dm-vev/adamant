package builtin

import (
	"embed"
	"fmt"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/internal/i18n"
	"github.com/df-mc/dragonfly/server/player/chat"
	"golang.org/x/text/language"
)

//go:embed locales/*.json
var descriptionFiles embed.FS

var descriptions = loadDescriptions()

func loadDescriptions() *i18n.Catalog {
	catalog, err := i18n.Load(descriptionFiles, "locales", language.English)
	if err != nil {
		panic(err)
	}
	return catalog
}

// newCommand gets its console fallback from the English catalog and supplies
// either the native client translation key or a server catalog message.
func newCommand(name string, aliases []string, r ...cmd.Runnable) cmd.Command {
	key := "commands." + name + ".description"
	english, ok := descriptions.Lookup(language.English, key)
	if !ok {
		panic(fmt.Sprintf("missing command description %q in locales/en.json", key))
	}
	translated := commandDescription{key: key, nativeKey: nativeCommandDescriptions[name]}
	return cmd.New(name, english, aliases, r...).WithDescriptionTranslation(chat.Translate(translated, 0, english))
}

type commandDescription struct {
	key, nativeKey string
}

func (d commandDescription) Resolve(locale language.Tag) string {
	if d.nativeKey != "" {
		return "%" + d.nativeKey
	}
	message, _ := descriptions.Lookup(locale, d.key)
	return message
}

// Keys come from the BDS 1.26.51 AvailableCommands capture and the vanilla
// language resources. Canonical Adamant names may differ from Bedrock names.
var nativeCommandDescriptions = map[string]string{
	"ban":             "commands.ban.description",
	"ban-ip":          "commands.banip.description",
	"bossbar":         "commands.bossbar.description",
	"clear":           "commands.clear.description",
	"clone":           "commands.clone.description",
	"damage":          "commands.damage.description",
	"defaultgamemode": "commands.defaultgamemode.description",
	"deop":            "commands.deop.description",
	"difficulty":      "commands.difficulty.description",
	"effect":          "commands.effect.description",
	"enchant":         "commands.enchant.description",
	"execute":         "commands.execute.description",
	"experience":      "commands.xp.description",
	"fill":            "commands.fill.description",
	"gamemode":        "commands.gamemode.description",
	"gamerule":        "commands.gamerule.description",
	"give":            "commands.give.description",
	"help":            "commands.help.description",
	"kick":            "commands.kick.description",
	"kill":            "commands.kill.description",
	"list":            "commands.list.description",
	"locate":          "commands.locate.description",
	"loot":            "commands.loot.description",
	"me":              "commands.me.description",
	"msg":             "commands.tell.description",
	"op":              "commands.op.description",
	"particle":        "commands.spawnParticleEmitter.description",
	"place":           "commands.place.description",
	"playsound":       "commands.playsound.description",
	"recipe":          "commands.recipes.description",
	"ride":            "commands.ride.description",
	"save-on":         "commands.save-on.description",
	"say":             "commands.say.description",
	"scoreboard":      "commands.scoreboard.description",
	"setblock":        "commands.setblock.description",
	"setworldspawn":   "commands.setworldspawn.description",
	"spawnpoint":      "commands.spawnpoint.description",
	"spreadplayers":   "commands.spreadplayers.description",
	"stop":            "commands.stop.description",
	"stopsound":       "commands.stopsound.description",
	"summon":          "commands.summon.description",
	"tag":             "commands.tag.description",
	"tellraw":         "commands.tellraw.description",
	"time":            "commands.time.description",
	"title":           "commands.title.description",
	"tp":              "commands.tp.description",
	"transfer":        "commands.transferserver.description",
	"trigger":         "commands.trigger.description",
	"weather":         "commands.weather.description",
	"whitelist":       "commands.allowlist.description",
}
