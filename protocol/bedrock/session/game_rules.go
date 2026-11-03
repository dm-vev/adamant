package session

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// clientGameRules maps stored gamerules onto the Bedrock gamerule names the
// client shows in its world settings and uses for client-side behaviour such
// as skipping the death screen. Rules synced through dedicated viewer methods
// (advance_time, fall_damage) and natural regeneration, which the server
// always handles itself, are not listed.
var clientGameRules = map[string]string{
	"immediate_respawn":            "doimmediaterespawn",
	"show_death_messages":          "showdeathmessages",
	"keep_inventory":               "keepinventory",
	"pvp":                          "pvp",
	"fire_damage":                  "firedamage",
	"drowning_damage":              "drowningdamage",
	"freeze_damage":                "freezedamage",
	"mob_griefing":                 "mobgriefing",
	"tnt_explodes":                 "tntexplodes",
	"tnt_explosion_drop_decay":     "tntexplosiondropdecay",
	"send_command_feedback":        "sendcommandfeedback",
	"command_block_output":         "commandblockoutput",
	"command_blocks_work":          "commandblocksenabled",
	"players_sleeping_percentage":  "playerssleepingpercentage",
	"respawn_radius":               "spawnradius",
	"projectiles_can_break_blocks": "projectilescanbreakblocks",
	"advance_weather":              "doweathercycle",
	"spawn_mobs":                   "domobspawning",
	"mob_drops":                    "domobloot",
	"block_drops":                  "dotiledrops",
	"entity_drops":                 "doentitydrops",
	"random_tick_speed":            "randomtickspeed",
	"max_command_sequence_length":  "maxcommandchainlength",
	"limited_crafting":             "dolimitedcrafting",
}

// ClientGameRules returns the Bedrock client view of the world's gamerules,
// used in the StartGame packet.
func ClientGameRules(w *world.World) []v2193.GameRule {
	rules := make([]v2193.GameRule, 0, len(clientGameRules))
	for name, client := range clientGameRules {
		if v, ok := w.GameRule(name); ok {
			rules = append(rules, v2193.GameRule{Name: client, Value: clientGameRuleValue(v)})
		}
	}
	return rules
}

// ViewGameRule sends a changed gamerule to the client if it has a Bedrock
// equivalent.
func (s *Session) ViewGameRule(name string, value any) {
	if client, ok := clientGameRules[name]; ok {
		s.sendGameRules([]v2193.GameRule{{Name: client, Value: clientGameRuleValue(value)}})
	}
}

// clientGameRuleValue converts integer rules to the uint32 gophertunnel
// encodes as a Bedrock integer gamerule.
func clientGameRuleValue(v any) any {
	if n, ok := v.(int); ok {
		return uint32(max(n, 0))
	}
	return v
}

// Spectating reports whether the player of the session watches as a
// spectator, whose chunk loading spectators_generate_chunks controls.
func (s *Session) Spectating(tx *world.Tx) bool {
	if s.ent == nil {
		return false
	}
	e, ok := s.ent.Entity(tx)
	if !ok {
		return false
	}
	g, ok := e.(interface{ GameMode() world.GameMode })
	return ok && g.GameMode() == world.GameModeSpectator
}
