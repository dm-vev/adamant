package overworld

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	java26 "github.com/df-mc/dragonfly/server/internal/java26"
)

type strongholdGraphFixture struct {
	Seed         int64    `json:"seed"`
	Chunk        [2]int32 `json:"chunk"`
	PieceCount   int      `json:"piece_count"`
	PiecesSHA256 string   `json:"pieces_sha256"`
	NextLong     int64    `json:"next_long"`
}

type strongholdGraphPiece struct {
	Box       [6]int32 `json:"box"`
	Depth     int      `json:"depth"`
	Dir       string   `json:"dir"`
	Door      string   `json:"door"`
	Kind      string   `json:"kind"`
	LeftHigh  bool     `json:"left_high"`
	LeftLow   bool     `json:"left_low"`
	RightHigh bool     `json:"right_high"`
	RightLow  bool     `json:"right_low"`
	Variant   int      `json:"variant"`
}

func TestStrongholdPieceGraphsMatchJava263(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_stronghold_graphs.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []strongholdGraphFixture
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		fixture := fixture
		t.Run(fmt.Sprintf("seed_%d_chunk_%d_%d", fixture.Seed, fixture.Chunk[0], fixture.Chunk[1]), func(t *testing.T) {
			random := java26.NewLegacyRandom(fixture.Seed)
			start, valid, err := buildModernStronghold(modernStructureBuildInput{seed: fixture.Seed, name: "stronghold", chunkX: fixture.Chunk[0], chunkZ: fixture.Chunk[1], random: &random})
			if err != nil {
				t.Fatal(err)
			}
			if !valid {
				t.Fatal("source graph was not generated")
			}
			pieces := make([]strongholdGraphPiece, len(start.pieces))
			for i, raw := range start.pieces {
				p := raw.(strongholdPiece)
				kind, ok := strongholdSourcePieceKind[p.kind]
				if !ok {
					t.Fatalf("unmapped source piece kind %q", p.kind)
				}
				pieces[i] = strongholdGraphPiece{Box: [6]int32{p.box.Min[0], p.box.Min[1], p.box.Min[2], p.box.Max[0], p.box.Max[1], p.box.Max[2]}, Depth: p.depth, Dir: p.dir, Door: strongholdSourceDoor(p.door), Kind: kind, LeftHigh: p.leftHigh, LeftLow: p.leftLow, RightHigh: p.rightHigh, RightLow: p.rightLow, Variant: p.variant}
			}
			encoded, err := json.Marshal(pieces)
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(encoded)
			if len(pieces) != fixture.PieceCount {
				t.Errorf("piece count = %d, want %d", len(pieces), fixture.PieceCount)
			}
			if got := hex.EncodeToString(sum[:]); got != fixture.PiecesSHA256 {
				t.Errorf("ordered Java piece graph SHA-256 = %s, want %s", got, fixture.PiecesSHA256)
			}
			if got := random.NextLong(); got != fixture.NextLong {
				t.Errorf("post-generation Java RNG nextLong = %d, want %d", got, fixture.NextLong)
			}
		})
	}
}

var strongholdSourcePieceKind = map[string]string{
	"start": "minecraft:shstart", "five": "minecraft:sh5c", "stairs": "minecraft:shsd", "straight": "minecraft:shs", "prison": "minecraft:shph", "turn_left": "minecraft:shlt", "turn_right": "minecraft:shrt", "crossing": "minecraft:shrc", "straight_stairs": "minecraft:shssd", "filler": "minecraft:shfc", "chest": "minecraft:shcc", "library": "minecraft:shli", "portal": "minecraft:shpr",
}

func strongholdSourceDoor(door string) string {
	switch door {
	case "", "opening":
		return "OPENING"
	case "wood":
		return "WOOD_DOOR"
	case "grates":
		return "GRATES"
	case "iron":
		return "IRON_DOOR"
	default:
		return door
	}
}
