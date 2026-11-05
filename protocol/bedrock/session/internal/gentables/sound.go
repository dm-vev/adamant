package main

import (
	"bytes"
	"fmt"
	"log"
	"strconv"
)

// soundKind is the table of the sounds of server/world/sound.
var soundKind = kindSpec{
	table:    "sound_table.go",
	variable: "soundRows",
	key:      "Sound",
	fields:   []string{"Sound", "Level", "Data", "Event", "Entity", "NoEntity", "Hash", "ExtraField", "Extra", "Play", "Volume", "Pitch"},
	pkg:      "github.com/df-mc/dragonfly/server/world/sound",
	worldPkg: "github.com/df-mc/dragonfly/server/world",
	iface:    "Sound",
	out:      "sound_switch_gen.go",
	testOut:  "sound_types_gen_test.go",

	generate: generateSounds,
	generateTypes: func(names []string) []byte {
		return typesFile("soundTypes", "for the tests that compare the packets of every sound.", "sound",
			"github.com/df-mc/dragonfly/server/world/sound", "Sound", names)
	},
}

// generateSounds returns the source of the switch of the sounds of the rows.
func generateSounds(rows []row) []byte {
	var b bytes.Buffer
	b.WriteString(header)
	b.WriteString(`package session

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/go-gl/mathgl/mgl64"
)

// literalSoundPacket hands out the packets of a sound of sound_table.go and
// returns true, or returns false for a sound the table does not hold, whose
// packets soundPackets works out.
func (s *Session) literalSoundPacket(pos mgl64.Vec3, t world.Sound, disableRelative bool, out func(proto.Packet)) bool {
	switch so := t.(type) {
`)
	for _, r := range rows {
		fmt.Fprintf(&b, "\tcase sound.%s:\n", r["Sound"])
		b.WriteString(soundCase(r))
	}
	b.WriteString("\tdefault:\n\t\treturn false\n\t}\n\treturn true\n}\n")
	return b.Bytes()
}

// soundCase returns the statements of the case of a row.
func soundCase(r row) string {
	name := r["Sound"]
	switch {
	case r.has("Level"):
		if r.has("Event") || r.has("Play") {
			log.Fatalf("row %s: Level with Event or Play", name)
		}
		if r.has("Data") {
			return fmt.Sprintf("\t\tout(&mc.LevelEventPacket{EventType: mc.%s, Position: vec64To32(pos), EventData: %s})\n", r["Level"], r["Data"])
		}
		return fmt.Sprintf("\t\tout(&mc.LevelEventPacket{EventType: mc.%s, Position: vec64To32(pos)})\n", r["Level"])
	case r.has("Play"):
		if r.has("Event") || !r.has("Volume") || !r.has("Pitch") {
			log.Fatalf("row %s: Play needs Volume and Pitch and no Event", name)
		}
		return fmt.Sprintf("\t\tout(&mc.PlaySoundPacket{SoundName: %s, Position: vec64To32(pos), Volume: %s, Pitch: %s})\n",
			strconv.Quote(r["Play"]), r["Volume"], r["Pitch"])
	case r.has("Event"):
		var b bytes.Buffer
		b.WriteString("\t\tpk := levelSoundPacket(pos, disableRelative)\n")
		fmt.Fprintf(&b, "\t\tpk.SoundType = mc.%s\n", r["Event"])
		if r.has("Entity") && r.has("NoEntity") {
			log.Fatalf("row %s: Entity with NoEntity", name)
		}
		if r.has("Entity") {
			fmt.Fprintf(&b, "\t\tpk.EntityType = %s\n", strconv.Quote(r["Entity"]))
		}
		if r.has("NoEntity") {
			b.WriteString("\t\tpk.EntityType = \"\"\n")
		}
		extras := 0
		for _, f := range []string{"Hash", "ExtraField", "Extra"} {
			if r.has(f) {
				extras++
			}
		}
		if extras > 1 {
			log.Fatalf("row %s: more than one of Hash, ExtraField and Extra", name)
		}
		switch {
		case r.has("Hash"):
			fmt.Fprintf(&b, "\t\tpk.ExtraData = int32(s.blockHash(so.%s))\n", r["Hash"])
		case r.has("ExtraField"):
			fmt.Fprintf(&b, "\t\tpk.ExtraData = int32(so.%s)\n", r["ExtraField"])
		case r.has("Extra"):
			fmt.Fprintf(&b, "\t\tpk.ExtraData = %s\n", r["Extra"])
		}
		b.WriteString("\t\tout(pk)\n")
		return b.String()
	}
	log.Fatalf("row %s: none of Level, Play and Event", name)
	return ""
}
