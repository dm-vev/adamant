package player

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestBedStandUpMatchesJava263Source(t *testing.T) {
	var fixture struct {
		Cases []struct {
			Name, Facing             string
			Yaw                      float64
			Bunk                     bool
			FirstCandidateObstructed bool
			FirstCandidateDangerous  bool
			Position                 *[3]float64
		} `json:"cases"`
	}
	data, err := os.ReadFile("testdata/java263_bed_standup_source.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, tc := range fixture.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			facing := cube.North
			for _, direction := range cube.Directions() {
				if direction.String() == tc.Facing {
					facing = direction
				}
			}
			w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
			t.Cleanup(func() { _ = w.Close() })
			foot := cube.Pos{0, 65, 0}
			head := foot.Side(facing.Face())
			job := w.Do(func(tx *world.Tx) {
				for x := -4; x <= 4; x++ {
					for z := -4; z <= 4; z++ {
						tx.SetBlock(cube.Pos{x, 64, z}, block.Stone{}, nil)
					}
				}
				tx.SetBlock(foot, block.StrawBed{Facing: facing}, nil)
				tx.SetBlock(head, block.StrawBed{Facing: facing, Head: true}, nil)
				// The original Source proxy checks its y64 stone layer before sparse
				// overrides: its requested bunk is not an effective lower bed.
				if tc.FirstCandidateObstructed {
					first, ok := bedStandUpPosition(tx, head, facing, tc.Yaw)
					if !ok {
						t.Fatal("baseline candidate missing")
					}
					obstacle := world.Block(block.Stone{})
					if tc.FirstCandidateDangerous {
						obstacle = block.Cactus{}
					}
					tx.SetBlock(cube.PosFromVec3(first), obstacle, nil)
				}
				got, ok := bedStandUpPosition(tx, head, facing, tc.Yaw)
				if tc.Position == nil {
					if ok {
						t.Fatalf("unexpected standup=%v", got)
					}
				} else if !ok || got != mgl64.Vec3(*tc.Position) {
					t.Fatalf("standup=%v found=%v Source=%v", got, ok, *tc.Position)
				}
			})
			<-job.Done()
			if err := job.Err(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestBedStandUpEffectiveBunkMatchesJava263Source(t *testing.T) {
	var fixture struct {
		Cases []struct {
			Name, Facing               string
			TopSurroundDangerousBlocks int
			AllSearchCandidatesBlocked bool
			Position                   *[3]float64
		} `json:"cases"`
	}
	data, err := os.ReadFile("testdata/java263_bed_effective_bunk_source.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, tc := range fixture.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			facing := cube.North
			for _, direction := range cube.Directions() {
				if direction.String() == tc.Facing {
					facing = direction
				}
			}
			w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
			t.Cleanup(func() { _ = w.Close() })
			foot := cube.Pos{0, 65, 0}
			head := foot.Side(facing.Face())
			job := w.Do(func(tx *world.Tx) {
				for x := -4; x <= 4; x++ {
					for z := -4; z <= 4; z++ {
						tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, nil)
						tx.SetBlock(cube.Pos{x, 64, z}, block.Slab{Block: block.Stone{}}, nil)
					}
				}
				tx.SetBlock(foot, block.StrawBed{Facing: facing}, nil)
				tx.SetBlock(head, block.StrawBed{Facing: facing, Head: true}, nil)
				tx.SetBlock(head.Side(cube.FaceDown), block.StrawBed{Facing: facing, Head: true}, nil)
				f, s := cube.Pos{}.Side(facing.Face()), cube.Pos{}.Side(facing.RotateRight().Face())
				back, opposite := cube.Pos{-f[0], 0, -f[2]}, cube.Pos{-s[0], 0, -s[2]}
				backTwo := back.Add(back)
				surround := [10]cube.Pos{s, s.Add(back), s.Add(backTwo), backTwo, opposite.Add(backTwo), opposite.Add(back), opposite, opposite.Add(f), f, s.Add(f)}
				if tc.TopSurroundDangerousBlocks != 0 {
					for _, offset := range surround {
						tx.SetBlock(head.Add(offset), block.Lava{Depth: 8}, nil)
					}
				}
				if tc.AllSearchCandidatesBlocked {
					for _, offset := range surround {
						tx.SetBlock(head.Add(offset), block.Stone{}, nil)
						tx.SetBlock(head.Add(offset).Side(cube.FaceDown), block.Stone{}, nil)
					}
					tx.SetBlock(head, block.Stone{}, nil)
					tx.SetBlock(foot, block.Stone{}, nil)
				}
				got, ok := bedStandUpPosition(tx, head, facing, 0)
				if tc.Position == nil {
					if ok {
						t.Fatalf("unexpected standup=%v", got)
					}
				} else if !ok || got != mgl64.Vec3(*tc.Position) {
					t.Fatalf("standup=%v found=%v Source=%v", got, ok, *tc.Position)
				}
			})
			<-job.Done()
			if err := job.Err(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
