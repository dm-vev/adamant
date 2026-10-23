package entity

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestFishFormSchools checks FollowFlockLeaderGoal: cod within eight blocks
// of each other form a school one of them leads, which the others follow.
// The pool is small enough to keep them that close.
func TestFishFormSchools(t *testing.T) {
	w := world.Config{Entities: DefaultRegistry, Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	var fish []*world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		for x := -3; x <= 3; x++ {
			for z := -3; z <= 3; z++ {
				tx.SetBlock(cube.Pos{x, 59, z}, block.Stone{}, nil)
				for y := 60; y <= 63; y++ {
					if x == -3 || x == 3 || z == -3 || z == 3 {
						tx.SetBlock(cube.Pos{x, y, z}, block.Stone{}, nil)
					} else {
						tx.SetBlock(cube.Pos{x, y, z}, block.Water{Still: true, Depth: 8}, nil)
					}
				}
			}
		}
		for i := range 4 {
			fish = append(fish, tx.AddEntity(NewCod(world.EntitySpawnOpts{Position: mgl64.Vec3{float64(i) - 1.5, 61, .5}})).H())
		}
	})
	for range 1200 {
		w.AdvanceTick()
		leader, followers := 0, 0
		<-w.Exec(func(tx *world.Tx) {
			for _, h := range fish {
				e, _ := h.Entity(tx)
				b := e.(*Cod).behaviour()
				if b.schoolSize == len(fish) {
					leader++
				}
				if b.isFollower(tx) {
					followers++
				}
			}
		})
		if leader == 1 && followers == len(fish)-1 {
			return
		}
	}
	t.Fatal("four cod side by side did not form one school in a minute")
}
