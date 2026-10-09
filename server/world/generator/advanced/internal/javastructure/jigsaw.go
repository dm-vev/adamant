package javastructure

import (
	"fmt"
	"strings"

	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/javadata"
)

// Projection is the Java StructureTemplatePool projection mode.
type Projection uint8

const (
	Rigid Projection = iota
	TerrainMatching
)

// JigsawOptions describes one Java JigsawStructure start.
type JigsawOptions struct {
	Pool string
	StartJigsaw string
	Origin [3]int32
	MaxDepth int
	MaxDistance int32
	ExpansionHack bool
	Seed int64
	Terrain TerrainAdjustment
	HeightAt func(x,z int32) int32
	ProjectStartToHeightmap bool
	LiquidSettings LiquidSettings
}

// JigsawPiece is one selected pool element with its Java placement parameters.
type JigsawPiece struct {
	Template *Template
	parts []jigsawPart
	Origin [3]int32
	Rotation Rotation
	Mirror Mirror
	Projection Projection
	GroundLevelDelta int32
	Processors []string
	ProcessorSpecs []ProcessorSpec
	Bounds Box
	Junctions []Junction
	LiquidSettings LiquidSettings
}
type jigsawPart struct {
	template *Template
	feature any
	processors []string
	processorSpecs []ProcessorSpec
	liquidSettings LiquidSettings
	overrideLiquidSettings bool
}
// Place clips this piece to one world-space box and applies caller-supplied processors in order.
func (p JigsawPiece) Place(palette int, clip Bounds, seed int64, processors ...Processor) (Placement,error) {
	if len(p.parts)>0 {
		out:=Placement{}
		for _,part:=range p.parts {
			if part.feature!=nil{return Placement{},fmt.Errorf("Java jigsaw: feature pool element requires a placed-feature executor")}
			settings := p.LiquidSettings
			if part.overrideLiquidSettings { settings = part.liquidSettings }
			placed,err:=part.place(palette,p.Origin,p.Rotation,p.Mirror,clip,seed,processors,settings)
			if err!=nil{return Placement{},err}
			out.Blocks=append(out.Blocks,placed.Blocks...)
			out.Entities=append(out.Entities,placed.Entities...)
		}
		return out,nil
	}
	placed, err := p.Template.Place(palette,p.Origin,p.Rotation,p.Mirror,clip,seed,processors...)
	markTemplateLiquidPass(&placed, p.LiquidSettings)
	return placed, err
}
func (part jigsawPart) place(palette int,origin [3]int32,rotation Rotation,mirror Mirror,clip Bounds,seed int64,processors []Processor,settings LiquidSettings)(Placement,error){
	if part.template==nil{return Placement{},nil}
	placed, err := part.template.Place(palette,origin,rotation,mirror,clip,seed,processors...)
	markTemplateLiquidPass(&placed, settings)
	return placed, err
}

func markTemplateLiquidPass(placement *Placement, settings LiquidSettings) {
	if len(placement.Blocks) != 0 {
		placement.Blocks[0].TemplateStart = true
		placement.Blocks[0].LiquidSettings = settings
	}
}
// PlaceWithEnvironment runs the pool element's processors and Java terrain-matching gravity projection.
func (p JigsawPiece) PlaceWithEnvironment(palette int, clip Bounds, seed int64, env ProcessorEnvironment) (Placement, error) {
	if len(p.parts)>0 {
		out:=Placement{}
		for _,part:=range p.parts {
			if part.feature!=nil {
				if p.Origin[0]>=clip.Min[0]&&p.Origin[0]<=clip.Max[0]&&p.Origin[1]>=clip.Min[1]&&p.Origin[1]<=clip.Max[1]&&p.Origin[2]>=clip.Min[2]&&p.Origin[2]<=clip.Max[2] {
					if env.PlaceFeature==nil{return Placement{},fmt.Errorf("Java jigsaw: feature pool element has no placed-feature executor")}
					if env.Random==nil{return Placement{},fmt.Errorf("Java jigsaw: feature pool element has no placement random source")}
					env.PlaceFeature(part.feature,p.Origin,env.Random)
				}
				continue
			}
			if part.template==nil{continue}
			partPiece:=p
			partPiece.Template=part.template
			partPiece.parts=nil
			partPiece.Processors=part.processors
			partPiece.ProcessorSpecs=part.processorSpecs
			if part.overrideLiquidSettings { partPiece.LiquidSettings = part.liquidSettings }
			placed,err:=partPiece.placeWithEnvironment(palette,clip,seed,env)
			if err!=nil{return Placement{},err}
			out.Blocks=append(out.Blocks,placed.Blocks...)
			out.Entities=append(out.Entities,placed.Entities...)
		}
		return out,nil
	}
	return p.placeWithEnvironment(palette,clip,seed,env)
}
func (p JigsawPiece) placeWithEnvironment(palette int, clip Bounds, seed int64, env ProcessorEnvironment) (Placement,error) {
	env.WorldSeed = seed
	env.StartPos = p.Origin
	if p.Template != nil && palette >= 0 && palette < len(p.Template.Palettes) { env.BlockCount = len(p.Template.Palettes[palette]) }
	processors, err := LoadProcessors(p.Processors, p.ProcessorSpecs, env)
	if err != nil { return Placement{}, err }
	replacement, err := compileJigsawReplace()
	if err != nil { return Placement{}, err }
	defaults := []Processor{
		processor(func(_ ProcessContext, block Block) (Block, bool, error) { return block, block.State.Name != "minecraft:structure_block", nil }),
		replacement,
	}
	processors = append(defaults, processors...)
	if p.Projection == TerrainMatching {
		gravity, err := compileGravity(map[string]any{"offset": int32(-1), "heightmap": "WORLD_SURFACE_WG"}, env)
		if err != nil { return Placement{}, err }
		processors = append(processors, gravity)
	}
	if env.Random != nil {
		processors = append(processors, processor(func(_ ProcessContext, block Block) (Block, bool, error) {
			SeedRandomizableContainerLoot(&block, env.Random)
			return block, true, nil
		}))
	}
	if env.SkipEntities {
		placed, err := p.Template.PlaceBlocks(palette, p.Origin, p.Rotation, p.Mirror, clip, seed, processors...)
		markTemplateLiquidPass(&placed, p.LiquidSettings)
		return placed, err
	}
	return p.Place(palette, clip, seed, processors...)
}

func randomizableContainer(name string) bool {
	switch name {
	case "minecraft:chest", "minecraft:trapped_chest", "minecraft:barrel", "minecraft:dispenser", "minecraft:dropper":
		return true
	default:
		return strings.HasSuffix(name, "_shulker_box")
	}
}

// SeedRandomizableContainerLoot assigns Java's structure-placement loot seed to a block entity.
func SeedRandomizableContainerLoot(block *Block, random *java26.Random) {
	if block == nil || random == nil || len(block.NBT) == 0 || !randomizableContainer(block.State.Name) { return }
	block.NBT = mergeNBT(block.NBT, map[string]any{"LootTableSeed": random.NextLong()})
}

// JigsawResult contains the selected piece graph, rigid terrain contributions, and junctions.
type JigsawResult struct { Pieces []JigsawPiece; RigidPieces []RigidPiece; Junctions []Junction }

type poolElement struct {
	template string
	feature any
	processors []string
	processorSpecs []ProcessorSpec
	projection Projection
	weight int32
	empty bool
	children []poolElement
	liquidSettings LiquidSettings
	overrideLiquidSettings bool
}
type connector struct { pos [3]int32; front,top string; name,target,pool string; joint string }

// BuildJigsaw expands a Java jigsaw start pool into a deterministic piece graph.
// random must be initialized with the structure's Java world-generation random source.
func BuildJigsaw(options JigsawOptions, random *java26.Random) (JigsawResult,error) {
	if random == nil { return JigsawResult{},fmt.Errorf("Java jigsaw: nil random source") }
	if options.MaxDepth < 0 { return JigsawResult{},fmt.Errorf("Java jigsaw: negative maximum depth") }
	if options.MaxDistance <= 0 { options.MaxDistance = 128 }
	startElements,_,err := loadPool(options.Pool)
	if err != nil { return JigsawResult{},err }
	if len(startElements)==0 { return JigsawResult{},nil }
	startRotation := Rotation(random.NextInt(4))
	startElement,err := choosePoolElement(startElements,random)
	if err != nil { return JigsawResult{},err }
	startTemplates,err:=loadPoolElementTemplates(startElement)
	if err!=nil{return JigsawResult{},err}
	if len(startTemplates)==0||!hasPoolPlacement(startTemplates){return JigsawResult{},nil}
	startOrigin := options.Origin
	if options.StartJigsaw != "" {
		connectors:=shuffledPoolConnectors(startTemplates,startOrigin,startRotation,NoMirror,random)
		found:=false
		for _,c:=range connectors { if strings.TrimPrefix(c.name,"minecraft:")==strings.TrimPrefix(options.StartJigsaw,"minecraft:") { startOrigin=cancelAnchor(startOrigin,c.pos,options.Origin); found=true; break } }
		if !found { return JigsawResult{},fmt.Errorf("Java jigsaw: start connector %q absent from %s",options.StartJigsaw,startElement.template) }
	}
	startPiece:=makeJigsawPoolPiece(startTemplates,startOrigin,startRotation,startElement,options.LiquidSettings)
	if options.ProjectStartToHeightmap && options.HeightAt!=nil {
		centerX:=(startPiece.Bounds.Min[0]+startPiece.Bounds.Max[0])/2; centerZ:=(startPiece.Bounds.Min[2]+startPiece.Bounds.Max[2])/2
		bottomY:=options.Origin[1]+options.HeightAt(centerX,centerZ)
		delta:=bottomY-(startPiece.Bounds.Min[1]+startPiece.GroundLevelDelta)
		startPiece=movePiece(startPiece,0,delta,0)
	}
	result:=JigsawResult{Pieces:[]JigsawPiece{startPiece}}
	queue:=[]int{0}; depths:=[]int{0}
	for len(queue)>0 {
		pieceIndex:=queue[0]; queue=queue[1:]
		depth:=depths[0]; depths=depths[1:]
		if depth>=options.MaxDepth { continue }
		source := result.Pieces[pieceIndex]
		sourceParts:=source.parts
		if len(sourceParts)==0&&source.Template!=nil{sourceParts=[]jigsawPart{{template:source.Template}}}
		sourceConnectors:=shuffledPoolConnectors(sourceParts,source.Origin,source.Rotation,source.Mirror,random)
		for _,sourceJigsaw:=range sourceConnectors {
			front:=directionStep(sourceJigsaw.front)
			jointPos:=[3]int32{sourceJigsaw.pos[0]+front[0],sourceJigsaw.pos[1]+front[1],sourceJigsaw.pos[2]+front[2]}
			poolName:=sourceJigsaw.pool
			if poolName=="" { continue }
			primary,fallback,err:=loadPool(poolName)
			if err!=nil { return JigsawResult{},err }
			candidates:=shuffledPoolElements(primary,random)
			candidates=append(candidates,shuffledPoolElements(fallback,random)...)
			if len(candidates)==0 { continue }
			attached:=false
			for _,element:=range candidates {
				if element.empty { break }
				targetTemplates,err:=loadPoolElementTemplates(element); if err!=nil{return JigsawResult{},err}
				if len(targetTemplates)==0{continue}
				targetConnectorsTemplate:=targetTemplates
				rotations:=randomRotations(random)
				for _,targetRotation:=range rotations {
					targetConnectors:=shuffledPoolConnectors(targetConnectorsTemplate,[3]int32{},targetRotation,NoMirror,random)
					for _,targetJigsaw:=range targetConnectors {
						localPos := targetJigsaw.pos
						if !canAttach(sourceJigsaw,targetJigsaw) { continue }
						origin := [3]int32{jointPos[0]-localPos[0],jointPos[1]-localPos[1],jointPos[2]-localPos[2]}
						candidate := makeJigsawPoolPiece(targetTemplates,origin,targetRotation,element,options.LiquidSettings)
						sourceLocalY := sourceJigsaw.pos[1]-source.Bounds.Min[1]
						targetLocalY := targetJigsaw.pos[1]
						deltaY := sourceLocalY-targetLocalY+front[1]
						if source.Projection==Rigid&&candidate.Projection==Rigid {
							candidate.GroundLevelDelta=source.GroundLevelDelta-deltaY
							candidate=movePiece(candidate,0,source.Bounds.Min[1]+deltaY-candidate.Bounds.Min[1],0)
						} else if options.HeightAt!=nil {
							candidate=movePiece(candidate,0,options.HeightAt(sourceJigsaw.pos[0],sourceJigsaw.pos[2])-targetLocalY-candidate.Bounds.Min[1],0)
						}
						if !withinDistance(candidate.Bounds,options.Origin,options.MaxDistance) { continue }
						if overlapsAny(candidate.Bounds,result.Pieces) { continue }
						junctionY:=int32(0)
						if source.Projection==Rigid {
							junctionY=source.Bounds.Min[1]+sourceLocalY
						} else if candidate.Projection==Rigid {
							junctionY=candidate.Bounds.Min[1]+targetLocalY
						} else if options.HeightAt!=nil {
							junctionY=options.HeightAt(sourceJigsaw.pos[0],sourceJigsaw.pos[2])+deltaY/2
						}
						sourceJunction:=Junction{X:candidate.Origin[0]+localPos[0],Y:junctionY-sourceLocalY+source.GroundLevelDelta,Z:candidate.Origin[2]+localPos[2]}
						targetJunction:=Junction{X:sourceJigsaw.pos[0],Y:junctionY-targetLocalY+candidate.GroundLevelDelta,Z:sourceJigsaw.pos[2]}
						candidate.Junctions=append(candidate.Junctions,targetJunction)
						result.Pieces[pieceIndex].Junctions=append(result.Pieces[pieceIndex].Junctions,sourceJunction)
						result.Pieces=append(result.Pieces,candidate)
						queue=append(queue,len(result.Pieces)-1); depths=append(depths,depth+1)
						attached=true; break
					}
					if attached { break }
				}
				if attached { break }
			}
		}
	}
	for _,piece:=range result.Pieces {
		if options.Terrain != TerrainNone {
			result.RigidPieces=append(result.RigidPieces,RigidPiece{Bounds:piece.Bounds,Adjustment:options.Terrain,GroundLevelDelta:piece.GroundLevelDelta})
		}
		result.Junctions=append(result.Junctions,piece.Junctions...)
	}
	return result,nil
}

func loadPool(name string) ([]poolElement, []poolElement, error) {
	name = strings.TrimPrefix(name, "minecraft:")
	if name == "empty" { return nil, nil, nil }
	root, err := javadata.LoadJSON("template_pool", name)
	if err != nil { return nil, nil, err }
	elements, err := decodePoolElements(root["elements"])
	if err != nil { return nil, nil, fmt.Errorf("Java pool %s: %w", name, err) }
	fallbackName, _ := root["fallback"].(string)
	if fallbackName == "" { return elements, nil, nil }
	fallback, _, err := loadPool(fallbackName)
	if err != nil { return nil, nil, err }
	return elements, fallback, nil
}

func decodePoolElements(raw any)([]poolElement,error){
	values,ok:=raw.([]any);if !ok{return nil,fmt.Errorf("elements has type %T",raw)}
	out:=make([]poolElement,0,len(values))
	for _,item:=range values{
		entry,ok:=item.(map[string]any);if !ok{return nil,fmt.Errorf("entry has type %T",item)}
		element,ok:=entry["element"].(map[string]any)
		if !ok{return nil,fmt.Errorf("element has type %T",entry["element"])}
		decoded,err:=decodePoolElement(element,asInt32(entry["weight"]))
		if err!=nil{return nil,err}
		out=append(out,decoded)
	}
	return out,nil
}
func decodePoolElement(element map[string]any,weight int32)(poolElement,error){
	typeName,_:=element["element_type"].(string)
	projection:=TerrainMatching
	if element["projection"]=="rigid"{projection=Rigid}
	switch strings.TrimPrefix(typeName,"minecraft:"){
	case "empty_pool_element","empty":
		return poolElement{empty:true,weight:weight,projection:projection},nil
	case "single_pool_element","legacy_single_pool_element":
		location,_:=element["location"].(string)
		processors,specs,err:=decodeElementProcessors(element["processors"])
		if err!=nil{return poolElement{},err}
		if location==""{return poolElement{},fmt.Errorf("single pool element lacks location")}
		rawSettings, override := element["override_liquid_settings"]
		settings, err := ParseLiquidSettings(rawSettings)
		if err != nil { return poolElement{}, err }
		return poolElement{template:strings.TrimPrefix(location,"minecraft:"),processors:processors,processorSpecs:specs,projection:projection,weight:weight,liquidSettings:settings,overrideLiquidSettings:override},nil
	case "feature_pool_element":
		feature,ok:=element["feature"]
		if !ok{return poolElement{},fmt.Errorf("feature pool element lacks feature")}
		switch feature.(type) {
		case string,map[string]any:
		default:return poolElement{},fmt.Errorf("feature pool element has invalid feature holder %T",feature)
		}
		return poolElement{feature:feature,projection:projection,weight:weight},nil
	case "list_pool_element":
		raw,ok:=element["elements"].([]any)
		if !ok{return poolElement{},fmt.Errorf("list pool element elements has type %T",element["elements"])}
		if len(raw)==0{return poolElement{},fmt.Errorf("list pool element has no elements")}
		children:=make([]poolElement,0,len(raw))
		for _,child:=range raw{
			definition,ok:=child.(map[string]any)
			if !ok{return poolElement{},fmt.Errorf("list pool element child has type %T",child)}
			decoded,err:=decodePoolElement(definition,1)
			if err!=nil{return poolElement{},err}
			setPoolElementProjection(&decoded,projection)
			children=append(children,decoded)
		}
		return poolElement{projection:projection,weight:weight,children:children},nil
	default:
		return poolElement{},fmt.Errorf("unsupported pool element type %q",typeName)
	}
}
func setPoolElementProjection(element *poolElement,projection Projection){
	element.projection=projection
	for i:=range element.children{setPoolElementProjection(&element.children[i],projection)}
}
func decodeElementProcessors(raw any) ([]string, []ProcessorSpec, error) {
	if raw == nil { return nil,nil,nil }
	if name,ok:=raw.(string);ok { return []string{strings.TrimPrefix(name,"minecraft:")},nil,nil }
	container,ok:=raw.(map[string]any);if !ok{return nil,nil,fmt.Errorf("invalid processor list %T",raw)}
	values,ok:=container["processors"].([]any);if !ok{return nil,nil,fmt.Errorf("inline processors has type %T",container["processors"])}
	specs:=make([]ProcessorSpec,0,len(values))
	for _,value:=range values{
		config,ok:=value.(map[string]any);if !ok{return nil,nil,fmt.Errorf("inline processor has type %T",value)}
		name,ok:=config["processor_type"].(string);if !ok{return nil,nil,fmt.Errorf("inline processor has no processor_type")}
		specs=append(specs,ProcessorSpec{Type:strings.TrimPrefix(name,"minecraft:"),Config:config})
	}
	return nil,specs,nil
}

func choosePoolIndex(pool []poolElement, r *java26.Random) (int,error) {
	if len(pool)==0 { return 0,fmt.Errorf("Java jigsaw: empty pool") }
	total:=int32(0);for _,e:=range pool{total+=max(int32(1),e.weight)}
	choice:=r.NextInt(total)
	for i,e:=range pool{choice-=max(int32(1),e.weight);if choice<0{return i,nil}}
	return len(pool)-1,nil
}
func choosePoolElement(pool []poolElement,r *java26.Random)(poolElement,error){
	index,err:=choosePoolIndex(pool,r);if err!=nil{return poolElement{},err};return pool[index],nil
}
func shuffledPoolElements(pool []poolElement,r *java26.Random)[]poolElement {
	size:=0
	for _,element:=range pool {size+=int(max(int32(1),element.weight))}
	out:=make([]poolElement,0,size)
	for _,element:=range pool {for range max(int32(1),element.weight) {out=append(out,element)}}
	for i:=len(out)-1;i>0;i-- {j:=int(r.NextInt(int32(i+1)));out[i],out[j]=out[j],out[i]}
	return out
}
func randomRotations(r *java26.Random)[]Rotation{
	out:=[]Rotation{None,Clockwise90,Clockwise180,CounterClockwise90}
	for i:=len(out)-1;i>0;i--{j:=int(r.NextInt(int32(i+1)));out[i],out[j]=out[j],out[i]}
	return out
}
func templateConnectors(t *Template,origin [3]int32,rotation Rotation,mirror Mirror)[]connector{
	if t==nil||len(t.Palettes)==0{return nil};var out []connector
	for _,b:=range t.Palettes[0]{if b.State.Name!="minecraft:jigsaw"{continue};nbt:=b.NBT
		orientation,_:=b.State.Properties["orientation"].(string);front,top,_:=strings.Cut(orientation,"_")
		c:=connector{pos:TransformPosition(b.Pos,mirror,rotation),front:transformDirection(front,mirror,rotation),top:transformDirection(top,mirror,rotation)}
		for axis:=range 3{c.pos[axis]+=origin[axis]}
		c.name,_=nbt["name"].(string);c.target,_=nbt["target"].(string);c.pool,_=nbt["pool"].(string);c.joint,_=nbt["joint"].(string)
		if c.joint==""{c.joint="rollable"};out=append(out,c)
	}
	return out
}
func shuffledPoolConnectors(parts []jigsawPart,origin [3]int32,rotation Rotation,mirror Mirror,r *java26.Random)[]connector{
	var out []connector
	for _,part:=range parts {
		if part.feature!=nil {
			out=append(out,connector{pos:origin,front:"down",top:"south",joint:"rollable"})
			continue
		}
		out=append(out,templateConnectors(part.template,origin,rotation,mirror)...)
	}
	for i:=len(out)-1;i>0;i-- {j:=int(r.NextInt(int32(i+1)));out[i],out[j]=out[j],out[i]}
	return out
}
func canAttach(source,target connector)bool{
	if source.front==""||target.front==""||directionOpposite(source.front)!=target.front{return false}
	if source.joint!="rollable"&&source.top!=target.top{return false}
	return target.name==""||strings.TrimPrefix(source.target,"minecraft:")==strings.TrimPrefix(target.name,"minecraft:")
}
func loadPoolElementTemplates(element poolElement)([]jigsawPart,error){
	if element.empty{return []jigsawPart{{}},nil}
	if len(element.children)>0{
		var parts []jigsawPart
		for _,child:=range element.children{
			loaded,err:=loadPoolElementTemplates(child)
			if err!=nil{return nil,err}
			parts=append(parts,loaded...)
		}
		return parts,nil
	}
	if element.feature!=nil{return []jigsawPart{{feature:element.feature}},nil}
	template,err:=LoadOrCreate(element.template)
	if err!=nil{return nil,err}
	return []jigsawPart{{template:template,processors:element.processors,processorSpecs:element.processorSpecs,liquidSettings:element.liquidSettings,overrideLiquidSettings:element.overrideLiquidSettings}},nil
}
func hasPoolPlacement(parts []jigsawPart)bool{
	for _,part:=range parts{if part.template!=nil||part.feature!=nil{return true}}
	return false
}
func makeJigsawPoolPiece(parts []jigsawPart,origin [3]int32,rotation Rotation,e poolElement,settings LiquidSettings)JigsawPiece{
	if e.overrideLiquidSettings { settings = e.liquidSettings }
	piece:=JigsawPiece{Origin:origin,Rotation:rotation,Projection:e.projection,GroundLevelDelta:1,Bounds:Box{Min:origin,Max:origin},LiquidSettings:settings}
	hasFeature:=false
	for _,part:=range parts { if part.feature!=nil {hasFeature=true};if piece.Template==nil&&part.template!=nil{piece.Template=part.template} }
	if len(e.children)>0||hasFeature{piece.parts=parts}
	if piece.Template!=nil{
		piece.Processors=e.processors
		piece.ProcessorSpecs=e.processorSpecs
		piece.Bounds=templateBounds(piece.Template,origin,rotation)
	}
	if hasFeature&&piece.Template!=nil{
		for axis:=range 3{piece.Bounds.Min[axis]=min(piece.Bounds.Min[axis],origin[axis]);piece.Bounds.Max[axis]=max(piece.Bounds.Max[axis],origin[axis])}
	}
	for _,part:=range parts{
		if part.template==nil||part.template==piece.Template{continue}
		bounds:=templateBounds(part.template,origin,rotation)
		for axis:=range 3{piece.Bounds.Min[axis]=min(piece.Bounds.Min[axis],bounds.Min[axis]);piece.Bounds.Max[axis]=max(piece.Bounds.Max[axis],bounds.Max[axis])}
	}
	return piece
}
func templateBounds(t *Template,origin [3]int32,r Rotation)Box{
	first:=[3]int32{};last:=[3]int32{t.Size[0]-1,t.Size[1]-1,t.Size[2]-1}
	corners:=[2][3]int32{TransformPosition(first,NoMirror,r),TransformPosition(last,NoMirror,r)}
	return Box{Min:[3]int32{origin[0]+min(corners[0][0],corners[1][0]),origin[1]+min(corners[0][1],corners[1][1]),origin[2]+min(corners[0][2],corners[1][2])},Max:[3]int32{origin[0]+max(corners[0][0],corners[1][0]),origin[1]+max(corners[0][1],corners[1][1]),origin[2]+max(corners[0][2],corners[1][2])}}
}
func movePiece(p JigsawPiece,x,y,z int32)JigsawPiece{p.Origin[0]+=x;p.Origin[1]+=y;p.Origin[2]+=z;p.Bounds.Min[0]+=x;p.Bounds.Min[1]+=y;p.Bounds.Min[2]+=z;p.Bounds.Max[0]+=x;p.Bounds.Max[1]+=y;p.Bounds.Max[2]+=z;for i:=range p.Junctions{p.Junctions[i].X+=x;p.Junctions[i].Y+=y;p.Junctions[i].Z+=z};return p}
func overlapsAny(b Box,pieces []JigsawPiece)bool{for _,p:=range pieces{if float64(b.Min[0])+.25<float64(p.Bounds.Max[0]+1)&&float64(b.Max[0]+1)-.25>float64(p.Bounds.Min[0])&&float64(b.Min[1])+.25<float64(p.Bounds.Max[1]+1)&&float64(b.Max[1]+1)-.25>float64(p.Bounds.Min[1])&&float64(b.Min[2])+.25<float64(p.Bounds.Max[2]+1)&&float64(b.Max[2]+1)-.25>float64(p.Bounds.Min[2]){return true}};return false}
func withinDistance(b Box,origin [3]int32,distance int32)bool{cx:=(b.Min[0]+b.Max[0])/2;cz:=(b.Min[2]+b.Max[2])/2;return abs32(cx-origin[0])<=distance&&abs32(cz-origin[2])<=distance}
func sourceRigid(p JigsawPiece)bool{return p.Projection==Rigid}
func directionStep(d string)[3]int32{switch d{case "north":return [3]int32{0,0,-1};case "south":return [3]int32{0,0,1};case "west":return [3]int32{-1,0,0};case "east":return [3]int32{1,0,0};case "up":return [3]int32{0,1,0};case "down":return [3]int32{0,-1,0};default:return [3]int32{}}}
func directionOpposite(d string)string{switch d{case "north":return "south";case "south":return "north";case "east":return "west";case "west":return "east";case "up":return "down";case "down":return "up";default:return ""}}
func asInt32(v any)int32{switch n:=v.(type){case float64:return int32(n);case int32:return n;case int:return int32(n)};return 1}
func abs32(v int32)int32{if v<0{return -v};return v}
func cancelAnchor(origin,connector,requested [3]int32)[3]int32{for axis:=range 3{origin[axis]+=requested[axis]-connector[axis]};return origin}
