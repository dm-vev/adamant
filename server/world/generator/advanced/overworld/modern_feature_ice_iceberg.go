package overworld

import (
	java26 "github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/world"
	"math"
)

func placeModernIceberg(region *featureRegion, config map[string]any, origin featurePos, random *java26.Random) bool {
	stateName, ok := config["state"].(string)
	if !ok {
		panic("overworld: malformed Java iceberg state")
	}
	mainRID := featureRequireState(region, "minecraft:"+normalizeJavaResource(stateName))
	snowBlockRID := featureRequireState(region, "minecraft:snow_block")
	waterRID := featureRequireState(region, "minecraft:water")
	airRID := featureRequireState(region, "minecraft:air")
	origin.y = region.f.source.SeaLevel()
	snowOnTop := random.NextDouble() > 0.7
	shapeAngle := random.NextDouble() * 2 * math.Pi
	shapeA := int32(11 - random.NextInt(5))
	shapeC := int32(3 + random.NextInt(3))
	ellipse := random.NextDouble() > 0.7
	width := int32(11)
	overHeight := int32(0)
	if ellipse {
		overHeight = random.NextInt(6) + 6
	} else {
		overHeight = random.NextInt(15) + 3
	}
	if !ellipse && random.NextDouble() > 0.9 {
		overHeight += random.NextInt(19) + 7
	}
	underHeight := min(overHeight+random.NextInt(11), int32(18))
	width = min(overHeight+random.NextInt(7)-random.NextInt(5), width)
	a := int32(11)
	if ellipse {
		a = shapeA
	}
	shapeSin, shapeCos := 0.0, 0.0
	if ellipse {
		shapeSin, shapeCos = icebergSinCos(shapeAngle)
	}
	for x := -a; x < a; x++ {
		for z := -a; z < a; z++ {
			for y := int32(0); y < overHeight; y++ {
				radius := int32(0)
				if ellipse {
					radius = icebergEllipseRadius(y, overHeight, width)
				} else {
					radius = icebergRoundRadius(random, y, overHeight, width)
				}
				if ellipse || x < radius {
					placeIcebergVoxel(region, origin, random, overHeight, x, y, z, radius, a, ellipse, shapeC, shapeSin, shapeCos, snowOnTop, mainRID, snowBlockRID)
				}
			}
		}
	}
	icebergSmooth(region, origin, width, overHeight, ellipse, shapeA, airRID)
	for x := -a; x < a; x++ {
		for z := -a; z < a; z++ {
			for y := int32(-1); y > -underHeight; y-- {
				newA := a
				if ellipse {
					scale := float32(a) * (1 - float32(math.Pow(float64(y), 2))/(float32(underHeight)*8))
					newA = int32(math.Ceil(float64(scale)))
				}
				radius := icebergSteepRadius(random, -y, underHeight, width)
				if x < radius {
					placeIcebergVoxel(region, origin, random, underHeight, x, y, z, radius, newA, ellipse, shapeC, shapeSin, shapeCos, snowOnTop, mainRID, snowBlockRID)
				}
			}
		}
	}
	doCutOut := false
	if ellipse {
		doCutOut = random.NextDouble() > 0.1
	} else {
		doCutOut = random.NextDouble() > 0.7
	}
	if doCutOut {
		icebergCutOut(region, random, width, overHeight, origin, ellipse, shapeA, shapeAngle, shapeC, waterRID, airRID)
	}
	return true
}

func placeIcebergVoxel(region *featureRegion, origin featurePos, random *java26.Random, height, x, y, z, radius, a int32, ellipse bool, c int32, sin, cos float64, snowOnTop bool, main, snowBlock world.NativeState) {
	signed := 0.0
	if ellipse {
		signed = icebergEllipseDistance(x, z, 0, 0, a, icebergEllipseC(y, height, c), sin, cos)
	} else {
		signed = icebergCircleDistance(random, x, z, radius)
	}
	if !(signed < 0) {
		return
	}
	p := featurePos{origin.x + x, origin.y + y, origin.z + z}
	compare := -0.5
	if !ellipse {
		compare = -6 - float64(random.NextInt(3))
	}
	if signed > compare && random.NextDouble() > 0.9 {
		return
	}
	name, _ := region.State(p)
	if !featureRegionAir(region, p) && name != "minecraft:snow_block" && name != "minecraft:ice" && name != "minecraft:water" {
		return
	}
	randomness := true
	if ellipse {
		randomness = random.NextDouble() > 0.05
	}
	divisor := int32(2)
	if ellipse {
		divisor = 3
	}
	if snowOnTop && name != "minecraft:water" && float64(height-y) <= float64(random.NextInt(max(1, height/divisor)))+float64(height)*0.6 && randomness {
		region.Set(p, snowBlock)
	} else {
		region.Set(p, main)
	}
}
func icebergEllipseDistance(x, z, ox, oz, a, c int32, sin, cos float64) float64 {
	xc, zs := float64(float64(x-ox)*cos), float64(float64(z-oz)*sin)
	xs, zc := float64(float64(x-ox)*sin), float64(float64(z-oz)*cos)
	u := float64(float64(xc-zs) / float64(a))
	v := float64(float64(xs+zc) / float64(c))
	u2, v2 := float64(u*u), float64(v*v)
	return float64(float64(u2+v2) - 1)
}
func icebergRoundRadius(r *java26.Random, y, height, width int32) int32 {
	k := float32(3.5) - r.NextFloat()
	scale := (1 - float32(math.Pow(float64(y), 2))/(float32(height)*k)) * float32(width)
	if height > 15+r.NextInt(5) {
		tempY := y
		if y < 3+r.NextInt(6) {
			tempY = y / 2
		}
		scale = (1 - float32(tempY)/(float32(height)*k*0.4)) * float32(width)
	}
	return int32(math.Ceil(float64(scale / 2)))
}
func icebergEllipseRadius(y, height, width int32) int32 {
	scale := (1 - float32(math.Pow(float64(y), 2))/float32(height)) * float32(width)
	return int32(math.Ceil(float64(scale / 2)))
}
func icebergSteepRadius(r *java26.Random, y, height, width int32) int32 {
	k := float32(1) + r.NextFloat()/2
	scale := (1 - float32(y)/(float32(height)*k)) * float32(width)
	return int32(math.Ceil(float64(scale / 2)))
}

func icebergSmooth(region *featureRegion, origin featurePos, width, height int32, ellipse bool, a int32, air world.NativeState) {
	radius := width / 2
	if ellipse {
		radius = a
	}
	for x := -radius; x <= radius; x++ {
		for z := -radius; z <= radius; z++ {
			for y := int32(0); y <= height; y++ {
				p := featurePos{origin.x + x, origin.y + y, origin.z + z}
				name, _ := region.State(p)
				if !isIcebergBlock(name) && name != "minecraft:snow" {
					continue
				}
				below := featurePos{p.x, p.y - 1, p.z}
				if featureRegionAir(region, below) {
					region.Set(p, air)
					region.Set(featurePos{p.x, p.y + 1, p.z}, air)
					continue
				}
				if isIcebergBlock(name) {
					count := 0
					for _, d := range [][3]int32{{-1, 0, 0}, {1, 0, 0}, {0, 0, -1}, {0, 0, 1}} {
						neighbor, _ := region.State(featurePos{p.x + d[0], p.y, p.z + d[2]})
						if !isIcebergBlock(neighbor) {
							count++
						}
					}
					if count >= 3 {
						region.Set(p, air)
					}
				}
			}
		}
	}
}
func isIcebergBlock(name string) bool {
	return name == "minecraft:packed_ice" || name == "minecraft:snow_block" || name == "minecraft:blue_ice"
}
func icebergCutOut(region *featureRegion, r *java26.Random, width, height int32, origin featurePos, ellipse bool, a int32, angle float64, c int32, water, air world.NativeState) {
	sx := int32(1)
	if r.NextBoolean() {
		sx = -1
	}
	sz := int32(1)
	if r.NextBoolean() {
		sz = -1
	}
	xoff := r.NextInt(max(width/2-2, int32(1)))
	if r.NextBoolean() {
		xoff = width/2 + 1 - r.NextInt(max(width-width/2-1, int32(1)))
	}
	zoff := r.NextInt(max(width/2-2, int32(1)))
	if r.NextBoolean() {
		zoff = width/2 + 1 - r.NextInt(max(width-width/2-1, int32(1)))
	}
	if ellipse {
		xoff = r.NextInt(max(a-5, int32(1)))
		zoff = xoff
	}
	localX, localZ := sx*xoff, sz*zoff
	cutAngle := angle + math.Pi/2
	if !ellipse {
		cutAngle = r.NextDouble() * 2 * math.Pi
	}
	cutSin, cutCos := icebergSinCos(cutAngle)
	for y := int32(0); y < height-3; y++ {
		radius := icebergRoundRadius(r, y, height, width)
		icebergCarve(region, radius, y, origin, false, cutSin, cutCos, localX, localZ, a, c, water, air)
	}
	for y := int32(-1); ; y-- {
		if y <= -height+r.NextInt(5) {
			break
		}
		radius := icebergSteepRadius(r, -y, height, width)
		icebergCarve(region, radius, y, origin, true, cutSin, cutCos, localX, localZ, a, c, water, air)
	}
}
func icebergCircleDistance(random *java26.Random, x, z, radius int32) float64 {
	offset := random.NextFloat()
	if offset < 0.2 {
		offset = 0.2
	}
	if offset > 0.8 {
		offset = 0.8
	}
	noise := float32(10) * offset / float32(radius)
	return float64(noise) + math.Pow(float64(x), 2) + math.Pow(float64(z), 2) - math.Pow(float64(radius), 2)
}
func icebergCarve(region *featureRegion, radius, y int32, origin featurePos, under bool, sin, cos float64, lx, lz, a, c int32, water, air world.NativeState) {
	aa := radius + 1 + a/3
	cc := min(radius-3, int32(3)) + c/2 - 1
	for x := -aa; x < aa; x++ {
		for z := -aa; z < aa; z++ {
			if !(icebergEllipseDistance(x, z, lx, lz, aa, cc, sin, cos) < 0) {
				continue
			}
			p := featurePos{origin.x + x, origin.y + y, origin.z + z}
			name, _ := region.State(p)
			if !isIcebergBlock(name) && name != "minecraft:snow_block" {
				continue
			}
			if under {
				region.Set(p, water)
			} else {
				region.Set(p, air)
				above := featurePos{p.x, p.y + 1, p.z}
				aboveName, _ := region.State(above)
				if aboveName == "minecraft:snow" {
					region.Set(above, air)
				}
			}
		}
	}
}
func icebergEllipseC(y, height, c int32) int32 {
	result := c
	if y > 0 && height-y <= 3 {
		result -= 4 - (height - y)
	}
	return result
}
