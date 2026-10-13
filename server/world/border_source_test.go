package world

import (
	"encoding/json"
	"math"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/go-gl/mathgl/mgl64"
)

type sourceBorderBounds struct {
	Size                   float64
	Center                 [2]float64
	MinX, MaxX, MinZ, MaxZ float64
}

func TestWorldBorderMatchesJava263Source(t *testing.T) {
	var fixture struct {
		Static struct {
			Bounds     sourceBorderBounds
			Acceptance map[string]bool
			Clamp      [3]float64         `json:"clamp_inside_limits"`
			Default    sourceBorderBounds `json:"max_size_default_bounds"`
		} `json:"static_fractional_small"`
		Absolute struct {
			Bounds sourceBorderBounds `json:"actual_bounds"`
			Clamp  [2]float64         `json:"clamp_outside"`
		} `json:"absolute_clamp"`
		Shrinking []struct {
			Ticks     int `json:"ticksAdvanced"`
			Bounds    sourceBorderBounds
			Remaining int64 `json:"remainingTicks"`
			Accepted  bool  `json:"playerAabbAccepted"`
		} `json:"shrinking_transition"`
		Codec struct {
			Payload  BorderSettings     `json:"encoded_payload"`
			Reloaded sourceBorderBounds `json:"reloaded_bounds_at_initialization"`
		} `json:"settings_codec"`
	}
	data, err := os.ReadFile("testdata/java263_world_border_source.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	w := Config{Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	assertBounds := func(want sourceBorderBounds) {
		t.Helper()
		minX, maxX, minZ, maxZ := w.border.bounds()
		got := sourceBorderBounds{w.Border().Size, [2]float64{w.Border().CenterX, w.Border().CenterZ}, minX, maxX, minZ, maxZ}
		if got != want {
			t.Fatalf("border=%+v Source=%+v", got, want)
		}
	}
	assertBounds(fixture.Static.Default)
	b := fixture.Static.Bounds
	w.SetBorderCenter(b.Center[0], b.Center[1])
	w.SetBorderSize(b.Size, 0)
	assertBounds(b)
	checks := map[string]bool{
		"point_min_x":                  w.border.contains(b.MinX, b.Center[1]),
		"point_max_x":                  w.border.contains(b.MaxX, b.Center[1]),
		"point_min_z":                  w.border.contains(b.Center[0], b.MinZ),
		"point_max_z":                  w.border.contains(b.Center[0], b.MaxZ),
		"blockpos_min_int":             w.WithinBorder(cube.Pos{int(math.Ceil(b.MinX)), 64, int(math.Ceil(b.MinZ))}),
		"vec3_center":                  w.border.contains(b.Center[0], b.Center[1]),
		"aabb_edge_epsilon_straddle":   w.WithinBorderBounds(cube.Box(b.MinX, 0, b.MinZ, b.MaxX+.000005, 2, b.MaxZ)),
		"aabb_beyond_epsilon_rejected": w.WithinBorderBounds(cube.Box(b.MinX, 0, b.MinZ, b.MaxX+.00002, 2, b.MaxZ)),
		"chunk_origin":                 w.WithinBorder(cube.Pos{0, 0, 0}) && w.WithinBorder(cube.Pos{15, 0, 15}),
	}
	for name, want := range fixture.Static.Acceptance {
		if checks[name] != want {
			t.Fatalf("%s=%v Source=%v", name, checks[name], want)
		}
	}
	if got := w.ClampToBorder(mgl64.Vec3{100, 71, -100}); got != mgl64.Vec3(fixture.Static.Clamp) {
		t.Fatalf("clamp=%v Source=%v", got, fixture.Static.Clamp)
	}
	b = fixture.Absolute.Bounds
	w.SetBorderCenter(b.Center[0], b.Center[1])
	w.SetBorderSize(b.Size, 0)
	assertBounds(b)
	if got := w.ClampToBorder(mgl64.Vec3{40000000, 9, -40000000}); got[0] != fixture.Absolute.Clamp[0] || got[1] != 9 || got[2] != fixture.Absolute.Clamp[1] {
		t.Fatalf("absolute clamp=%v Source XZ=%v", got, fixture.Absolute.Clamp)
	}
	w.SetBorderCenter(0, 0)
	w.SetBorderSize(20, 0)
	w.SetBorderSize(4, 8)
	advanced := 0
	for _, frame := range fixture.Shrinking {
		for advanced < frame.Ticks {
			w.AdvanceTick()
			advanced++
		}
		assertBounds(frame.Bounds)
		if got := w.Border().LerpTime; got != frame.Remaining {
			t.Fatalf("remaining ticks=%d Source=%d", got, frame.Remaining)
		}
		if got := w.WithinBorderBounds(cube.Box(8.7, 0, 0, 9.3, 1.8, .6)); got != frame.Accepted {
			t.Fatalf("shrinking player bbox accepted=%v Source=%v", got, frame.Accepted)
		}
	}
	w.SetBorder(fixture.Codec.Payload)
	assertBounds(fixture.Codec.Reloaded)
	if got := w.Border(); got != fixture.Codec.Payload {
		t.Fatalf("reloaded border settings=%+v Source=%+v", got, fixture.Codec.Payload)
	}
}
