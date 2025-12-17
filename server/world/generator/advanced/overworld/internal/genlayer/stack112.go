package genlayer

type Stack struct {
	GenBiomes  Layer
	BiomeIndex Layer
}

// NewStack112 creates the GenLayer stack for Minecraft Java Edition 1.12.
// largeBiomes controls whether the additional zoom stages for the LARGE_BIOMES world type are enabled.
func NewStack112(worldSeed int64, largeBiomes bool) Stack {
	continent := &layerContinent{baseLayer: newBaseLayer(1)}
	zoomFuzzy := &layerZoom{baseLayer: newBaseLayer(2000), parent: continent, fuzzy: true}
	addIsland1 := &layerAddIsland{baseLayer: newBaseLayer(1), parent: zoomFuzzy}
	zoom2001 := &layerZoom{baseLayer: newBaseLayer(2001), parent: addIsland1}
	addIsland2 := &layerAddIsland{baseLayer: newBaseLayer(2), parent: zoom2001}
	addIsland50 := &layerAddIsland{baseLayer: newBaseLayer(50), parent: addIsland2}
	addIsland70 := &layerAddIsland{baseLayer: newBaseLayer(70), parent: addIsland50}
	removeTooMuchOcean := &layerRemoveTooMuchOcean{baseLayer: newBaseLayer(2), parent: addIsland70}
	addSnow := &layerAddSnow{baseLayer: newBaseLayer(2), parent: removeTooMuchOcean}
	addIsland3 := &layerAddIsland{baseLayer: newBaseLayer(3), parent: addSnow}
	coolWarm := &layerCoolWarm{baseLayer: newBaseLayer(2), parent: addIsland3}
	heatIce := &layerHeatIce{baseLayer: newBaseLayer(2), parent: coolWarm}
	special := &layerSpecial{baseLayer: newBaseLayer(3), parent: heatIce}
	zoom2002 := &layerZoom{baseLayer: newBaseLayer(2002), parent: special}
	zoom2003 := &layerZoom{baseLayer: newBaseLayer(2003), parent: zoom2002}
	addIsland4 := &layerAddIsland{baseLayer: newBaseLayer(4), parent: zoom2003}
	addMushroom := &layerAddMushroom{baseLayer: newBaseLayer(5), parent: addIsland4}
	deepOcean := &layerDeepOcean{baseLayer: newBaseLayer(4), parent: addMushroom}

	biomeLayer := &layerBiome{baseLayer: newBaseLayer(200), parent: deepOcean}
	zoom1000 := &layerZoom{baseLayer: newBaseLayer(1000), parent: biomeLayer}
	zoom1001 := &layerZoom{baseLayer: newBaseLayer(1001), parent: zoom1000}
	biomeEdge := &layerBiomeEdge{baseLayer: newBaseLayer(1000), parent: zoom1001}

	riverInit := &layerRiverInit{baseLayer: newBaseLayer(100), parent: deepOcean}
	hillsZoom128 := &layerZoom{baseLayer: newBaseLayer(0), parent: riverInit}
	hillsZoom64 := &layerZoom{baseLayer: newBaseLayer(0), parent: hillsZoom128}

	hills := &layerHills{baseLayer: newBaseLayer(1000), parent: biomeEdge, parent2: hillsZoom64}
	sunflower := &layerSunflower{baseLayer: newBaseLayer(1001), parent: hills}

	zoom32 := &layerZoom{baseLayer: newBaseLayer(1000), parent: sunflower}
	addIslandFinal := &layerAddIsland{baseLayer: newBaseLayer(3), parent: zoom32}
	zoom16 := &layerZoom{baseLayer: newBaseLayer(1001), parent: addIslandFinal}
	shore := &layerShore{baseLayer: newBaseLayer(1000), parent: zoom16}
	zoom8 := &layerZoom{baseLayer: newBaseLayer(1002), parent: shore}
	zoom4 := &layerZoom{baseLayer: newBaseLayer(1003), parent: zoom8}

	var main Layer = zoom4
	if largeBiomes {
		main = &layerZoom{baseLayer: newBaseLayer(1005), parent: &layerZoom{baseLayer: newBaseLayer(1004), parent: main}}
	}
	main = &layerSmooth{baseLayer: newBaseLayer(1000), parent: main}

	// River chain (scale 1:4).
	zoom128River := &layerZoom{baseLayer: newBaseLayer(1000), parent: riverInit}
	zoom64River := &layerZoom{baseLayer: newBaseLayer(1001), parent: zoom128River}
	zoom32River := &layerZoom{baseLayer: newBaseLayer(1000), parent: zoom64River}
	zoom16River := &layerZoom{baseLayer: newBaseLayer(1001), parent: zoom32River}
	zoom8River := &layerZoom{baseLayer: newBaseLayer(1002), parent: zoom16River}
	zoom4River := &layerZoom{baseLayer: newBaseLayer(1003), parent: zoom8River}
	river := &layerRiver{baseLayer: newBaseLayer(1), parent: zoom4River}
	riverSmooth := &layerSmooth{baseLayer: newBaseLayer(1000), parent: river}

	riverMix := &layerRiverMix{baseLayer: newBaseLayer(100), parent: main, parent2: riverSmooth}

	voronoi := &layerVoronoi114{baseLayer: newBaseLayer(10), parent: riverMix}

	visited := map[Layer]struct{}{}
	initLayerSeeds(voronoi, worldSeed, visited)

	return Stack{GenBiomes: riverMix, BiomeIndex: voronoi}
}

func initLayerSeeds(l Layer, worldSeed int64, visited map[Layer]struct{}) {
	if l == nil {
		return
	}
	if _, ok := visited[l]; ok {
		return
	}
	visited[l] = struct{}{}

	switch t := l.(type) {
	case *layerContinent:
		t.init(worldSeed)
	case *layerZoom:
		t.init(worldSeed)
		initLayerSeeds(t.parent, worldSeed, visited)
	case *layerAddIsland:
		t.init(worldSeed)
		initLayerSeeds(t.parent, worldSeed, visited)
	case *layerRemoveTooMuchOcean:
		t.init(worldSeed)
		initLayerSeeds(t.parent, worldSeed, visited)
	case *layerAddSnow:
		t.init(worldSeed)
		initLayerSeeds(t.parent, worldSeed, visited)
	case *layerCoolWarm:
		t.init(worldSeed)
		initLayerSeeds(t.parent, worldSeed, visited)
	case *layerHeatIce:
		t.init(worldSeed)
		initLayerSeeds(t.parent, worldSeed, visited)
	case *layerSpecial:
		t.init(worldSeed)
		initLayerSeeds(t.parent, worldSeed, visited)
	case *layerAddMushroom:
		t.init(worldSeed)
		initLayerSeeds(t.parent, worldSeed, visited)
	case *layerDeepOcean:
		t.init(worldSeed)
		initLayerSeeds(t.parent, worldSeed, visited)
	case *layerBiome:
		t.init(worldSeed)
		initLayerSeeds(t.parent, worldSeed, visited)
	case *layerBiomeEdge:
		t.init(worldSeed)
		initLayerSeeds(t.parent, worldSeed, visited)
	case *layerRiverInit:
		t.init(worldSeed)
		initLayerSeeds(t.parent, worldSeed, visited)
	case *layerHills:
		t.init(worldSeed)
		initLayerSeeds(t.parent, worldSeed, visited)
		initLayerSeeds(t.parent2, worldSeed, visited)
	case *layerSunflower:
		t.init(worldSeed)
		initLayerSeeds(t.parent, worldSeed, visited)
	case *layerShore:
		t.init(worldSeed)
		initLayerSeeds(t.parent, worldSeed, visited)
	case *layerRiver:
		t.init(worldSeed)
		initLayerSeeds(t.parent, worldSeed, visited)
	case *layerSmooth:
		t.init(worldSeed)
		initLayerSeeds(t.parent, worldSeed, visited)
	case *layerRiverMix:
		t.init(worldSeed)
		initLayerSeeds(t.parent, worldSeed, visited)
		initLayerSeeds(t.parent2, worldSeed, visited)
	case *layerVoronoi114:
		t.init(worldSeed)
		initLayerSeeds(t.parent, worldSeed, visited)
	default:
		// Unknown layer type.
	}
}
