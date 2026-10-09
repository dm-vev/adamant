package overworld

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/javastructure"
	java26 "github.com/df-mc/dragonfly/server/internal/java26"
)

func placeModernFossil(region *featureRegion, config map[string]any, origin featurePos, random *java26.Random) bool {
	fossilNames,ok:=config["fossil_structures"].([]any);if !ok||len(fossilNames)==0{panic("overworld: malformed Java fossil structures")}
	overlayNames,ok:=config["overlay_structures"].([]any);if !ok||len(overlayNames)!=len(fossilNames){panic("overworld: malformed Java fossil overlay structures")}
	rotation:=javastructure.Rotation(random.NextInt(4))
	index:=int(random.NextInt(int32(len(fossilNames))))
	fossilName,ok:=fossilNames[index].(string);if !ok{panic("overworld: malformed Java fossil structure name")}
	overlayName,ok:=overlayNames[index].(string);if !ok{panic("overworld: malformed Java fossil overlay name")}
	fossil,err:=javastructure.Load(normalizeJavaResource(fossilName));if err!=nil{panic(err)}
	overlay,err:=javastructure.Load(normalizeJavaResource(overlayName));if err!=nil{panic(err)}
	sizeX,sizeZ:=fossil.Size[0],fossil.Size[2]
	if rotation==javastructure.Clockwise90||rotation==javastructure.CounterClockwise90{sizeX,sizeZ=sizeZ,sizeX}
	lowX,lowZ:=origin.x-sizeX/2,origin.z-sizeZ/2
	lowest:=origin.y
	for x:=int32(0);x<sizeX;x++{for z:=int32(0);z<sizeZ;z++{h:=region.Height(lowX+x,lowZ+z,true);if h<lowest{lowest=h}}}
	targetY:=lowest-15-random.NextInt(10)
	if min:=region.MinY()+10;targetY<min{targetY=min}
	sizeX0,sizeZ0:=fossil.Size[0]-1,fossil.Size[2]-1
	baseX,baseZ:=lowX,lowZ
	switch rotation{case javastructure.Clockwise90:baseX+=sizeZ0;case javastructure.Clockwise180:baseX+=sizeX0;baseZ+=sizeZ0;case javastructure.CounterClockwise90:baseZ+=sizeX0}
	templateOrigin:=[3]int32{baseX,targetY,baseZ}
	maxEmpty:=featureNumber(config["max_empty_corners_allowed"])
	rotX,rotZ:=fossil.Size[0],fossil.Size[2];if rotation==javastructure.Clockwise90||rotation==javastructure.CounterClockwise90{rotX,rotZ=rotZ,rotX}
	minBound:=[3]int32{templateOrigin[0],templateOrigin[1],templateOrigin[2]}
	maxBound:=[3]int32{templateOrigin[0]+rotX-1,templateOrigin[1]+fossil.Size[1]-1,templateOrigin[2]+rotZ-1}
	empty:=0
	for _,x:=range []int32{minBound[0],maxBound[0]}{for _,y:=range []int32{minBound[1],maxBound[1]}{for _,z:=range []int32{minBound[2],maxBound[2]}{
		name,_:=region.State(featurePos{x,y,z});if featureRegionAir(region,featurePos{x,y,z})||name=="minecraft:water"||name=="minecraft:lava"{empty++}
	}}}
	if empty>maxEmpty{return false}
	chunkX, chunkZ := origin.x>>4, origin.z>>4
	clip:=javastructure.Bounds{Min:[3]int32{chunkX*16-16,region.MinY(),chunkZ*16-16},Max:[3]int32{chunkX*16+31,region.MaxY(),chunkZ*16+31}}
	place:=func(template *javastructure.Template,resource any) {
		name:=normalizeJavaResource(resource.(string))
		processors, err := javastructure.LoadProcessors([]string{name}, nil, javastructure.ProcessorEnvironment{
			BaseBlock:  func(x, y, z int32) world.Block { return region.Block(featurePos{x, y, z}) },
			JavaBiomeAt: region.f.source.JavaBiomeAt,
			BaseHeight: func(x, z int32, oceanFloor bool) int32 {
				kind := "WORLD_SURFACE_WG"
				if oceanFloor {
					kind = "OCEAN_FLOOR_WG"
				}
				return region.placementHeight(x, z, kind)
			},
			WorldSeed: region.f.seed, StartPos: templateOrigin, BlockCount: len(template.Palettes[0]), Random: random, SkipEntities: true,
			SettingsRandom: random,
		})
		if err != nil {
			panic(err)
		}
		placement, err := template.PlaceBlocks(0, templateOrigin, rotation, javastructure.NoMirror, clip, 0, processors...)
		if err != nil {
			panic(err)
		}
		placement.TemplateUpdateFlags = 3
		placement.TemplateKnownShape = false
		region.f.applyStructurePlacement(region, placement)
	}
	place(fossil,config["fossil_processors"])
	place(overlay,config["overlay_processors"])
	return true
}
