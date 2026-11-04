// Package mojang holds the constants gophertunnel gives values other than
// Mojang's protocol documentation (Mojang/bedrock-protocol-docs,
// json/*.json) does, for the protocol of each codec package: constimport
// writes Mojang's in their place, and the test of the constants against
// gophertunnel leaves them out.
package mojang

import "strings"

// Enum is the constants of an enum by Mojang's documentation: those whose
// names begin with Prefix, each the prefix and a name of Values, of its
// value. gophertunnel's constants of the prefix are replaced, those of no
// name of Values dropped.
type Enum struct {
	Prefix string
	Values map[string]int64
}

// Enums are the enums corrected, by codec package.
var Enums = map[string][]Enum{
	"v2193": {
		// Mojang's json/PlayerVideoCapturePacketPayload.json of v1.26.50
		// is a variant of Start Video Capture (x-ordinal-index 0) and
		// Stop Video Capture (1). gophertunnel v1.62.0 numbered them the
		// other way, stop 0 and start 1.
		{Prefix: "PlayerVideoCaptureAction", Values: map[string]int64{"Start": 0, "Stop": 1}},
		// Mojang's json/Memory__MemoryCategory.json of v1.26.50 lists 110
		// categories (uint8), each of its index: of v1.26.60, the same
		// with Executable (110) added and their values given
		// (x-enum-binary-value). gophertunnel v1.62.0 had a Balancer (5)
		// and a VR (110) Mojang has not and no LightVolumeManager (43),
		// and so numbered 39 of them wrongly. The names are those
		// gophertunnel gives them.
		{Prefix: "MemoryCategory", Values: map[string]int64{
			"Unknown": 0, "InvalidSizeUnknown": 1, "Actor": 2, "ActorAnimation": 3, "ActorRendering": 4,
			"BlockTickingQueues": 5, "BiomeStorage": 6, "Blobs": 7, "Cereal": 8, "CircuitSystem": 9,
			"Client": 10, "Commands": 11, "DBStorage": 12, "Debug": 13, "Documentation": 14, "ECSSystems": 15,
			"FMOD": 16, "Fonts": 17, "ImGUI": 18, "Input": 19, "JsonUI": 20, "JsonUIControlFactoryJson": 21,
			"JsonUIControlTree": 22, "JsonUIControlTreeControlElement": 23,
			"JsonUIControlTreePopulateDataBinding": 24, "JsonUIControlTreePopulateFocus": 25,
			"JsonUIControlTreePopulateLayout": 26, "JsonUIControlTreePopulateOther": 27,
			"JsonUIControlTreePopulateSprite": 28, "JsonUIControlTreePopulateText": 29,
			"JsonUIControlTreePopulateTTS": 30, "JsonUIControlTreeVisibility": 31, "JsonUICreateUI": 32,
			"JsonUIDefs": 33, "JsonUILayoutManager": 34, "JsonUILayoutManagerRemoveDependencies": 35,
			"JsonUILayoutManagerInitVariable": 36, "Languages": 37, "Level": 38, "LevelStructures": 39,
			"LevelChunk": 40, "LevelChunkGen": 41, "LevelChunkGenThreadLocal": 42, "LightVolumeManager": 43,
			"Network": 44, "Marketplace": 45, "MaterialDragonCompiledDefinition": 46,
			"MaterialDragonMaterial": 47, "MaterialDragonResource": 48, "MaterialDragonUniformMap": 49,
			"MaterialRenderMaterial": 50, "MaterialRenderMaterialGroup": 51, "MaterialVariationManager": 52,
			"Molang": 53, "OreUI": 54, "OreUIClient": 55, "PersonaPieces": 56, "PersonaAnimations": 57,
			"PersonaCharacters": 58, "PersonaSkinPacks": 59, "PersonaRepo": 60, "Player": 61, "RenderChunk": 62,
			"RenderChunkIndexBuffer": 63, "RenderChunkVertexBuffer": 64, "Rendering": 65,
			"RenderingBGFXInit": 66, "RenderingBGFXStartFrame": 67, "RenderingBlockTessellator": 68,
			"RenderingEndFrame": 69, "RenderingGraphicsTasksInit": 70, "RenderingLibrary": 71,
			"RenderingPolygonOperatorPool": 72, "RenderingPBRTextureData": 73, "RenderingRenderRegistry": 74,
			"RenderingSetup": 75, "RenderingVertices": 76, "RequestLog": 77, "ResourcePacks": 78, "Sound": 79,
			"SubChunkBiomeData": 80, "SubChunkBlockData": 81, "SubChunkLightData": 82, "Textures": 83,
			"WeatherRenderer": 84, "WorldGenerator": 85, "Tasks": 86, "Test": 87, "TestLoadTestTags": 88,
			"Scripting": 89, "ScriptingRuntime": 90, "ScriptingContext": 91, "ScriptingContextBindingsMC": 92,
			"ScriptingContextBindingsGT": 93, "ScriptingContextRun": 94, "DataDrivenUI": 95,
			"DataDrivenUIDefs": 96, "Gameface": 97, "GamefaceSystem": 98, "GamefaceDOM": 99, "GamefaceCSS": 100,
			"GamefaceDisplay": 101, "GamefaceTempAllocator": 102, "GamefacePoolAllocator": 103,
			"GamefaceDump": 104, "GamefaceMedia": 105, "GamefaceJSON": 106, "GamefaceScriptEngine": 107,
			"GamefaceScript": 108, "GamefaceLayout": 109,
		}},
	},
}

// Corrected returns whether a constant of a codec package is one of an
// enum corrected.
func Corrected(pkg, name string) bool {
	for _, e := range Enums[pkg] {
		if strings.HasPrefix(name, e.Prefix) {
			return true
		}
	}
	return false
}
