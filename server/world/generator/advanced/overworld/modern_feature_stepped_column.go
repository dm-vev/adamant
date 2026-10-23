package overworld

import vanilla "github.com/df-mc/dragonfly/server/internal/vanilla"

type steppedColumnCluster struct {
	region                      *featureRegion
	block, cannotPlaceOn        any
	canReplace, continueThrough map[string]any
}

func placeModernSteppedColumnCluster(region *featureRegion, config map[string]any, origin featurePos, random *vanilla.Random) bool {
	cluster := steppedColumnCluster{
		region: region, block: config["block"], cannotPlaceOn: config["cannot_place_on"],
		canReplace: config["can_replace"].(map[string]any), continueThrough: config["continue_through"].(map[string]any),
	}
	if !cluster.canPlaceAt(origin) {
		return false
	}
	height := sampleInt(config["height"], random)
	reach := min(height, sampleInt(config["cluster_reach"], random))
	count := sampleInt(config["column_count"], random)
	placed := false
	for range count {
		// randomBetweenClosed draws X, Y and Z, including the constant Y draw.
		p := featurePos{origin.x - reach + random.NextInt(2*reach+1), origin.y + random.NextInt(1), origin.z - reach + random.NextInt(2*reach+1)}
		columnHeight := height - absI32(p.x-origin.x) - absI32(p.z-origin.z)
		if columnHeight < 0 {
			continue
		}
		columnReach := sampleInt(config["column_reach"], random)
		placed = cluster.placeColumn(p, columnHeight, columnReach, random) || placed
	}
	return placed
}

func (c *steppedColumnCluster) canPlaceAt(p featurePos) bool {
	if !featurePredicate(c.region, p, c.canReplace) {
		return false
	}
	below := featurePos{p.x, p.y - 1, p.z}
	name, _ := c.region.State(below)
	return !treeIsAirName(name) && !featureBlockHolderContains(c.cannotPlaceOn, name)
}

func (c *steppedColumnCluster) placeColumn(origin featurePos, height, reach int32, random *vanilla.Random) bool {
	placed := false
	for dz := -reach; dz <= reach; dz++ {
		for dx := -reach; dx <= reach; dx++ {
			p := featurePos{origin.x + dx, origin.y, origin.z + dz}
			limit := absI32(dx) + absI32(dz)
			var found bool
			if featurePredicate(c.region, p, c.canReplace) {
				p, found = c.findSurface(p, limit)
			} else {
				p, found = c.findAir(p, limit)
			}
			if !found {
				continue
			}
			for remaining := height - limit/2; remaining >= 0; remaining-- {
				if featurePredicate(c.region, p, c.canReplace) {
					state, actor, ok := c.region.SampleNativeState(c.block, p, random)
					if !ok {
						panic("overworld: unresolved stepped-column state provider")
					}
					c.region.SetNativeBlock(p, state, actor)
					placed = true
				} else if !featurePredicate(c.region, p, c.continueThrough) {
					break
				}
				p.y++
			}
		}
	}
	return placed
}

func (c *steppedColumnCluster) findSurface(p featurePos, limit int32) (featurePos, bool) {
	for p.y > c.region.MinY()+1 && limit > 0 {
		limit--
		if c.canPlaceAt(p) {
			return p, true
		}
		p.y--
	}
	return p, false
}

func (c *steppedColumnCluster) findAir(p featurePos, limit int32) (featurePos, bool) {
	for p.y <= c.region.MaxY() && limit > 0 {
		limit--
		name, _ := c.region.State(p)
		if featureBlockHolderContains(c.cannotPlaceOn, name) {
			return p, false
		}
		if treeIsAirName(name) {
			return p, true
		}
		p.y++
	}
	return p, false
}
