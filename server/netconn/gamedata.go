package netconn

import "github.com/dm-vev/adamant/net/bedrock/proto/v2193"

// GameData is what the server tells a client that starts the game: the
// world and the player it spawns as. The connection fills in the rest of
// the StartGame packet.
type GameData struct {
	// WorldName is the name of the world; the name of the server if empty.
	WorldName string
	// WorldSeed is the seed of the world.
	WorldSeed int64
	// Difficulty is the difficulty of the world.
	Difficulty int32
	// EntityUniqueID and EntityRuntimeID are the IDs of the player itself.
	EntityUniqueID  int64
	EntityRuntimeID uint64
	// PlayerGameMode is the game mode of the player.
	PlayerGameMode int32
	// PersonaDisabled and CustomSkinsDisabled turn off persona and custom
	// skins.
	PersonaDisabled, CustomSkinsDisabled bool
	// EmoteChatMuted hides the chat messages of emotes.
	EmoteChatMuted bool
	// BaseGameVersion is the version of the game the vanilla content of
	// the world is of.
	BaseGameVersion string
	// PlayerPosition, Pitch and Yaw are where the player spawns.
	PlayerPosition [3]float32
	Pitch, Yaw     float32
	// Dimension is the dimension the player spawns in.
	Dimension int32
	// WorldSpawn is the spawn of the world.
	WorldSpawn [3]int32
	// EditorWorldType, CreatedInEditor and ExportedFromEditor describe a
	// world of the editor.
	EditorWorldType                     int32
	CreatedInEditor, ExportedFromEditor bool
	// WorldGameMode is the default game mode of the world.
	WorldGameMode int32
	// Hardcore is the hardcore mode of the world.
	Hardcore bool
	// XBLBroadcastMode is who sees the game on Xbox Live.
	XBLBroadcastMode int32
	// GameRules are the game rules the client knows of.
	GameRules []v2193.GameRule
	// Time is the time of the world.
	Time int64
	// DayCycleLockTime is the time the day cycle stopped at.
	DayCycleLockTime int32
	// CustomBlocks are the blocks the server adds to the game.
	CustomBlocks []v2193.BlockEntry
	// Items are all items of the game, vanilla and custom.
	Items []v2193.ItemEntry
	// PlayerMovementSettings sets how movement is authoritative.
	PlayerMovementSettings v2193.PlayerMovementSettings
	// ServerAuthoritativeInventory makes the server own the inventory.
	ServerAuthoritativeInventory bool
	// Experiments are the experiments of the world.
	Experiments []v2193.ExperimentData
	// PlayerPermissions is the permission level of the player.
	PlayerPermissions uint8
	// ChunkRadius, if not zero, is the chunk radius the client gets,
	// whatever it asks for.
	ChunkRadius int32
	// ClientSideGeneration lets the client generate chunks.
	ClientSideGeneration bool
	// ChatRestrictionLevel restricts the chat.
	ChatRestrictionLevel uint8
	// DisablePlayerInteractions turns off interactions between players.
	DisablePlayerInteractions bool
	// UseBlockNetworkIDHashes names blocks by the hash of their state
	// rather than by their index in the palette.
	UseBlockNetworkIDHashes bool
	// PropertyData are the properties of the player entity.
	PropertyData map[string]any
	// Dimensions are the dimensions the server adds to the game.
	Dimensions []v2193.DimensionDefinition
}

// ServerStatus is the status of the server that clients look up before
// joining.
type ServerStatus struct {
	// ServerName is the name of the server; ServerSubName is shown below.
	ServerName, ServerSubName string
	// PlayerCount and MaxPlayers are the players online and how many may
	// join.
	PlayerCount, MaxPlayers int
}

// StatusProvider gives the status of the server.
type StatusProvider interface {
	// ServerStatus returns the status of the server with the players the
	// listener counts.
	ServerStatus(playerCount, maxPlayers int) ServerStatus
}

// statusProvider is a StatusProvider of a fixed name.
type statusProvider struct{ name, subName string }

func (s statusProvider) ServerStatus(playerCount, maxPlayers int) ServerStatus {
	return ServerStatus{ServerName: s.name, ServerSubName: s.subName, PlayerCount: playerCount, MaxPlayers: maxPlayers}
}

// NewStatusProvider returns a StatusProvider of a fixed name and subname.
func NewStatusProvider(name, subName string) StatusProvider {
	return statusProvider{name, subName}
}
