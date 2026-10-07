package javastructure

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

// Junction stores the source coordinates of a jigsaw connector's density contribution.
type Junction struct { X, Y, Z int32 }

// Beardifier is the Java 26 structure terrain-adaptation sampler.
type Beardifier struct { pieces []RigidPiece; junctions []Junction }

var beardKernel [24*24*24]float32

func init() {
	for z := range 24 { for x := range 24 { for y := range 24 {
		dx, dy, dz := float64(x-12), float64(y-12)+0.5, float64(z-12)
		beardKernel[z*24*24+x*24+y] = float32(math.Pow(math.E,-(dx*dx+dy*dy+dz*dz)/16))
	} } }
}

// NewBeardifier snapshots rigid pieces and jigsaw junctions for one deterministic structure context.
func NewBeardifier(pieces []RigidPiece, junctions []Junction) *Beardifier {
	return &Beardifier{pieces: append([]RigidPiece(nil), pieces...), junctions: append([]Junction(nil), junctions...)}
}

// AdjustDensity returns the Java Beardifier density contribution at the block coordinate.
func (b *Beardifier) AdjustDensity(x,y,z int32) float32 {
	if b == nil { return 0 }
	var value float32
	for _, piece := range b.pieces {
		box := piece.Bounds
		dx := max(int32(0),max(box.Min[0]-x,x-box.Max[0]))
		dz := max(int32(0),max(box.Min[2]-z,z-box.Max[2]))
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

