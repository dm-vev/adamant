package world

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/go-gl/mathgl/mgl64"
)

type nativeItemLaunchObservation struct {
	Words      []uint32
	Phase, Yaw float32
	Velocity   [3]float32
	NextWord   uint32 `json:"next_word"`
}

func TestItemLaunchMatchesExecutedClientAndLiveTLS(t *testing.T) {
	data, err := os.ReadFile("../block/testdata/bedrock_1_26_51_item_launch_native.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Seed     uint32
			Launches []nativeItemLaunchObservation
		}
		Live []struct {
			Position   cube.Pos
			Launch     nativeItemLaunchObservation
			FactoryYaw float32 `json:"factory_yaw"`
		} `json:"live_cases"`
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != 6 || len(native.Live) != 7 {
		t.Fatal("incomplete native launch/TLS evidence")
	}
	compare := func(t *testing.T, got nativeItemLaunch, want nativeItemLaunchObservation) {
		t.Helper()
		if math.Float32bits(got.phase) != math.Float32bits(want.Phase) || math.Float32bits(got.yaw) != math.Float32bits(want.Yaw) {
			t.Errorf("phase/yaw=%v/%v, native=%v/%v", got.phase, got.yaw, want.Phase, want.Yaw)
		}
		for axis, v := range want.Velocity {
			if math.Float32bits(float32(got.velocity[axis])) != math.Float32bits(v) {
				t.Errorf("axis %d velocity=%v, native=%v", axis, got.velocity[axis], v)
			}
		}
	}
	for _, scene := range native.Scenarios {
		t.Run(fmt.Sprintf("seed%d", scene.Seed), func(t *testing.T) {
			if len(scene.Launches) != 200 {
				t.Fatal("missing consecutive MT boundary launches")
			}
			source := newLevelRandomSource(scene.Seed)
			for _, want := range scene.Launches {
				if len(want.Words) != 4 {
					t.Fatal("native constructor did not consume four TLS words")
				}
				used := 0
				got := nativeItemLaunchState(func() float32 {
					if used == 4 {
						t.Fatal("constructor consumed an unobserved TLS word")
					}
					word := source.Uint32()
					if word != want.Words[used] {
						t.Fatal("native constructor MT continuation differs")
					}
					used++
					return nativeRandomFloat32(word)
				})
				if used != 4 {
					t.Fatal("constructor skipped a phase/yaw/velocity draw")
				}
				compare(t, got, want)
				copy := *source
				if copy.Uint32() != want.NextWord {
					t.Fatal("constructor's next native MT word differs")
				}
			}
		})
	}
	for _, live := range native.Live {
		t.Run(fmt.Sprintf("live%v", live.Position), func(t *testing.T) {
			if live.FactoryYaw != 0 {
				t.Fatal("native factory did not replace the constructor yaw")
			}
			used := 0
			got := nativeItemLaunchState(func() float32 {
				if used == len(live.Launch.Words) {
					t.Fatal("live constructor consumed extra TLS words")
				}
				word := live.Launch.Words[used]
				used++
				return nativeRandomFloat32(word)
			})
			if used != 4 {
				t.Fatal("live constructor TLS budget differs")
			}
			compare(t, got, live.Launch)
		})
	}
}

type itemLaunchProbeType struct{ id string }

func (p itemLaunchProbeType) EncodeEntity() string                      { return p.id }
func (itemLaunchProbeType) Open(*Tx, *EntityHandle, *EntityData) Entity { return nil }
func (itemLaunchProbeType) BBox(Entity) cube.BBox                       { return cube.Box(0, 0, 0, .25, .25, .25) }
func (itemLaunchProbeType) DecodeNBT(map[string]any, *EntityData)       {}
func (itemLaunchProbeType) EncodeNBT(*EntityData) map[string]any        { return nil }

type itemLaunchProbeConfig struct{}

func (itemLaunchProbeConfig) Apply(*EntityData) {}

func TestItemSpawnOptionsApplyNativeTossAndPreserveExplicitMotion(t *testing.T) {
	for _, id := range []string{"minecraft:item", "minecraft:armor_stand"} {
		for _, toss := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/toss%t", id, toss), func(t *testing.T) {
				opts := EntitySpawnOpts{Position: mgl64.Vec3{4.5, 65.5, 8.5}, Velocity: mgl64.Vec3{1, 2, 3}, Rotation: cube.Rotation{45, 20}, ItemToss: toss}
				h := opts.New(itemLaunchProbeType{id}, itemLaunchProbeConfig{})
				if h.data.Pos != opts.Position || h.data.Rot != opts.Rotation {
					t.Fatal("native item initialisation changed explicit origin/rotation")
				}
				if id == "minecraft:item" && toss {
					if h.data.Vel[1] != float64(float32(.2)) {
						t.Fatal("item toss did not use native upward velocity")
					}
					for _, axis := range []int{0, 2} {
						if math.Abs(h.data.Vel[axis]) > float64(float32(.1)) {
							t.Fatal("native item toss exceeded horizontal range")
						}
					}
				} else if h.data.Vel != opts.Velocity {
					t.Fatal("explicit motion changed or item toss affected another entity type")
				}
			})
		}
	}
}

func TestItemConstructorLeavesFactoryZeroAndSavedMotionIntact(t *testing.T) {
	typ := itemLaunchProbeType{"minecraft:item"}
	for _, toss := range []bool{false, true} {
		h := EntitySpawnOpts{ItemToss: toss}.New(typ, itemLaunchProbeConfig{})
		if h.data.Rot != (cube.Rotation{}) {
			t.Fatal("constructor yaw overrode the factory's zero rotation")
		}
	}
	h := entityFromData(typ, 1, map[string]any{
		"Pos": []float32{4.5, 65.5, 8.5}, "Motion": []float32{.25, -.125, .5}, "Rotation": []float32{0, 0},
	})
	if h.data.Pos != (mgl64.Vec3{4.5, 65.5, 8.5}) || h.data.Vel != (mgl64.Vec3{.25, -.125, .5}) || h.data.Rot != (cube.Rotation{}) {
		t.Fatal("native item construction changed restored fields")
	}
}
