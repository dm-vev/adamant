package session

import (
	"encoding/json"
	"os"
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/entity"
)

// bdsActorOrder is the capture of testdata/bedrock_1_26_52_actor_order.json:
// the order in which BDS 1.26.52.3 lists the types of actors in
// AvailableActorIdentifiers and sends the SyncActorProperty packets.
type bdsActorOrder struct {
	Version          string `json:"version"`
	Build            int    `json:"build"`
	ActorIdentifiers struct {
		SHA256 string   `json:"sha256"`
		IDs    []string `json:"ids"`
	} `json:"actor_identifiers"`
	ActorProperties []struct {
		Type   string `json:"type"`
		SHA256 string `json:"sha256"`
	} `json:"actor_properties"`
}

func loadBDSActorOrder(t *testing.T) bdsActorOrder {
	t.Helper()
	b, err := os.ReadFile("testdata/bedrock_1_26_52_actor_order.json")
	if err != nil {
		t.Fatal(err)
	}
	var c bdsActorOrder
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatal(err)
	}
	if c.Version != "1.26.52.3" || c.Build != 51798919 || len(c.ActorIdentifiers.SHA256) != 64 || len(c.ActorProperties) == 0 {
		t.Fatalf("capture of %s build %d is not that of BDS 1.26.52.3", c.Version, c.Build)
	}
	return c
}

// TestActorOrderAsBDS checks that the session lists the types of actors of
// the default registry in AvailableActorIdentifiers, and declares their
// properties in SyncActorProperty, in the order BDS 1.26.52.3 does for the
// types both have.
func TestActorOrderAsBDS(t *testing.T) {
	c := loadBDSActorOrder(t)
	if !slices.Equal(bdsActorIdentifiers, c.ActorIdentifiers.IDs) {
		t.Fatal("bdsActorIdentifiers is not the order of the capture")
	}
	var types []string
	for _, p := range c.ActorProperties {
		types = append(types, p.Type)
	}
	if !slices.Equal(bdsActorProperties, types) {
		t.Fatal("bdsActorProperties is not the order of the capture")
	}

	// Two types of the default registry share minecraft:falling_block: the
	// list holds it twice, adjacent.
	ids := slices.Compact(actorTypesInOrder(entity.DefaultRegistry, bdsActorIdentifierRank))
	var shared []string
	for _, id := range c.ActorIdentifiers.IDs {
		if slices.Contains(ids, id) {
			shared = append(shared, id)
		}
	}
	if !slices.Equal(ids[:len(shared)], shared) {
		t.Fatalf("identifiers in the order\n%q\nBDS lists them in\n%q", ids[:len(shared)], shared)
	}
	if rest := ids[len(shared):]; len(rest) != 0 {
		t.Fatalf("types BDS does not list: %q", rest)
	}

	var declared []string
	for _, id := range slices.Compact(actorTypesInOrder(entity.DefaultRegistry, bdsActorPropertyRank)) {
		if _, ok := actorPropertyTypes[id]; ok {
			declared = append(declared, id)
		}
	}
	if !slices.Equal(declared, types) {
		t.Fatalf("properties declared in the order\n%q\nBDS sends them in\n%q", declared, types)
	}
}

// TestActorOrderPutsOthersLast checks that the types BDS does not have
// follow those it has, by ID.
func TestActorOrderPutsOthersLast(t *testing.T) {
	ids := []string{"adamant:b", "minecraft:item", "adamant:a", "minecraft:player", "minecraft:item"}
	orderActorIDs(ids, bdsActorIdentifierRank)
	want := []string{"minecraft:player", "minecraft:item", "minecraft:item", "adamant:a", "adamant:b"}
	if !slices.Equal(ids, want) {
		t.Fatalf("ordered %q, want %q", ids, want)
	}
}
