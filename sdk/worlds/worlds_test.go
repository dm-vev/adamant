package worlds_test

import (
	"slices"
	"testing"
	"time"

	"github.com/dm-vev/adamant/sdk/plugin"
	"github.com/dm-vev/adamant/sdk/sdktest"
	"github.com/dm-vev/adamant/sdk/worlds"
	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/player"
	"github.com/dm-vev/adamant/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestWorldControlsForceChunkAndRegions(t *testing.T) {
	app := sdktest.NewApp(t)
	w := sdktest.NewWorld(t)

	// GameRules, time, weather, difficulty, scoreboard.
	if err := worlds.SetGameRule(w, "keep_inventory", true); err != nil {
		t.Fatalf("SetGameRule: %v", err)
	}
	if !worlds.GameRuleBool(w, "keep_inventory") {
		t.Fatal("GameRuleBool(keep_inventory) = false")
	}
	worlds.SetTime(w, 6000)
	worlds.StopTime(w)
	if worlds.Time(w) != 6000 {
		t.Fatalf("Time = %d, want 6000", worlds.Time(w))
	}
	worlds.StartTime(w)
	worlds.StartRain(w, time.Minute)
	worlds.StopRain(w)
	worlds.SetDifficulty(w, world.DifficultyHard)
	if worlds.Difficulty(w) != world.DifficultyHard {
		t.Fatalf("Difficulty = %v, want Hard", worlds.Difficulty(w))
	}
	if worlds.Scoreboard(w) == nil {
		t.Fatal("Scoreboard returned nil")
	}

	// ForceChunk ticket auto-released on plugin disable.
	pos := world.ChunkPos{1, 2}
	app.Use(plugin.Func("tickets", func(ctx *plugin.Context) {
		<-w.Exec(func(tx *world.Tx) {
			if _, err := worlds.ForceChunk(ctx, tx, pos); err != nil {
				t.Fatalf("ForceChunk: %v", err)
			}
		})
	}, nil))
	if !worlds.IsChunkForced(w, pos) || !slices.Contains(worlds.ForcedChunks(w), pos) {
		t.Fatal("expected chunk {1,2} force-loaded")
	}
	app.Disable()
	<-w.Exec(func(*world.Tx) {}) // flush deferred release
	if worlds.IsChunkForced(w, pos) {
		t.Fatal("expected chunk {1,2} unforced on plugin disable")
	}

	// RegionIndex spatial lookup, priority, and enter/leave tracking.
	idx := worlds.NewRegionIndex()
	idx.Add(worlds.Region{
		Name:     "spawn",
		Box:      worlds.NewBox(cube.Pos{-10, 0, -10}, cube.Pos{10, 128, 10}),
		Priority: 1,
		Tags:     []string{"safe"},
	})
	idx.Add(worlds.Region{
		Name:     "pvp_pit",
		Box:      worlds.NewBox(cube.Pos{0, 60, 0}, cube.Pos{5, 70, 5}),
		Priority: 10,
		Tags:     []string{"pvp"},
	})

	hits := idx.At(mgl64.Vec3{2.5, 64, 2.5})
	if len(hits) != 2 || hits[0].Name != "pvp_pit" || hits[1].Name != "spawn" {
		t.Fatalf("At({2.5,64,2.5}) = %+v, want [pvp_pit, spawn]", hits)
	}
	if !hits[0].HasTag("pvp") {
		t.Fatal("expected pvp tag")
	}
	if len(idx.Intersecting(worlds.NewBox(cube.Pos{4, 64, 4}, cube.Pos{20, 64, 20}))) != 2 {
		t.Fatal("expected 2 intersecting regions")
	}

	var entered, left []string
	stop := idx.TrackWorld(nil, w,
		func(_ *player.Player, r *worlds.Region, _ *world.Tx) { entered = append(entered, r.Name) },
		func(_ *player.Player, r *worlds.Region, _ *world.Tx) { left = append(left, r.Name) },
	)
	defer stop()

	fp := sdktest.SpawnPlayer(t, w, "Runner", mgl64.Vec3{50, 64, 50})
	fp.Run(func(p *player.Player, _ *world.Tx) {
		p.Teleport(mgl64.Vec3{2.5, 64, 2.5})
		p.Teleport(mgl64.Vec3{50, 64, 50})
	})
	if !slices.Equal(entered, []string{"pvp_pit", "spawn"}) || !slices.Equal(left, []string{"pvp_pit", "spawn"}) {
		t.Fatalf("TrackWorld entered=%v left=%v", entered, left)
	}

	if !idx.Remove("pvp_pit") || len(idx.AtBlock(cube.Pos{2, 64, 2})) != 1 {
		t.Fatal("Remove(pvp_pit) failed")
	}
}
