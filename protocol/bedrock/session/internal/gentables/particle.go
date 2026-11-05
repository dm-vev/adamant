package main

import (
	"bytes"
	"fmt"
	"log"
	"unicode"
)

// particleKind is the table of the particles of server/world/particle.
var particleKind = kindSpec{
	table:    "particle_table.go",
	variable: "particleRows",
	key:      "Particle",
	fields:   []string{"Particle", "Level", "Legacy", "Bool", "Hash", "RGBA", "ARGB", "Field"},
	pkg:      "github.com/df-mc/dragonfly/server/world/particle",
	worldPkg: "github.com/df-mc/dragonfly/server/world",
	iface:    "Particle",
	out:      "particle_switch_gen.go",
	testOut:  "particle_types_gen_test.go",

	generate: generateParticles,
	generateTypes: func(names []string) []byte {
		return typesFile("particleTypes", "for the tests that compare the packets of every particle.", "particle",
			"github.com/df-mc/dragonfly/server/world/particle", "Particle", names)
	},
}

// generateParticles returns the source of the switch of the particles of the
// rows.
func generateParticles(rows []row) []byte {
	var b bytes.Buffer
	b.WriteString(header)
	b.WriteString(`package session

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/go-gl/mathgl/mgl64"
)

// literalParticlePacket hands out the packet of a particle of
// particle_table.go and returns true, or returns false for a particle the
// table does not hold, whose packets particlePackets works out.
func (s *Session) literalParticlePacket(pos mgl64.Vec3, p world.Particle, out func(proto.Packet)) bool {
	switch pa := p.(type) {
`)
	for _, r := range rows {
		fmt.Fprintf(&b, "\tcase particle.%s:\n", r["Particle"])
		b.WriteString(particleCase(r))
	}
	b.WriteString("\tdefault:\n\t\treturn false\n\t}\n\treturn true\n}\n")
	return b.Bytes()
}

// particleCase returns the statements of the case of a row.
func particleCase(r row) string {
	name := r["Particle"]
	var event string
	switch {
	case r.has("Level") && r.has("Legacy"):
		log.Fatalf("row %s: Level with Legacy", name)
	case r.has("Level"):
		event = "mc." + r["Level"]
	case r.has("Legacy"):
		event = "mc.LevelEventParticleLegacyEvent | " + legacyType(r["Legacy"])
	default:
		log.Fatalf("row %s: none of Level and Legacy", name)
	}
	var data string
	n := 0
	for _, f := range []string{"Bool", "Hash", "RGBA", "ARGB", "Field"} {
		if !r.has(f) {
			continue
		}
		n++
		field := r[f]
		switch f {
		case "Bool":
			data = fmt.Sprintf("int32(boolByte(pa.%s))", field)
		case "Hash":
			data = fmt.Sprintf("int32(s.blockHash(pa.%s))", field)
		case "RGBA":
			data = fmt.Sprintf("int32FromRGBA(pa.%s)", field)
		case "ARGB":
			data = fmt.Sprintf("(int32(pa.%[1]s.A) << 24) | (int32(pa.%[1]s.R) << 16) | (int32(pa.%[1]s.G) << 8) | int32(pa.%[1]s.B)", field)
		case "Field":
			data = "pa." + field
		}
	}
	if n > 1 {
		log.Fatalf("row %s: more than one of Bool, Hash, RGBA, ARGB and Field", name)
	}
	if data == "" {
		return fmt.Sprintf("\t\tout(&mc.LevelEventPacket{EventType: %s, Position: vec64To32(pos)})\n", event)
	}
	return fmt.Sprintf("\t\tout(&mc.LevelEventPacket{EventType: %s, Position: vec64To32(pos), EventData: %s})\n", event, data)
}

// legacyType returns the expression of the legacy particle type a row names:
// a number as it is, else the name of a constant of the protocol.
func legacyType(s string) string {
	if unicode.IsDigit(rune(s[0])) {
		return s
	}
	return "mc." + s
}
