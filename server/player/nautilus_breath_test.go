package player

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestBreathOfTheNautilusHoldsAirUnderwater(t *testing.T) {
	cases := []struct {
		name       string
		underwater bool
		effects    []effect.LastingType
		want       time.Duration
	}{
		{"underwater without effects", true, nil, 3 * time.Second},
		{"underwater with nautilus", true, []effect.LastingType{effect.BreathOfTheNautilus}, 5 * time.Second},
		{"underwater with nautilus and water breathing", true, []effect.LastingType{effect.BreathOfTheNautilus, effect.WaterBreathing}, 15 * time.Second},
		{"in air with nautilus", false, []effect.LastingType{effect.BreathOfTheNautilus}, 15 * time.Second},
	}
	for _, c := range cases {
		w := world.Config{Synchronous: true}.New()
		<-w.Exec(func(tx *world.Tx) {
			pos := mgl64.Vec3{0.5, 1, 0.5}
			if c.underwater {
				for y := 1; y <= 3; y++ {
					tx.SetBlock(cube.Pos{0, y, 0}, block.Water{Depth: 8, Still: true}, nil)
				}
			}
			p := tx.AddEntity(world.EntitySpawnOpts{Position: pos}.New(Type, Config{Position: pos, GameMode: world.GameModeSurvival})).(*Player)
			p.SetAirSupply(5 * time.Second)
			p.breathing = false
			for _, e := range c.effects {
				p.AddEffect(effect.New(e, 1, time.Minute))
			}
			for range 40 {
				p.tickAirSupply()
			}
			if got := p.AirSupply(); got != c.want {
				t.Errorf("%s: air supply %v, want %v", c.name, got, c.want)
			}
		})
		_ = w.Close()
	}
}
