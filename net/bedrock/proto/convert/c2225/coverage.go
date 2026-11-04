package c2225

// reviewed says, for each packet whose layout 2225 changed, how the step
// carries it, so that a field the generator copies by name, gives its zero
// value or drops is one somebody looked at. A packet that changes in a
// next schema fails TestCoverage until it is reviewed here.
var reviewed = map[uint32]string{
	11: "StartGame: EditorLevelMigrationVersion is new, 0 (legacy) going up; dropped going down",
	12: "AddPlayer: PassengerOfBlock is new, none going up; dropped going down",
	13: "AddActor: PassengerOfBlock is new, none going up; dropped going down",
	30: "InventoryTransaction: the Hand of a use on an entity and of a release is new, 0 (the main hand) going up; " +
		"dropped going down (hand_convert.go)",
	44:  "Animate: Hand is new, 0 (the main hand) going up; dropped going down",
	58:  "LevelChunk: ClientBiomeUpdate is new, false (the client keeps its biomes, as in 2193) going up; dropped going down",
	63:  "PlayerList: the PlayFab ID moves between the skin and the entry (hand_convert.go)",
	90:  "StructureBlockUpdate: the filtered name is optional in 2225, \"\" is none (fixups.go)",
	93:  "PlayerSkin: the skin has no PlayFab ID in 2225: lost going up, \"\" going down (hand_convert.go)",
	137: "EducationSettings: CanModifyBlocks is one of the agent's capabilities in 2225 (fixups.go)",
	144: "PlayerAuthInput: the actions of its stack request are renumbered, which the codecs do, and the reserved craft " +
		"has no form in 2193: left out going down (hand_convert.go, fixups.go)",
	147: "ItemStackRequest: as the stack request of PlayerAuthInput",
	164: "ClientBoundDebugRenderer: the fields of a marker are a DebugMarkerData in 2225, its colour bytes (fixups.go)",
	180: "DimensionData: CloudHeight and RenderClouds are new, 0 and false going up (the session sends dimensions of " +
		"its own; BDS 1.26.61 tells its defaults, open question Q2); dropped going down",
	315: "ServerBoundDiagnostics: the system categories and the position and dimension of a timed entity are optional " +
		"in 2225 (fixups.go); the memory categories have the same values in both, by Mojang's JSON",
	345: "ClientBoundAttributeLayerSync: the payload of an environment attribute is a variant in 2225 and the noise is " +
		"named by its settings (fixups.go); a transition's clock is lost going down",
	348: "ClientboundUpdateSoundData: 2193 writes the update seven times, 2225 once (fixups.go)",
}

// noForm lists what the step leaves out as the other protocol has no form
// of it: packets by ID, and packets that hold a value of an enum the other
// protocol has not (upValues, downValues).
var noForm = map[uint32]string{
	353: "ClientboundMatchmakingState: none going down",
	354: "ServerboundStonecutterSetRecipe: none going down",
	355: "ClientboundStonecutterSetRecipe: none going down",
	356: "ServerboundMatchmakingCancel: none going down",
	357: "SetPassengerOfBlock: none going down",
	358: "ServerboundCursorItemDrag: none going down",
	359: "ClientboundPlayAudioContent: none going down",
	360: "ServerboundRegisterAudioContent: none going down",
	25:  "LevelEvent: the level events 9817-9820 of 2193, none going up; the particles of an ice ball (16489) of 2225, none going down",
}
