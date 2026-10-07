package overworld

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/javadata"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/javastructure"
)

func init(){registerModernStructureBuilder("ruined_portal",buildModernRuinedPortal)}
type modernRuinedPortal struct {
	template *javastructure.Template
	box javastructure.Box
	origin [3]int32
	pivot [3]int32
	rotation javastructure.Rotation
	mirror javastructure.Mirror
	processorSpecs []javastructure.ProcessorSpec
	vertical string
	airPocket, cold, overgrown, vines bool
	seed int64
	source ModernFeatureSource
}

func (p modernRuinedPortal) Bounds() javastructure.Box { return p.box }

func (p modernRuinedPortal) Place(clip javastructure.Bounds, env javastructure.ProcessorEnvironment) (javastructure.Placement, error) {
	if env.Random == nil {
		return javastructure.Placement{}, fmt.Errorf("ruined portal placement requires the shared Java structure random stream")
	}
	if env.BaseBlock == nil || env.BaseHeight == nil {
		return javastructure.Placement{}, fmt.Errorf("ruined portal placement requires mutable world queries")
	}
	pivot := javastructure.TransformPosition(p.pivot, p.mirror, p.rotation)
	placeOrigin := [3]int32{
		p.origin[0] + p.pivot[0] - pivot[0],
		p.origin[1] + p.pivot[1] - pivot[1],
		p.origin[2] + p.pivot[2] - pivot[2],
	}
	processors, err := javastructure.LoadProcessors(nil, p.processorSpecs, env)
	if err != nil {
		return javastructure.Placement{}, err
	}
	processors = append(processors, func(_ javastructure.ProcessContext, block javastructure.Block) (javastructure.Block, bool, error) {
		javastructure.SeedRandomizableContainerLoot(&block, env.Random)
		return block, true, nil
	})
	placement, err := p.template.Place(0, placeOrigin, p.rotation, p.mirror, clip, p.seed, processors...)
	if err != nil {
		return javastructure.Placement{}, err
	}
	random := env.Random
	overlay := make(map[[3]int32]string, len(placement.Blocks))
	for i := range placement.Blocks {
		overlay[placement.Blocks[i].Pos] = placement.Blocks[i].State.Name
	}
	cx := (p.box.Min[0] + p.box.Max[0]) / 2
	cz := (p.box.Min[2] + p.box.Max[2]) / 2
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
			if !follow && p.box.Min[1] < y {
				y = p.box.Min[1]
			}
			if abs32(y-p.box.Min[1]) > 3 {
				continue
			}
			ok, err := p.canSpreadAt(env, overlay, x, y, z)
			if err != nil {
				return javastructure.Placement{}, err
			}
			if !ok {
				continue
			}
			if p.overgrown {
				if err := p.tryLeaves(env, overlay, &placement, clip, x, y, z, random); err != nil {
					return javastructure.Placement{}, err
				}
			}
		}
	}
	for x := p.box.Min[0] + 1; x < p.box.Max[0]; x++ {
		for z := p.box.Min[2] + 1; z < p.box.Max[2]; z++ {
			pos := [3]int32{x, p.box.Min[1], z}
			name, err := portalBlockName(env, overlay, pos)
			if err != nil {
				return javastructure.Placement{}, err
			}
			if name == "minecraft:netherrack" {
				if err := p.drip(env, overlay, &placement, clip, x, p.box.Min[1]-1, z, random); err != nil {
					return javastructure.Placement{}, err
				}
			}
		}
	}
	if p.vines || p.overgrown {
		for x := p.box.Min[0]; x <= p.box.Max[0]; x++ {
			for y := p.box.Min[1]; y <= p.box.Max[1]; y++ {
				for z := p.box.Min[2]; z <= p.box.Max[2]; z++ {
					pos := [3]int32{x, y, z}
					if p.vines {
						if err := p.tryVine(env, overlay, &placement, clip, pos, random); err != nil {
							return javastructure.Placement{}, err
						}
					}
					if p.overgrown {
						if err := p.tryLeaves(env, overlay, &placement, clip, x, y, z, random); err != nil {
							return javastructure.Placement{}, err
						}
					}
				}
			}
		}
	}
	return placement, nil
}

func portalBlockName(env javastructure.ProcessorEnvironment, overlay map[[3]int32]string, pos [3]int32) (string, error) {
	if name, ok := overlay[pos]; ok {
		return name, nil
	}
	name, _ := env.BaseBlock(pos[0], pos[1], pos[2]).EncodeBlock()
	return name, nil
}

func (p modernRuinedPortal) replaceable(env javastructure.ProcessorEnvironment, overlay map[[3]int32]string, x, y, z int32) (bool, error) {
	name, err := portalBlockName(env, overlay, [3]int32{x, y, z})
	if err != nil {
		return false, err
	}
	if name == "minecraft:air" || name == "minecraft:obsidian" || name == "minecraft:lava" && p.vertical != "in_nether" {
		return false, nil
	}
	blocked, err := javadata.BlockTagContains("#minecraft:features_cannot_replace", name)
	return !blocked, err
}

func (p modernRuinedPortal) canSpreadAt(env javastructure.ProcessorEnvironment, overlay map[[3]int32]string, x, y, z int32) (bool, error) {
	return p.replaceable(env, overlay, x, y, z)
}

func (p modernRuinedPortal) spreadMaterial(random *java26.Random) string {
	if !p.cold && random.NextFloat() < 0.07 {
		return "magma_block"
	}
	return "netherrack"
}

func (p modernRuinedPortal) drip(env javastructure.ProcessorEnvironment, overlay map[[3]int32]string, out *javastructure.Placement, clip javastructure.Bounds, x, y, z int32, random *java26.Random) error {
	for i := 0; i < 9; i++ {
		ok, err := p.canSpreadAt(env, overlay, x, y, z)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		p.putIfClip(out, overlay, clip, x, y, z, p.spreadMaterial(random))
		if i == 8 || random.NextFloat() >= 0.5 {
			break
		}
		y--
	}
	return nil
}

func (p modernRuinedPortal) tryLeaves(env javastructure.ProcessorEnvironment, overlay map[[3]int32]string, out *javastructure.Placement, clip javastructure.Bounds, x, y, z int32, random *java26.Random) error {
	if random.NextFloat() >= 0.5 {
		return nil
	}
	name, err := portalBlockName(env, overlay, [3]int32{x, y, z})
	if err != nil {
		return err
	}
	if name != "minecraft:netherrack" {
		return nil
	}
	above, err := portalBlockName(env, overlay, [3]int32{x, y + 1, z})
	if err != nil {
		return err
	}
	if above == "minecraft:air" {
		p.putIfClip(out, overlay, clip, x, y+1, z, "jungle_leaves")
	}
	return nil
}

func (p modernRuinedPortal) tryVine(env javastructure.ProcessorEnvironment, overlay map[[3]int32]string, out *javastructure.Placement, clip javastructure.Bounds, pos [3]int32, random *java26.Random) error {
	name, err := portalBlockName(env, overlay, pos)
	if err != nil {
		return err
	}
	if name == "minecraft:air" || name == "minecraft:vine" {
		return nil
	}
	directions := [4][3]int32{{0, 0, -1}, {1, 0, 0}, {0, 0, 1}, {-1, 0, 0}}
	direction := directions[random.NextInt(4)]
	x, y, z := pos[0]+direction[0], pos[1]+direction[1], pos[2]+direction[2]
	neighbor, err := portalBlockName(env, overlay, [3]int32{x, y, z})
	if err != nil {
		return err
	}
	if neighbor != "minecraft:air" || x < clip.Min[0] || x > clip.Max[0] || y < clip.Min[1] || y > clip.Max[1] || z < clip.Min[2] || z > clip.Max[2] {
		return nil
	}
	side := "south"
	switch direction {
	case [3]int32{1, 0, 0}:
		side = "west"
	case [3]int32{0, 0, 1}:
		side = "north"
	case [3]int32{-1, 0, 0}:
		side = "east"
	}
	p.putIfClip(out, overlay, clip, x, y, z, "vine")
	out.Blocks[len(out.Blocks)-1].State.Properties = map[string]any{side: true}
	return nil
}
func buildModernRuinedPortal(in modernStructureBuildInput)(modernStructureStart,bool,error){
 if in.random==nil||in.source==nil{return modernStructureStart{},false,fmt.Errorf("ruined portal requires structure random and world source")}
 setups,ok:=in.config["setups"].([]any);if !ok||len(setups)==0{return modernStructureStart{},false,fmt.Errorf("ruined portal has no setup variants")}
 chosen,err:=chooseRuinedPortalSetup(setups,in.random);if err!=nil{return modernStructureStart{},false,err}
 airProbability:=portalFloat32(chosen["air_pocket_probability"]);airPocket:=airProbability==1||(airProbability>0&&in.random.NextFloat()<airProbability)
 giant:=in.random.NextFloat()<.05
 templates:=[]string{"ruined_portal/portal_1","ruined_portal/portal_2","ruined_portal/portal_3","ruined_portal/portal_4","ruined_portal/portal_5","ruined_portal/portal_6","ruined_portal/portal_7","ruined_portal/portal_8","ruined_portal/portal_9","ruined_portal/portal_10"};if giant{templates=[]string{"ruined_portal/giant_portal_1","ruined_portal/giant_portal_2","ruined_portal/giant_portal_3"}}
 t,err:=javastructure.Load(templates[in.random.NextInt(int32(len(templates))) ]);if err!=nil{return modernStructureStart{},false,err};rotation:=javastructure.Rotation(in.random.NextInt(4));mirror:=javastructure.NoMirror;if in.random.NextFloat()<0.5{mirror=javastructure.FrontBack}
 pivot:=[3]int32{t.Size[0]/2,0,t.Size[2]/2};base:=[3]int32{in.chunkX*16,0,in.chunkZ*16};rotPivot:=javastructure.TransformPosition(pivot,mirror,rotation);placeOrigin:=[3]int32{base[0]+pivot[0]-rotPivot[0],0,base[2]+pivot[2]-rotPivot[2]};box:=modernTemplateBounds(t,placeOrigin,rotation);centerX,centerZ:=(box.Min[0]+box.Max[0])/2,(box.Min[2]+box.Max[2])/2
 vertical:=stringValue(chosen["placement"]);height:=in.source.BaseHeight(centerX,centerZ,vertical=="on_ocean_floor")-1;newY:=height;span:=box.Max[1]-box.Min[1]+1;switch vertical{case "in_nether":if airPocket{newY=32+int32(in.random.NextInt(69))}else if in.random.NextFloat()<.5{newY=27+int32(in.random.NextInt(3))}else{newY=29+int32(in.random.NextInt(72))};case "in_mountain":newY=portalRandomInterval(in.random,70,height-span);case "underground":newY=portalRandomInterval(in.random,-49,height-span);case "partly_buried":newY=height-span+2+int32(in.random.NextInt(7))}
	minY:=int32(-49);for newY>minY{corners:=0;for _,xz:=range [][2]int32{{box.Min[0],box.Min[2]},{box.Max[0],box.Min[2]},{box.Min[0],box.Max[2]},{box.Max[0],box.Max[2]}}{n,_:=in.source.BaseBlock(xz[0],newY,xz[1]).EncodeBlock();if n!="minecraft:air"&&n!="minecraft:water"&&n!="minecraft:lava"{corners++;if corners==3{break}}};if corners>=3{break};newY--}
	biome:=normalizeJavaResource(in.source.JavaBiomeAt(base[0],base[1],base[2]))
	cold:=false
	if boolPortal(chosen["can_be_cold"]) { data,e:=javadata.LoadJSON("biome",biome);if e!=nil{return modernStructureStart{},false,e};if attrs,ok:=data["temperature"].(float64);ok{cold=attrs<=0.15} }
	specs := portalProcessorSpecs(chosen, airPocket, cold, vertical)
	piece := modernRuinedPortal{
		template: t, box: box, origin: base, pivot: pivot, rotation: rotation, mirror: mirror,
		processorSpecs: specs, vertical: vertical, airPocket: airPocket, cold: cold,
		overgrown: boolPortal(chosen["overgrown"]), vines: boolPortal(chosen["vines"]),
		seed: in.seed, source: in.source,
	}
	return modernStructureStart{name: in.name, pieces: []modernStructurePiece{piece}, box: box, terrain: terrainAdjustment(stringValue(in.config["terrain_adaptation"]))}, true, nil
}

func chooseRuinedPortalSetup(setups []any, random *java26.Random) (map[string]any, error) {
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

func portalRandomInterval(random *java26.Random, min, max int32) int32 {
	if min >= max {
		return max
	}
	return min + random.NextInt(max-min+1)
}

func portalProcessorSpecs(setup map[string]any, air, cold bool, placement string) []javastructure.ProcessorSpec {
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
	specs := []javastructure.ProcessorSpec{
		{Type: "block_ignore", Config: map[string]any{"blocks": ignored}},
		{Type: "rule", Config: map[string]any{"rules": rules}},
		{Type: "block_age", Config: map[string]any{"mossiness": setup["mossiness"]}},
		{Type: "protected_blocks", Config: map[string]any{"value": "#minecraft:features_cannot_replace"}},
		{Type: "lava_submerged_block", Config: map[string]any{}},
	}
	if boolPortal(setup["replace_with_blackstone"]) {
		specs = append(specs, javastructure.ProcessorSpec{Type: "blackstone_replace", Config: map[string]any{}})
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

func (p modernRuinedPortal) putIfClip(out *javastructure.Placement, overlay map[[3]int32]string, clip javastructure.Bounds, x, y, z int32, name string) {
	pos := [3]int32{x, y, z}
	if pos[0] < clip.Min[0] || pos[0] > clip.Max[0] || pos[1] < clip.Min[1] || pos[1] > clip.Max[1] || pos[2] < clip.Min[2] || pos[2] > clip.Max[2] {
		return
	}
	properties := map[string]any(nil)
	if name == "jungle_leaves" {
		properties = map[string]any{"persistent": true}
	}
	state := world.BlockState{Name: "minecraft:" + name, Properties: properties}
	out.Blocks = append(out.Blocks, javastructure.Block{Pos: pos, State: state})
	overlay[pos] = state.Name
}
