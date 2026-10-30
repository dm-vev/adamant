package block

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

func TestButtonProjectileGeometryMatchesInspectedClient(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_button_geometry.json")
	if err != nil {
		t.Fatal(err)
	}
	var reference struct {
		Complete        bool
		Bounds          [][6]float64
		CollidingActors map[string]struct {
			ActorType uint32 `json:"actor_type"`
		} `json:"colliding_actors"`
		PressTicks struct{ Wood, Stone int } `json:"press_ticks"`
	}
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	if !reference.Complete || len(reference.Bounds) != 6 || len(reference.CollidingActors) != 2 {
		t.Fatal("invalid inspected client button geometry")
	}
	for _, kind := range ButtonTypes() {
		ticks := reference.PressTicks.Wood
		if kind == StoneButton() || kind.blockName() == "polished_blackstone_button" {
			ticks = reference.PressTicks.Stone
		}
		if kind.pressTicks() != ticks {
			t.Errorf("%s press ticks=%d native=%d", kind.blockName(), kind.pressTicks(), ticks)
		}
		for face, native := range reference.Bounds {
			for _, pressed := range []bool{false, true} {
				b := Button{Type: kind, Facing: cube.Face(face), Pressed: pressed}
				collider, ok := any(b).(world.ProjectileCollider)
				if !ok {
					t.Fatal("button lacks its native projectile collision geometry")
				}
				for _, id := range []string{"minecraft:arrow", "minecraft:thrown_trident", "minecraft:snowball", "minecraft:item"} {
					h := (world.EntitySpawnOpts{}).New(buttonNativeProjectileType{id: id}, buttonNativeProjectileConfig{})
					e := buttonNativeProjectile{h: h, d: &world.EntityData{}}
					got := collider.ProjectileBBox(cube.Pos{}, nil, e)
					_, collides := reference.CollidingActors[id]
					if !collides {
						if len(got) != 0 {
							t.Errorf("%s collides with native-excluded %s", kind.blockName(), id)
						}
					} else if len(got) != 1 || got[0] != cube.Box(native[0], native[1], native[2], native[3], native[4], native[5]) {
						t.Errorf("%s face=%d pressed=%v actor=%s boxes=%v native=%v", kind.blockName(), face, pressed, id, got, native)
					}
					_ = h.Close()
				}
			}
		}
	}
}
