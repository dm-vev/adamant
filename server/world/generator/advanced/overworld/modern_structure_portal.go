package overworld

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/structure"
)

func init(){registerModernStructureBuilder("ruined_portal",buildModernRuinedPortal)}
type modernRuinedPortal struct {
	template *structure.Template
	box structure.Box
	origin [3]int32
	pivot [3]int32
	rotation structure.Rotation
	mirror structure.Mirror
	processorSpecs []structure.ProcessorSpec
	vertical string
	airPocket, cold, overgrown, vines bool
	seed int64
	source ModernFeatureSource
}

func (p modernRuinedPortal) Bounds() structure.Box { return p.box }

// Place ports RuinedPortalPiece.postProcess. Only the chunk holding the
// centre of the template places the portal, the whole template at once, and
// spreads its netherrack, drips and plants with one random stream.
func (p modernRuinedPortal) Place(clip structure.Bounds, env structure.ProcessorEnvironment) (structure.Placement, error) {
	if env.Random == nil {
		return structure.Placement{}, fmt.Errorf("ruined portal placement requires the shared structure random stream")
	}
	if env.BlockStateID == nil || env.BaseHeight == nil {
		return structure.Placement{}, fmt.Errorf("ruined portal placement requires mutable world queries")
	}
	cx, cy, cz := boxCenter(p.box.Min[0], p.box.Max[0]), boxCenter(p.box.Min[1], p.box.Max[1]), boxCenter(p.box.Min[2], p.box.Max[2])
	if cx < clip.Min[0] || cx > clip.Max[0] || cy < clip.Min[1] || cy > clip.Max[1] || cz < clip.Min[2] || cz > clip.Max[2] {
		return structure.Placement{}, nil
	}
	for axis := range 3 {
		clip.Min[axis] = min(clip.Min[axis], p.box.Min[axis])
		clip.Max[axis] = max(clip.Max[axis], p.box.Max[axis])
	}
	// The template turns about the pivot, which the mirror leaves in place.
	pivot := structure.TransformPosition(p.pivot, structure.NoMirror, p.rotation)
	placeOrigin := [3]int32{
		p.origin[0] + p.pivot[0] - pivot[0],
		p.origin[1] + p.pivot[1] - pivot[1],
		p.origin[2] + p.pivot[2] - pivot[2],
	}
	processors, err := structure.LoadProcessors(nil, p.processorSpecs, env)
	if err != nil {
		return structure.Placement{}, err
	}
	processors = append(processors, structure.Processor{Process: func(_ structure.ProcessContext, block structure.Block) (structure.Block, bool, error) {
		structure.SeedRandomizableContainerLoot(&block, env.Random)
		return block, true, nil
	}})
	placement, err := p.template.Place(0, placeOrigin, p.rotation, p.mirror, clip, p.seed, processors...)
	if err != nil {
		return structure.Placement{}, err
	}
	placement.BeyondChunk = true
	w := &portalWorld{env: env, out: &placement, overlay: make(map[[3]int32]world.BlockState, len(placement.Blocks))}
	for _, b := range placement.Blocks {
		w.overlay[b.Pos] = b.State
	}
	random := env.Random

	// spreadNetherrack
	follow := p.vertical == "on_land_surface" || p.vertical == "on_ocean_floor"
	avgWidth := (p.box.Max[0] - p.box.Min[0] + 1 + p.box.Max[2] - p.box.Min[2] + 1) / 2
	adjust := int32(random.NextInt(maxInt32(1, 8-avgWidth/2)))
	for x := cx - 14; x <= cx+14; x++ {
		for z := cz - 14; z <= cz+14; z++ {
			distance := abs32(x-cx) + abs32(z-cz) + adjust
			if distance >= 14 || random.NextDouble() >= portalSpreadChance[distance] {
				continue
			}
			y := env.BaseHeight(x, z, p.vertical == "on_ocean_floor") - 1
			if !follow {
				y = min(p.box.Min[1], y)
			}
			if abs32(y-p.box.Min[1]) > 3 || !p.replaceable(w.state(x, y, z)) {
				continue
			}
			p.placeNetherrackOrMagma(w, random, x, y, z)
			if p.overgrown {
				p.maybeAddLeavesAbove(w, random, x, y, z)
			}
			p.addDripColumn(w, random, x, y-1, z)
		}
	}
	// addNetherrackDripColumnsBelowPortal
	for x := p.box.Min[0] + 1; x < p.box.Max[0]; x++ {
		for z := p.box.Min[2] + 1; z < p.box.Max[2]; z++ {
			if w.state(x, p.box.Min[1], z).Name == "minecraft:netherrack" {
				p.addDripColumn(w, random, x, p.box.Min[1]-1, z)
			}
		}
	}
	if p.vines || p.overgrown {
		for x := p.box.Min[0]; x <= p.box.Max[0]; x++ {
			for y := p.box.Min[1]; y <= p.box.Max[1]; y++ {
				for z := p.box.Min[2]; z <= p.box.Max[2]; z++ {
					if p.vines {
						if err := p.maybeAddVines(w, random, x, y, z); err != nil {
							return structure.Placement{}, err
						}
					}
					if p.overgrown {
						p.maybeAddLeavesAbove(w, random, x, y, z)
					}
				}
			}
		}
	}
	return placement, nil
}

// boxCenter is BoundingBox.getCenter along one axis.
func boxCenter(min, max int32) int32 { return min + (max-min+1)/2 }

// portalWorld reads the level a ruined portal decorates, including its own
// writes, and drops writes outside the region as WorldGenRegion does.
type portalWorld struct {
	env     structure.ProcessorEnvironment
	out     *structure.Placement
	overlay map[[3]int32]world.BlockState
}

func (w *portalWorld) state(x, y, z int32) world.BlockState {
	if state, ok := w.overlay[[3]int32{x, y, z}]; ok {
		return state
	}
	id, err := w.env.BlockStateID(x, y, z)
	if err != nil {
		panic(err)
	}
	name, properties, ok := gamedata.BlockStateFromID(id)
	if !ok {
		panic(fmt.Sprintf("overworld: unknown block state %d at %d,%d,%d", id, x, y, z))
	}
	return world.BlockState{Name: name, Properties: properties}
}

func (w *portalWorld) set(x, y, z int32, state world.BlockState) {
	if y < int32(w.env.Range.Min()) || y > int32(w.env.Range.Max()) || (w.env.CanWriteBlock != nil && !w.env.CanWriteBlock(x, y, z)) {
		return
	}
	pos := [3]int32{x, y, z}
	w.out.Blocks = append(w.out.Blocks, structure.Block{Pos: pos, State: state})
	w.overlay[pos] = state
}

func portalAir(name string) bool {
	return name == "minecraft:air" || name == "minecraft:cave_air" || name == "minecraft:void_air"
}

// replaceable is canBlockBeReplacedByNetherrackOrMagma.
func (p modernRuinedPortal) replaceable(state world.BlockState) bool {
	if state.Name == "minecraft:air" || state.Name == "minecraft:obsidian" || state.Name == "minecraft:lava" && p.vertical != "in_nether" {
		return false
	}
	blocked, err := gamedata.BlockTagContains("#minecraft:features_cannot_replace", state.Name)
	if err != nil {
		panic(err)
	}
	return !blocked
}

func (p modernRuinedPortal) placeNetherrackOrMagma(w *portalWorld, random *vanilla.Random, x, y, z int32) {
	if !p.replaceable(w.state(x, y, z)) {
		return
	}
	name := "minecraft:netherrack"
	if !p.cold && random.NextFloat() < 0.07 {
		name = "minecraft:magma_block"
	}
	w.set(x, y, z, world.BlockState{Name: name})
}

// addDripColumn is addNetherrackDripColumn: up to eight more blocks below,
// each with half the chance of the one above.
func (p modernRuinedPortal) addDripColumn(w *portalWorld, random *vanilla.Random, x, y, z int32) {
	p.placeNetherrackOrMagma(w, random, x, y, z)
	for remaining := 8; remaining > 0 && random.NextFloat() < 0.5; remaining-- {
		y--
		p.placeNetherrackOrMagma(w, random, x, y, z)
	}
}

func (p modernRuinedPortal) maybeAddLeavesAbove(w *portalWorld, random *vanilla.Random, x, y, z int32) {
	if random.NextFloat() < 0.5 && w.state(x, y, z).Name == "minecraft:netherrack" && portalAir(w.state(x, y+1, z).Name) {
		w.set(x, y+1, z, world.BlockState{Name: "minecraft:jungle_leaves", Properties: map[string]any{"persistent": true}})
	}
}

// maybeAddVines hangs a vine on a random side of a block whose collision
// face on that side is full.
func (p modernRuinedPortal) maybeAddVines(w *portalWorld, random *vanilla.Random, x, y, z int32) error {
	state := w.state(x, y, z)
	if portalAir(state.Name) || state.Name == "minecraft:vine" {
		return nil
	}
	sides := [4]struct {
		offset [3]int32
		face   cube.Face
		vine   string
	}{{[3]int32{0, 0, -1}, cube.FaceNorth, "south"}, {[3]int32{1, 0, 0}, cube.FaceEast, "west"}, {[3]int32{0, 0, 1}, cube.FaceSouth, "north"}, {[3]int32{-1, 0, 0}, cube.FaceWest, "east"}}
	side := sides[random.NextInt(4)]
	nx, ny, nz := x+side.offset[0], y+side.offset[1], z+side.offset[2]
	if !portalAir(w.state(nx, ny, nz).Name) {
		return nil
	}
	id, err := gamedata.BlockStateID(state.Name, state.Properties)
	if err != nil {
		return err
	}
	full, err := gamedata.GenerationCollisionFaceCoversRectangleID(id, int(side.face), 0, 0, 1, 1)
	if err != nil || !full {
		return err
	}
	w.set(nx, ny, nz, world.BlockState{Name: "minecraft:vine", Properties: map[string]any{side.vine: true}})
	return nil
}

func buildModernRuinedPortal(in modernStructureBuildInput)(modernStructureStart,bool,error){
 if in.random==nil||in.source==nil{return modernStructureStart{},false,fmt.Errorf("ruined portal requires structure random and world source")}
 setups,ok:=in.config["setups"].([]any);if !ok||len(setups)==0{return modernStructureStart{},false,fmt.Errorf("ruined portal has no setup variants")}
 chosen,err:=chooseRuinedPortalSetup(setups,in.random);if err!=nil{return modernStructureStart{},false,err}
 airProbability:=portalFloat32(chosen["air_pocket_probability"]);airPocket:=airProbability==1||(airProbability>0&&in.random.NextFloat()<airProbability)
 giant:=in.random.NextFloat()<.05
 templates:=[]string{"ruined_portal/portal_1","ruined_portal/portal_2","ruined_portal/portal_3","ruined_portal/portal_4","ruined_portal/portal_5","ruined_portal/portal_6","ruined_portal/portal_7","ruined_portal/portal_8","ruined_portal/portal_9","ruined_portal/portal_10"};if giant{templates=[]string{"ruined_portal/giant_portal_1","ruined_portal/giant_portal_2","ruined_portal/giant_portal_3"}}
 t,err:=structure.Load(templates[in.random.NextInt(int32(len(templates))) ]);if err!=nil{return modernStructureStart{},false,err};rotation:=structure.Rotation(in.random.NextInt(4));mirror:=structure.FrontBack;if in.random.NextFloat()<0.5{mirror=structure.NoMirror}
 pivot:=[3]int32{t.Size[0]/2,0,t.Size[2]/2};base:=[3]int32{in.chunkX*16,0,in.chunkZ*16};rotPivot:=structure.TransformPosition(pivot,structure.NoMirror,rotation);placeOrigin:=[3]int32{base[0]+pivot[0]-rotPivot[0],0,base[2]+pivot[2]-rotPivot[2]};box:=modernMirroredTemplateBounds(t,placeOrigin,mirror,rotation);centerX,centerZ:=boxCenter(box.Min[0],box.Max[0]),boxCenter(box.Min[2],box.Max[2])
 vertical:=stringValue(chosen["placement"]);height:=in.source.BaseHeight(centerX,centerZ,vertical=="on_ocean_floor")-1;newY:=height;span:=box.Max[1]-box.Min[1]+1;switch vertical{case "in_nether":if airPocket{newY=32+int32(in.random.NextInt(69))}else if in.random.NextFloat()<.5{newY=27+int32(in.random.NextInt(3))}else{newY=29+int32(in.random.NextInt(72))};case "in_mountain":newY=portalRandomInterval(in.random,70,height-span);case "underground":newY=portalRandomInterval(in.random,-49,height-span);case "partly_buried":newY=height-span+2+int32(in.random.NextInt(7))}
	// findSuitableY: walk down until three bottom corners of the box stand on
	// blocks the placement's heightmap counts as occupied. The template goes
	// to that height, where the stub's biome is checked.
	oceanFloor := vertical == "on_ocean_floor"
	minY := int32(-49)
	for ; newY > minY; newY-- {
		corners := 0
		for _, xz := range [][2]int32{{box.Min[0], box.Min[2]}, {box.Max[0], box.Min[2]}, {box.Min[0], box.Max[2]}, {box.Max[0], box.Max[2]}} {
			name, _ := in.source.BaseBlock(xz[0], newY, xz[1]).EncodeBlock()
			if name != "minecraft:air" && name != "minecraft:cave_air" && name != "minecraft:void_air" && (!oceanFloor || name != "minecraft:water" && name != "minecraft:lava") {
				corners++
			}
		}
		if corners >= 3 {
			break
		}
	}
	base[1] = newY
	box.Min[1] += newY
	box.Max[1] += newY
	if allowed, err := structureStubBiomeAllowed(in.source, in.config, base[0], base[1], base[2]); err != nil || !allowed {
		return modernStructureStart{}, false, err
	}
	cold := false
	if boolPortal(chosen["can_be_cold"]) {
		biome := normalizeResource(in.source.NoiseBiomeAt(base[0]>>2, base[1]>>2, base[2]>>2))
		data, err := gamedata.LoadJSON("biome", biome)
		if err != nil {
			return modernStructureStart{}, false, err
		}
		temperature, _ := data["temperature"].(float64)
		// Biome.coldEnoughToSnow at the stub.
		cold = heightAdjustedTemperature(float32(temperature), data["temperature_modifier"] == "frozen", base[0], base[1], base[2], in.source.SeaLevel()) < 0.15
	}
	specs := portalProcessorSpecs(chosen, airPocket, cold, vertical)
	piece := modernRuinedPortal{
		template: t, box: box, origin: base, pivot: pivot, rotation: rotation, mirror: mirror,
		processorSpecs: specs, vertical: vertical, airPocket: airPocket, cold: cold,
		overgrown: boolPortal(chosen["overgrown"]), vines: boolPortal(chosen["vines"]),
		seed: in.seed, source: in.source,
	}
	return modernStructureStart{name: in.name, pieces: []modernStructurePiece{piece}, box: box, terrain: terrainAdjustment(stringValue(in.config["terrain_adaptation"]))}, true, nil
}

func chooseRuinedPortalSetup(setups []any, random *vanilla.Random) (map[string]any, error) {
	if len(setups) == 1 {
		setup, ok := setups[0].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("malformed ruined portal setup")
		}
		return setup, nil
	}
	total := float32(0)
	for _, value := range setups {
		setup, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("malformed ruined portal setup")
		}
		total += portalFloat32(setup["weight"])
	}
	pick := random.NextFloat()
	for _, value := range setups {
		setup := value.(map[string]any)
		pick -= portalFloat32(setup["weight"]) / total
		if pick < 0 {
			return setup, nil
		}
	}
	return nil, fmt.Errorf("ruined portal setup weights do not select an entry")
}

func portalRandomInterval(random *vanilla.Random, min, max int32) int32 {
	if min >= max {
		return max
	}
	return min + random.NextInt(max-min+1)
}

func portalProcessorSpecs(setup map[string]any, air, cold bool, placement string) []structure.ProcessorSpec {
	ignored := []any{"minecraft:structure_block"}
	if !air {
		ignored = append(ignored, "minecraft:air")
	}
	rules := []any{
		map[string]any{
			"input_predicate": map[string]any{"predicate_type": "minecraft:random_block_match", "block": "minecraft:gold_block", "probability": .3},
			"output_state": "minecraft:air",
		},
	}
	lavaOutput := "minecraft:magma_block"
	lavaTest := map[string]any{"predicate_type": "minecraft:block_match", "block": "minecraft:lava"}
	if placement == "on_ocean_floor" {
		rules = append(rules, map[string]any{"input_predicate": lavaTest, "output_state": lavaOutput})
	} else if cold {
		rules = append(rules, map[string]any{"input_predicate": lavaTest, "output_state": "minecraft:netherrack"})
	} else {
		rules = append(rules, map[string]any{
			"input_predicate": map[string]any{"predicate_type": "minecraft:random_block_match", "block": "minecraft:lava", "probability": .2},
			"output_state": lavaOutput,
		})
	}
	if !cold {
		rules = append(rules, map[string]any{
			"input_predicate": map[string]any{"predicate_type": "minecraft:random_block_match", "block": "minecraft:netherrack", "probability": .07},
			"output_state": lavaOutput,
		})
	}
	specs := []structure.ProcessorSpec{
		{Type: "block_ignore", Config: map[string]any{"blocks": ignored}},
		{Type: "rule", Config: map[string]any{"rules": rules}},
		{Type: "block_age", Config: map[string]any{"mossiness": setup["mossiness"]}},
		{Type: "protected_blocks", Config: map[string]any{"value": "#minecraft:features_cannot_replace"}},
		{Type: "lava_submerged_block", Config: map[string]any{}},
	}
	if boolPortal(setup["replace_with_blackstone"]) {
		specs = append(specs, structure.ProcessorSpec{Type: "blackstone_replace", Config: map[string]any{}})
	}
	return specs
}

func maxInt32(a, b int32) int32 {
	if a > b {
		return a
	}
	return b
}

func portalFloat32(value any) float32 {
	switch number := value.(type) {
	case float64:
		return float32(number)
	case float32:
		return number
	case int:
		return float32(number)
	case int32:
		return float32(number)
	}
	return 0
}

func boolPortal(value any) bool {
	result, _ := value.(bool)
	return result
}

var portalSpreadChance = [...]float64{1, 1, 1, 1, 1, 1, 1, 0.9, 0.9, 0.8, 0.7, 0.6, 0.4, 0.2}

