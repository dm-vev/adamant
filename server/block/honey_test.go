package block

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/go-gl/mathgl/mgl64"
)

func TestHoneyContactMatchesExecutedClient(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_honey_contact.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Origin, Velocity, Motion [3]float32
			Width                    float32
			Contacts                 []cube.Pos
			FallDistance             float32 `json:"fall_distance"`
			FallAfter                float32 `json:"fall_after"`
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != 10752 {
		t.Fatal("incomplete original-client honey contact fixture")
	}
	vec := func(p [3]float32) mgl64.Vec3 { return mgl64.Vec3{float64(p[0]), float64(p[1]), float64(p[2])} }
	for n, c := range native.Scenarios {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			velocity, fall := vec(c.Velocity), c.FallDistance
			for _, pos := range c.Contacts {
				var reset bool
				velocity, reset = honeyContactVelocity(pos, vec(c.Origin), velocity, float64(c.Width))
				if reset {
					fall = 0
				}
			}
			if velocity != vec(c.Motion) || fall != c.FallAfter {
				t.Errorf("velocity=%v fall=%g, original=%v %g", velocity, fall, vec(c.Motion), c.FallAfter)
			}
		})
	}
}

func TestHoneyBlockInterfacesAndFriction(t *testing.T) {
	var honey any = HoneyBlock{}
	if _, ok := honey.(EntityInsider); !ok {
		t.Fatal("HoneyBlock does not implement EntityInsider")
	}
	frictional, ok := honey.(Frictional)
	if !ok {
		t.Fatal("HoneyBlock does not implement Frictional")
	}
	if got := frictional.Friction(); got != 0.8 {
		t.Fatalf("Friction() = %v, want 0.8", got)
	}
}

func TestHoneyStandingMatchesExecutedClient(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_honey_contact.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete bool
		Standing []struct {
			Velocity, Motion [3]float32
			Sneaking         bool
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Standing) != 144 {
		t.Fatal("incomplete original-client honey standing fixture")
	}
	for n, c := range native.Standing {
		v := mgl64.Vec3{float64(c.Velocity[0]), float64(c.Velocity[1]), float64(c.Velocity[2])}
		want := mgl64.Vec3{float64(c.Motion[0]), float64(c.Motion[1]), float64(c.Motion[2])}
		if got := honeyStandVelocity(v, c.Sneaking); got != want {
			t.Errorf("case %d: velocity=%v, original=%v", n, got, want)
		}
	}
}
