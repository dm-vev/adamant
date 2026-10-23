package gamedata

import "testing"

func TestGenerationFaceStateBoundaries(t *testing.T) {
	// Captured from executed Java 26.3 support/collision shapes and SupportType.FULL.
	for _, tc := range []struct {
		name               string
		properties         map[string]any
		attachment, sturdy uint8
	}{
		{"stone", nil, 63, 63},
		{"oak_leaves", nil, 63, 0},
		{"oak_slab", map[string]any{"type": "bottom"}, 1, 1},
		{"oak_slab", map[string]any{"type": "top"}, 2, 2},
		{"oak_slab", map[string]any{"type": "double"}, 63, 63},
		{"snow", map[string]any{"layers": int32(7)}, 1, 1},
		{"snow", map[string]any{"layers": int32(8)}, 63, 63},
		{"oak_trapdoor", map[string]any{"facing": "north", "open": true}, 8, 8},
		{"oak_trapdoor", map[string]any{"facing": "south", "open": true}, 4, 4},
		{"oak_trapdoor", map[string]any{"facing": "west", "open": true}, 32, 32},
		{"oak_trapdoor", map[string]any{"facing": "east", "open": true}, 16, 16},
		{"oak_trapdoor", map[string]any{"half": "top", "open": false}, 2, 2},
		{"oak_trapdoor", map[string]any{"half": "bottom", "open": false}, 1, 1},
	} {
		shape, err := BlockGenerationShape(tc.name, tc.properties)
		if err != nil || shape.Attachment != tc.attachment || shape.Sturdy != tc.sturdy {
			t.Errorf("%s %v: masks=%d/%d, want %d/%d, error=%v", tc.name, tc.properties, shape.Attachment, shape.Sturdy, tc.attachment, tc.sturdy, err)
		}
	}
}

func TestGenerationCollisionSupportAndReplacement(t *testing.T) {
	// Executed source results distinguish support faces from collision volume
	// and collision emptiness from replaceability.
	for _, tc := range []struct {
		name                     string
		properties               map[string]any
		center                   uint8
		full, empty, replaceable bool
	}{
		{"air", nil, 0, false, true, true},
		{"stone", nil, 63, true, false, false},
		{"dandelion", nil, 0, false, true, false},
		{"snow", map[string]any{"layers": int32(1)}, 1, false, true, true},
		{"snow", map[string]any{"layers": int32(2)}, 1, false, false, true},
		{"snow", map[string]any{"layers": int32(8)}, 63, false, false, true},
		{"oak_fence", nil, 3, false, false, false},
	} {
		shape, err := BlockGenerationShape(tc.name, tc.properties)
		if err != nil || shape.Center != tc.center || shape.CollisionFull != tc.full ||
			shape.CollisionEmpty != tc.empty || shape.Replaceable != tc.replaceable {
			t.Errorf("%s %v: got %+v error=%v, want center=%d full=%v empty=%v replaceable=%v",
				tc.name, tc.properties, shape, err, tc.center, tc.full, tc.empty, tc.replaceable)
		}
	}
}

func TestCollisionFaceRectangleUnionCoverage(t *testing.T) {
	// Complementary half-face rectangles cover exactly; a gap or an
	// out-of-bounds partial face does not. This exercises union coverage rather
	// than requiring either individual shape to be a full face.
	rect := func(minU, minV, maxU, maxV float64) generationCollisionRect {
		return generationCollisionRect{minU: minU, minV: minV, maxU: maxU, maxV: maxV}
	}
	for _, tc := range []struct {
		name          string
		first, second []generationCollisionRect
		want          bool
	}{
		{"complementary slabs", []generationCollisionRect{rect(0, 0, 0.5, 1)}, []generationCollisionRect{rect(0.5, 0, 1, 1)}, true},
		{"stair-like complementary", []generationCollisionRect{rect(0, 0, 1, 0.5)}, []generationCollisionRect{rect(0, 0.5, 1, 1)}, true},
		{"gap", []generationCollisionRect{rect(0, 0, 0.49, 1)}, []generationCollisionRect{rect(0.51, 0, 1, 1)}, false},
		{"partial bounds", []generationCollisionRect{rect(0.1, 0, 1, 1)}, nil, false},
		{"overlap but incomplete", []generationCollisionRect{rect(0, 0, 0.75, 1)}, []generationCollisionRect{rect(0.5, 0, 0.9, 1)}, false},
	} {
		if got := collisionRectanglesCover(tc.first, tc.second); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestCollisionFaceRegionRectangleCoverage(t *testing.T) {
	rect := func(minU, minV, maxU, maxV float64) generationCollisionRect {
		return generationCollisionRect{minU: minU, minV: minV, maxU: maxU, maxV: maxV}
	}
	for _, tc := range []struct {
		name                   string
		rectangles             []generationCollisionRect
		minU, minV, maxU, maxV float64
		want                   bool
	}{
		{"complementary region boxes", []generationCollisionRect{rect(0, 0, 0.5, 1), rect(0.5, 0, 1, 1)}, 0.1, 0.2, 0.9, 0.8, true},
		{"hole in requested region", []generationCollisionRect{rect(0, 0, 0.4, 1), rect(0.6, 0, 1, 1)}, 0.1, 0, 0.9, 1, false},
		{"rectangle outside face", []generationCollisionRect{rect(0.2, 0.2, 0.8, 0.8)}, 0, 0, 1, 1, false},
		{"exact bounds", []generationCollisionRect{rect(0.25, 0.25, 0.75, 0.75)}, 0.25, 0.25, 0.75, 0.75, true},
	} {
		if got := collisionRectanglesCoverBounds(tc.rectangles, tc.minU, tc.minV, tc.maxU, tc.maxV); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
func TestCollisionFaceQueriesAndConnectionException(t *testing.T) {
	stoneID, err := BlockStateID("stone", nil)
	if err != nil {
		t.Fatal(err)
	}
	flowerID, err := BlockStateID("dandelion", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name             string
		id               uint32
		face             int
		nonEmpty, covers bool
	}{
		{"stone top", stoneID, 1, true, true},
		{"flower top", flowerID, 1, false, false},
	} {
		nonEmpty, err := GenerationCollisionFaceNonEmptyID(tc.id, tc.face)
		if err != nil || nonEmpty != tc.nonEmpty {
			t.Errorf("%s non-empty: got %v, err=%v, want %v", tc.name, nonEmpty, err, tc.nonEmpty)
		}
		covers, err := GenerationCollisionFaceCoversRectangleID(tc.id, tc.face, 0, 0, 1, 1)
		if err != nil || covers != tc.covers {
			t.Errorf("%s coverage: got %v, err=%v, want %v", tc.name, covers, err, tc.covers)
		}
	}
	for _, tc := range []struct {
		name string
		want bool
	}{
		{"stone", false},
		{"oak_leaves", true},
		{"barrier", true},
		{"carved_pumpkin", true},
	} {
		id, err := BlockStateID(tc.name, nil)
		if err != nil {
			t.Fatal(err)
		}
		shape, err := BlockGenerationShapeID(id)
		if err != nil || shape.ConnectionException != tc.want {
			t.Errorf("%s exception: got %v, err=%v, want %v", tc.name, shape.ConnectionException, err, tc.want)
		}
	}
}
func TestBlockStateSignalSourceExport(t *testing.T) {
	for _, tc := range []struct {
		name string
		want bool
	}{
		{"stone", false},
		{"redstone_torch", true},
		{"lever", true},
	} {
		id, err := BlockStateID(tc.name, nil)
		if err != nil {
			t.Fatal(err)
		}
		got, err := BlockSignalSourceID(id)
		if err != nil || got != tc.want {
			t.Errorf("%s signal source: got %v, err=%v, want %v", tc.name, got, err, tc.want)
		}
	}
}
func TestSourceConductorFireAndInstrumentLookups(t *testing.T) {
	for _, tc := range []struct {
		name               string
		properties         map[string]any
		conductor, canBurn bool
		instrument         string
	}{
		{"stone", nil, true, false, "basedrum"},
		{"oak_planks", nil, true, true, "bass"},
		{"oak_slab", map[string]any{"type": "bottom"}, false, true, "bass"},
		{"oak_slab", map[string]any{"type": "bottom", "waterlogged": true}, false, false, "bass"},
	} {
		id, err := BlockStateID(tc.name, tc.properties)
		if err != nil {
			t.Fatal(err)
		}
		conductor, err := BlockRedstoneConductorID(id)
		if err != nil || conductor != tc.conductor {
			t.Errorf("%s conductor: got %v, err=%v, want %v", tc.name, conductor, err, tc.conductor)
		}
		canBurn, err := BlockCanBurnID(id)
		if err != nil || canBurn != tc.canBurn {
			t.Errorf("%s canBurn: got %v, err=%v, want %v", tc.name, canBurn, err, tc.canBurn)
		}
		instrument, err := BlockInstrumentID(id)
		if err != nil || instrument != tc.instrument {
			t.Errorf("%s instrument: got %q, err=%v, want %q", tc.name, instrument, err, tc.instrument)
		}
	}
}
