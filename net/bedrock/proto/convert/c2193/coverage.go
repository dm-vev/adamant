package c2193

// reviewed documents every packet whose wire layout changed between 2169 and 2193.
var reviewed = map[uint32]string{
	30:  "InventoryTransaction: hasType/hasActions bools removed, InventoryAction double optionals simplified, UseItemTransactionData.Hand added in 2193",
	74:  "BossEvent: PlayerUniqueID removed in 2193; zero going down",
	86:  "PlaySound: BypassListenerRangeCheck and PlaybackPositionSeconds added in 2193; dropped going down",
	111: "MoveActorDelta: Ticks added in 2193; dropped going down",
	144: "PlayerAuthInput: InputFlags presence bool and double optionals simplified, PlayerInventoryAction.Actions unwrapped and Hand added in 2193",
	148: "ItemStackResponse: double optionals simplified to single optionals in 2193",
	174: "SubChunk: HeightMapData/RenderHeightMapData changed from flat 256 int8s to row-prefixed 16x16 HeightMap in 2193 (fixups.go)",
	180: "DimensionData: Range[max,min] replaced by MinimumY, HeightRange and DefaultBiome in 2193 (fixups.go)",
	198: "CameraPresets: ApplyInheritedStartingRotation and StartingRotation added in 2193; dropped going down",
	315: "ServerBoundDiagnostics: EntityDiagnosticTimingInfo Position and Dimension added in 2193; dropped going down",
	328: "DebugDrawer: TextShape.LineGapHeight added in 2193; dropped going down (hand_convert.go)",
	329: "ServerBoundPackSettingChange: PackSettingTypeStringList added in 2193; replaced by empty string going down (hand_convert.go)",
	345: "ClientBoundAttributeLayerSync: EnvironmentAttributeData.NoiseAlignment added in 2193; dropped going down",
}

// noForm lists packets that only exist in 2193 and are omitted going down to 2169.
var noForm = map[uint32]string{
	351: "SetPlayerFurnaceOptions: none going down",
	352: "RecordStarted: none going down",
}
