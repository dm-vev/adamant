package main

import (
	"bytes"
	"fmt"
	"log"
)

// actionKind is the table of the actions of server/entity.
var actionKind = kindSpec{
	table:    "entity_action_table.go",
	variable: "entityActionRows",
	key:      "Action",
	fields:   []string{"Action", "Event", "Alt", "When", "Data", "Ticks", "Animate", "Count", "Default"},
	pkg:      "github.com/df-mc/dragonfly/server/entity",
	worldPkg: "github.com/df-mc/dragonfly/server/world",
	iface:    "EntityAction",
	out:      "entity_action_switch_gen.go",
	testOut:  "entity_action_types_gen_test.go",

	generate: generateActions,
	generateTypes: func(names []string) []byte {
		return typesFile("entityActionTypes", "for the tests that compare the packets of every action.", "entity",
			"github.com/df-mc/dragonfly/server/entity", "EntityAction", names)
	},
}

// generateActions returns the source of the switch of the actions of the rows.
func generateActions(rows []row) []byte {
	var b bytes.Buffer
	b.WriteString(header)
	b.WriteString(`package session

import (
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
)

// literalEntityAction writes the packet of an action of entity_action_table.go
// and returns true, or returns false for an action the table does not hold,
// whose packets tabledEntityAction works out.
func (s *Session) literalEntityAction(e world.Entity, a world.EntityAction) bool {
	switch act := a.(type) {
`)
	for _, r := range rows {
		fmt.Fprintf(&b, "\tcase entity.%s:\n", r["Action"])
		b.WriteString(actionCase(r))
	}
	b.WriteString("\tdefault:\n\t\treturn false\n\t}\n\treturn true\n}\n")
	return b.Bytes()
}

// actionCase returns the statements of the case of a row.
func actionCase(r row) string {
	name := r["Action"]
	if r.has("Animate") {
		if !r.has("Count") || !r.has("Default") || r.has("Event") {
			log.Fatalf("row %s: Animate takes Count and Default, and no Event", name)
		}
		return fmt.Sprintf(`		count := act.%[1]s
		if count <= 0 {
			count = %[2]s
		}
		s.writePacket(&mc.AnimatePacket{
			ActionType:      mc.%[3]s,
			EntityRuntimeID: s.entityRuntimeID(e),
			Data:            float32(count),
		})
`, r["Count"], r["Default"], r["Animate"])
	}
	if !r.has("Event") || r.has("Count") || r.has("Default") {
		log.Fatalf("row %s: an Event, and no Count and Default without Animate", name)
	}
	if r.has("Alt") != r.has("When") {
		log.Fatalf("row %s: Alt and When come together", name)
	}
	if r.has("Data") && r.has("Ticks") {
		log.Fatalf("row %s: Data with Ticks", name)
	}
	if r.has("Alt") && (r.has("Data") || r.has("Ticks")) {
		log.Fatalf("row %s: Alt with data", name)
	}
	var b bytes.Buffer
	event := "mc." + r["Event"]
	if r.has("Alt") {
		fmt.Fprintf(&b, "\t\tevent := byte(mc.%s)\n\t\tif act.%s {\n\t\t\tevent = mc.%s\n\t\t}\n", r["Event"], r["When"], r["Alt"])
		event = "event"
	}
	fmt.Fprintf(&b, "\t\ts.writePacket(&mc.ActorEventPacket{\n\t\t\tEntityRuntimeID: s.entityRuntimeID(e),\n\t\t\tEventType:       %s,\n", event)
	switch {
	case r.has("Data"):
		fmt.Fprintf(&b, "\t\t\tEventData:       int32(act.%s),\n", r["Data"])
	case r.has("Ticks"):
		fmt.Fprintf(&b, "\t\t\tEventData:       int32(act.%s.Milliseconds() / 50),\n", r["Ticks"])
	}
	b.WriteString("\t\t})\n")
	return b.String()
}
