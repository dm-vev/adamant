package nether

import (
	"fmt"
	"sync"

	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/structure"
)

// netherPortalStart contains only immutable Java generation metadata. Placement is
// deliberately performed later with the decoration-step random source.
type netherPortalStart struct {
	box          structure.Box
	template     *structure.Template
	templateName string
	origin       [3]int32
	pivot        [3]int32
	rotation     structure.Rotation
	mirror       structure.Mirror
	airPocket    bool
	mossiness    float32
	overgrown    bool
	vines        bool
	cold         bool
	blackstone   bool
}

type netherPortalStartResult struct {
	start *netherPortalStart
	err   error
}
type netherPortalStartFlight struct {
	done   chan struct{}
	result netherPortalStartResult
}

// netherPortalCache is usable from a zero-value Nether and bounds retained starts.
type netherPortalCache struct {
	mu      sync.Mutex
	starts  map[world.ChunkPos]netherPortalStartResult
	flights map[world.ChunkPos]*netherPortalStartFlight
}

func (g *Nether) portalStart(cx, cz int32) (*netherPortalStart, error) {
	candidate, err := structure.PotentialChunk(g.seed, cx, cz, 34222645, 40, 15, structure.Linear)
	if err != nil {
		return nil, err
	}
	if candidate != [2]int32{cx, cz} {
		return nil, nil
	}
	key := world.ChunkPos{cx, cz}
	cache := &g.portals
	cache.mu.Lock()
	if result, ok := cache.starts[key]; ok {
		cache.mu.Unlock()
		return result.start, result.err
	}
	if flight := cache.flights[key]; flight != nil {
		cache.mu.Unlock()
		<-flight.done
		return flight.result.start, flight.result.err
	}
	if cache.flights == nil {
		cache.flights = make(map[world.ChunkPos]*netherPortalStartFlight)
	}
	flight := &netherPortalStartFlight{done: make(chan struct{})}
	cache.flights[key] = flight
	cache.mu.Unlock()

	start, err := g.buildNetherPortalStart(cx, cz)
	result := netherPortalStartResult{start: start, err: err}
	cache.mu.Lock()
	if cache.starts == nil {
		cache.starts = make(map[world.ChunkPos]netherPortalStartResult)
	}
	if len(cache.starts) >= 256 {
		for old := range cache.starts {
			delete(cache.starts, old)
			break
		}
	}
	cache.starts[key] = result
	delete(cache.flights, key)
	flight.result = result
	close(flight.done)
	cache.mu.Unlock()
	return start, err
}

func (g *Nether) buildNetherPortalStart(cx, cz int32) (start *netherPortalStart, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			start, err = nil, fmt.Errorf("build Nether ruined portal start at %d,%d: %v", cx, cz, recovered)
		}
	}()
	random := largeFeatureRandom(g.seed, int(cx), int(cz))
	const airPocket = false
	giant := random.NextFloat() < 0.05
	var templateName string
	if giant {
		templateName = fmt.Sprintf("ruined_portal/giant_portal_%d", random.NextInt(3)+1)
	} else {
		templateName = fmt.Sprintf("ruined_portal/portal_%d", random.NextInt(10)+1)
	}
	template := loadNetherTemplate(templateName)
	rotation := structure.Rotation(random.NextInt(4))
	mirror := structure.NoMirror
	if random.NextFloat() >= 0.5 {
		mirror = structure.FrontBack
	}
	pivot := [3]int32{template.Size[0] / 2, 0, template.Size[2] / 2}
	base := [3]int32{cx * 16, 0, cz * 16}
	origin := portalTemplateOrigin(base, pivot, rotation)
	min, max := portalTemplateBounds(template, origin, mirror, rotation)
	y := portalSuitableY(&random, g, min, max, airPocket)
	base[1] = y
	min, max = portalTemplateBounds(template, portalTemplateOrigin(base, pivot, rotation), mirror, rotation)
	return &netherPortalStart{
		box: structure.Box{Min: min, Max: max}, template: template, templateName: templateName,
		origin: base, pivot: pivot, rotation: rotation, mirror: mirror, airPocket: airPocket,
		mossiness: 0, overgrown: false, vines: false, cold: false, blackstone: true,
	}, nil
}

func (s *netherFeatureSource) placeRuinedPortalStep(pos world.ChunkPos, env structure.ProcessorEnvironment, emit func(structure.Placement)) {
	if env.Random == nil {
		panic("nether: ruined portal placement has no step random source")
	}
	const salt, spacing, separation = int32(34222645), int32(40), int32(15)
	targetX, targetZ := int32(pos[0]), int32(pos[1])
	clip := structure.Bounds{Min: [3]int32{targetX * 16, 0, targetZ * 16}, Max: [3]int32{targetX*16 + 15, 127, targetZ*16 + 15}}
	for sourceZ := targetZ - 2; sourceZ <= targetZ+2; sourceZ++ {
		for sourceX := targetX - 2; sourceX <= targetX+2; sourceX++ {
			candidate, err := structure.PotentialChunk(s.generator.seed, sourceX, sourceZ, salt, spacing, separation, structure.Linear)
			if err != nil {
				panic(err)
			}
			if candidate[0] != sourceX || candidate[1] != sourceZ {
				continue
			}
			start, err := s.generator.portalStart(sourceX, sourceZ)
			if err != nil {
				panic(err)
			}
			if start == nil {
				continue
			}
			placement, ok, err := start.place(clip, env, s.generator.seed)
			if err != nil {
				panic(fmt.Errorf("place Nether ruined portal %s: %w", start.templateName, err))
			}
			if ok {
				emit(placement)
			}
		}
	}
}

func (p *netherPortalStart) place(chunkClip structure.Bounds, env structure.ProcessorEnvironment, seed int64) (structure.Placement, bool, error) {
	center := portalBoxCenter(p.box)
	if center[0]>>4 != chunkClip.Min[0]>>4 || center[2]>>4 != chunkClip.Min[2]>>4 {
		return structure.Placement{}, false, nil
	}
	if env.Random == nil {
		return structure.Placement{}, false, fmt.Errorf("Nether portal placement requires decoration random")
	}
	// Native writes are clipped to Nether's 0..127 build range, while this box
	// remains the complete Java geometry and center-owner clipping region.
	clip := structure.Bounds{Min: p.box.Min, Max: p.box.Max}
	if clip.Min[1] < 0 {
		clip.Min[1] = 0
	}
	if clip.Max[1] > 127 {
		clip.Max[1] = 127
	}
	processors, err := structure.LoadProcessors(nil, netherPortalProcessorSpecs(p), env)
	if err != nil {
		return structure.Placement{}, false, err
	}
	processors = append(processors, structure.Processor{Process: func(_ structure.ProcessContext, b structure.Block) (structure.Block, bool, error) {
		structure.SeedRandomizableContainerLoot(&b, env.Random)
		return b, true, nil
	}})
	origin := portalTemplateOrigin(p.origin, p.pivot, p.rotation)
	out, err := p.template.Place(0, origin, p.rotation, p.mirror, clip, seed, processors...)
	if err != nil {
		return structure.Placement{}, false, err
	}
	out.TemplateUpdateFlags = 2
	out.TemplateKnownShape = false
	overlay := make(map[[3]int32]structure.Block, len(out.Blocks)+256)
	for _, b := range out.Blocks {
		overlay[b.Pos] = b
	}
	nameAt := func(pos [3]int32) string {
		if b, ok := overlay[pos]; ok {
			return b.State.Name
		}
		name, _ := env.BaseBlock(pos[0], pos[1], pos[2]).EncodeBlock()
		return name
	}
	add := func(pos [3]int32, name string) {
		if pos[1] < 0 || pos[1] > 127 {
			return
		}
		b := structure.Block{Pos: pos, State: world.BlockState{Name: name}}
		out.Blocks = append(out.Blocks, b)
		overlay[pos] = b
	}
	canReplace := func(pos [3]int32) bool {
		name := nameAt(pos)
		if name == "minecraft:air" || name == "minecraft:obsidian" {
			return false
		}
		protected, err := gamedata.BlockTagContains("#minecraft:features_cannot_replace", name)
		if err != nil {
			panic(fmt.Errorf("resolve portal protected-block tag: %w", err))
		}
		return !protected
	}
	placeMaterial := func(pos [3]int32) {
		if canReplace(pos) {
			if !p.cold && env.Random.NextFloat() < 0.07 {
				add(pos, "minecraft:magma_block")
			} else {
				add(pos, "minecraft:netherrack")
			}
		}
	}
	drip := func(pos [3]int32) {
		placeMaterial(pos)
		for cap := 8; cap > 0 && env.Random.NextFloat() < 0.5; cap-- {
			pos[1]--
			placeMaterial(pos)
		}
	}
	averageWidth := (p.box.Max[0] - p.box.Min[0] + 1 + p.box.Max[2] - p.box.Min[2] + 1) / 2
	adjust := int32(env.Random.NextInt(max32(1, 8-averageWidth/2)))
	for x := center[0] - 14; x <= center[0]+14; x++ {
		for z := center[2] - 14; z <= center[2]+14; z++ {
			distance := abs32(x-center[0]) + abs32(z-center[2]) + adjust
			if distance >= int32(len(portalSpreadChance)) {
				continue
			}
			if env.Random.NextDouble() >= float64(portalSpreadChance[distance]) {
				continue
			}
			y := sPortalSurface(env, overlay, x, z)
			if y > p.box.Min[1] {
				y = p.box.Min[1]
			}
			pos := [3]int32{x, y, z}
			if abs32(y-p.box.Min[1]) <= 3 && canReplace(pos) {
				placeMaterial(pos)
				drip([3]int32{x, y - 1, z})
			}
		}
	}
	for x := p.box.Min[0] + 1; x < p.box.Max[0]; x++ {
		for z := p.box.Min[2] + 1; z < p.box.Max[2]; z++ {
			if nameAt([3]int32{x, p.box.Min[1], z}) == "minecraft:netherrack" {
				drip([3]int32{x, p.box.Min[1] - 1, z})
			}
		}
	}
	adapted, err := out.AdaptItemFrameEntities()
	return adapted, err == nil, err
}

func portalBoxCenter(box structure.Box) [3]int32 {
	return [3]int32{
		box.Min[0] + (box.Max[0]-box.Min[0]+1)/2,
		box.Min[1] + (box.Max[1]-box.Min[1]+1)/2,
		box.Min[2] + (box.Max[2]-box.Min[2]+1)/2,
	}
}

var portalSpreadChance = [...]float32{1, 1, 1, 1, 1, 1, 1, .9, .9, .8, .7, .6, .4, .2}

func sPortalSurface(env structure.ProcessorEnvironment, overlay map[[3]int32]structure.Block, x, z int32) int32 {
	for y := int32(127); y >= 0; y-- {
		if block, ok := overlay[[3]int32{x, y, z}]; ok {
			if block.State.Name != "minecraft:air" {
				return y
			}
			continue
		}
		name, _ := env.BaseBlock(x, y, z).EncodeBlock()
		if name != "minecraft:air" {
			return y
		}
	}
	return -1
}

func portalTemplateOrigin(base, pivot [3]int32, rotation structure.Rotation) [3]int32 {
	transformedPivot := structure.TransformPosition(pivot, structure.NoMirror, rotation)
	return [3]int32{base[0] + pivot[0] - transformedPivot[0], base[1] + pivot[1] - transformedPivot[1], base[2] + pivot[2] - transformedPivot[2]}
}

func portalTemplateBounds(template *structure.Template, origin [3]int32, mirror structure.Mirror, rotation structure.Rotation) ([3]int32, [3]int32) {
	var min, max [3]int32
	for axis := range 3 {
		min[axis], max[axis] = 1<<30, -1<<30
	}
	for x := int32(0); x < 2; x++ {
		for y := int32(0); y < 2; y++ {
			for z := int32(0); z < 2; z++ {
				corner := [3]int32{x * (template.Size[0] - 1), y * (template.Size[1] - 1), z * (template.Size[2] - 1)}
				worldPos := structure.TransformPosition(corner, mirror, rotation)
				for axis := range 3 {
					worldPos[axis] += origin[axis]
					min[axis] = min32(min[axis], worldPos[axis])
					max[axis] = max32(max[axis], worldPos[axis])
				}
			}
		}
	}
	return min, max
}

func portalSuitableY(random *vanilla.Random, generator *Nether, min, max [3]int32, airPocket bool) int32 {
	newY := int32(0)
	if airPocket {
		newY = 32 + int32(random.NextInt(69))
	} else if random.NextFloat() < .5 {
		newY = 27 + int32(random.NextInt(3))
	} else {
		newY = 29 + int32(random.NextInt(72))
	}
	corners := [4][2]int32{{min[0], min[2]}, {max[0], min[2]}, {min[0], max[2]}, {max[0], max[2]}}
	var columns [4][128]uint32
	for i, corner := range corners {
		generator.baseColumn(corner[0], corner[1], &columns[i])
	}
	for y := newY; y > 15; y-- {
		opaque := 0
		for i := range columns {
			if columns[i][y] != generator.airRID {
				opaque++
				if opaque == 3 {
					return y
				}
			}
		}
	}
	return 15
}

func netherPortalProcessorSpecs(p *netherPortalStart) []structure.ProcessorSpec {
	always := map[string]any{"predicate_type": "minecraft:always_true"}
	rule := func(block string, probability float32, output string) any {
		return map[string]any{
			"input_predicate":    map[string]any{"predicate_type": "minecraft:random_block_match", "block": block, "probability": probability},
			"location_predicate": always, "output_state": output,
		}
	}
	rules := []any{
		rule("minecraft:gold_block", .3, "minecraft:air"),
	}
	if p.cold {
		rules = append(rules, map[string]any{
			"input_predicate":    map[string]any{"predicate_type": "minecraft:block_match", "block": "minecraft:lava"},
			"location_predicate": always, "output_state": "minecraft:netherrack",
		})
	} else {
		rules = append(rules, rule("minecraft:lava", .2, "minecraft:magma_block"))
		rules = append(rules, rule("minecraft:netherrack", .07, "minecraft:magma_block"))
	}
	ignored := []any{"minecraft:structure_block"}
	if !p.airPocket {
		ignored = append(ignored, "minecraft:air")
	}
	specs := []structure.ProcessorSpec{
		{Type: "block_ignore", Config: map[string]any{"blocks": ignored}},
		{Type: "rule", Config: map[string]any{"rules": rules}},
		{Type: "block_age", Config: map[string]any{"mossiness": float64(p.mossiness)}},
		{Type: "protected_blocks", Config: map[string]any{"value": "#minecraft:features_cannot_replace"}},
		{Type: "lava_submerged_block", Config: map[string]any{}},
	}
	if p.blackstone {
		specs = append(specs, structure.ProcessorSpec{Type: "blackstone_replace", Config: map[string]any{}})
	}
	return specs
}

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}
