package structure

import (
	"math"
)

// TerrainAdjustment is a Java StructureTerrainAdaptation value.
type TerrainAdjustment uint8

const (
	TerrainNone TerrainAdjustment = iota
	TerrainBury
	TerrainBeardThin
	TerrainBeardBox
	TerrainEncapsulate
)

// Box is an inclusive world-space structure bounding box.
type Box struct { Min, Max [3]int32 }

// RigidPiece is a piece contributing terrain adaptation to the Beardifier.
type RigidPiece struct { Bounds Box; Adjustment TerrainAdjustment; GroundLevelDelta int32 }

// Junction retains a source jigsaw connector's terrain and destination metadata.
type Junction struct { X, Y, Z, DeltaY int32; DestProjection Projection }

// Beardifier is the Java 26 structure terrain-adaptation sampler.
type Beardifier struct {
	pieces    []RigidPiece
	junctions []Junction
	// minY and maxY bound the heights any piece or junction adds to: every
	// contribution vanishes 12 blocks above and below its reference height.
	minY, maxY int32
}

var beardKernel [24*24*24]float32

func init() {
	for z := range 24 { for x := range 24 { for y := range 24 {
		dx, dy, dz := float64(x-12), float64(y-12)+0.5, float64(z-12)
		beardKernel[z*24*24+x*24+y] = float32(math.Pow(math.E,-(dx*dx+dy*dy+dz*dz)/16))
	} } }
}

// NewBeardifier snapshots rigid pieces and jigsaw junctions for one deterministic structure context.
func NewBeardifier(pieces []RigidPiece, junctions []Junction) *Beardifier {
	b := &Beardifier{pieces: append([]RigidPiece(nil), pieces...), junctions: append([]Junction(nil), junctions...)}
	b.bound()
	return b
}

// bound computes the heights the pieces and junctions of b add to.
func (b *Beardifier) bound() {
	b.minY, b.maxY = math.MaxInt32, math.MinInt32
	include := func(low, high int32) {
		b.minY, b.maxY = min(b.minY, low-12), max(b.maxY, high+12)
	}
	for _, piece := range b.pieces {
		box := piece.Bounds
		groundY := box.Min[1] + piece.GroundLevelDelta
		switch piece.Adjustment {
		case TerrainBury, TerrainBeardThin:
			include(groundY, groundY)
		case TerrainBeardBox:
			include(groundY, box.Max[1])
		case TerrainEncapsulate:
			include(box.Min[1], box.Max[1])
		}
	}
	for _, junction := range b.junctions {
		include(junction.Y, junction.Y)
	}
}

// NewChunkBeardifier returns the Beardifier of the chunk at chunkX, chunkZ. As
// Beardifier.forStructuresInChunk, it keeps the pieces within 12 blocks of the
// chunk and the junctions whose source lies within 12 blocks of it: the others
// add nothing to the densities of the chunk.
func NewChunkBeardifier(chunkX, chunkZ int32, pieces []RigidPiece, junctions []Junction) *Beardifier {
	minX, minZ := chunkX*16, chunkZ*16
	maxX, maxZ := minX+15, minZ+15
	b := &Beardifier{}
	for _, piece := range pieces {
		box := piece.Bounds
		if box.Max[0] >= minX-12 && box.Min[0] <= maxX+12 && box.Max[2] >= minZ-12 && box.Min[2] <= maxZ+12 {
			b.pieces = append(b.pieces, piece)
		}
	}
	for _, junction := range junctions {
		if junction.X > minX-12 && junction.Z > minZ-12 && junction.X < maxX+12 && junction.Z < maxZ+12 {
			b.junctions = append(b.junctions, junction)
		}
	}
	b.bound()
	return b
}

// AdjustDensity returns the Java Beardifier density contribution at the block coordinate.
func (b *Beardifier) AdjustDensity(x,y,z int32) float32 {
	if b == nil || y < b.minY || y > b.maxY { return 0 }
	var value float32
	for _, piece := range b.pieces {
		box := piece.Bounds
		dx := max(int32(0),max(box.Min[0]-x,x-box.Max[0]))
		dz := max(int32(0),max(box.Min[2]-z,z-box.Max[2]))
		if dx >= 12 || dz >= 12 {
			// Every adjustment adds nothing this far from the piece.
			continue
		}
		groundY := box.Min[1]+piece.GroundLevelDelta
		dyToGround := y-groundY
		var dy int32
		switch piece.Adjustment {
		case TerrainNone:
			continue
		case TerrainBury, TerrainBeardThin:
			dy = dyToGround
		case TerrainBeardBox:
			dy = max(int32(0),max(groundY-y,y-box.Max[1]))
		case TerrainEncapsulate:
			dy = max(int32(0),max(box.Min[1]-y,y-box.Max[1]))
		default:
			continue
		}
		switch piece.Adjustment {
		case TerrainBury:
			value += buryContribution(float32(dx),float32(dy)/2,float32(dz))
		case TerrainBeardThin, TerrainBeardBox:
			value += beardContribution(dx,dy,dz,dyToGround)*0.8
		case TerrainEncapsulate:
			value += buryContribution(float32(dx)/2,float32(dy)/2,float32(dz)/2)*0.8
		}
	}
	for _, junction := range b.junctions {
		value += beardContribution(x-junction.X,y-junction.Y,z-junction.Z,y-junction.Y)*0.4
	}
	return value
}

func buryContribution(dx,dy,dz float32) float32 {
	distance := dx*dx+dy*dy+dz*dz
	if distance >= 36 { return 0 }
	return 1-float32(math.Sqrt(float64(distance)))/6
}

func beardContribution(dx,dy,dz,yToGround int32) float32 {
	xi, yi, zi := dx+12,dy+12,dz+12
	if xi<0 || xi>=24 || yi<0 || yi>=24 || zi<0 || zi>=24 { return 0 }
	dyOffset := float32(yToGround)+0.5
	distance := float32(dx)*float32(dx)+dyOffset*dyOffset+float32(dz)*float32(dz)
	inv := fastInvSqrt(float64(distance/2))
	v := -dyOffset*float32(inv)/2
	return v*beardKernel[int(zi)*24*24+int(xi)*24+int(yi)]
}

func fastInvSqrt(value float64) float64 {
	half := 0.5*value
	bitsValue := math.Float64bits(value)
	bitsValue = 6910469410427058090 - (bitsValue >> 1)
	x := math.Float64frombits(bitsValue)
	return x*(1.5-half*x*x)
}

