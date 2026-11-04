package server

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"time"
	_ "unsafe"

	"strings"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/identity"
	// The packages of the functions New calls through go:linkname must be
	// linked in, whether or not a listener imports them.
	_ "github.com/df-mc/dragonfly/server/item/creative"
	_ "github.com/df-mc/dragonfly/server/item/recipe"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/chat"
	"github.com/df-mc/dragonfly/server/player/playerdb"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/generator/advanced"
	"github.com/df-mc/dragonfly/server/world/mcdb"
)

// Config contains options for starting a Minecraft server.
type Config struct {
	// Log is the Logger to use for logging information. If nil, Log is set to
	// slog.Default(). Errors reported by the underlying network are only logged
	// if Log has at least debug level.
	Log *slog.Logger
	// Listeners is a list of functions to create a Listener using a Config, one
	// for each Listener to be added to the Server. Each is passed the Server
	// as the Host its listener shows to the clients that look it up. If left
	// empty, no players will be able to connect to the Server.
	Listeners []func(conf Config, host Host) (Listener, error)
	// Name is the name of the server. By default, it is shown to users in the
	// server list before joining the server and when opening the in-game menu.
	Name string
	// Allower may be used to specify what players can join the server and what
	// players cannot. By returning false in the Allow method, for example if
	// the player has been banned, will prevent the player from joining.
	Allower Allower
	// ReconnectPolicy controls what happens when a player tries to join while already connected.
	ReconnectPolicy ReconnectPolicy
	// IdentityKey is what the server keys its players by: identity.XUID,
	// their Xbox accounts, for a server whose listeners admit only players
	// Xbox Live vouched for, or identity.Name, their names. identity.Auto,
	// the zero value, keys them by name: the program resolves it from the
	// AuthEnabled key of config.toml as it composes the Config.
	IdentityKey identity.Mode
	// MaxPlayers is the maximum amount of players allowed to join the server at
	// once.
	MaxPlayers int
	// MaxChunkRadius is the maximum view distance that each player may have,
	// measured in chunks. A chunk radius generally leads to more memory usage.
	MaxChunkRadius int
	// JoinMessage, QuitMessage and ShutdownMessage are the messages to send for
	// when a player joins or quits the server and when the server shuts down,
	// kicking all online players. If set, JoinMessage and QuitMessage must have
	// exactly 1 argument, which will be replaced with the name of the player
	// joining or quitting.
	// ShutdownMessage must have no arguments. It is set to
	// chat.MessageServerDisconnect if empty.
	JoinMessage, QuitMessage, ShutdownMessage chat.Translation
	// StatusProvider provides the server status shown to players in the server
	// list. By default, StatusProvider will show the server name from the Name
	// field and the current player count and maximum players.
	StatusProvider StatusProvider
	// PlayerProvider is the player.Provider used for storing and loading player
	// data. If left as nil, player data will be newly created every time a
	// player joins the server and no data will be stored.
	PlayerProvider player.Provider
	// WorldProvider is the world.Provider used for storing and loading world
	// data. If left as nil, world data will be newly created every time and
	// chunks will always be newly generated when loaded. The world provider
	// will be used for storing/loading the default overworld, nether and end.
	WorldProvider world.Provider
	// ReadOnlyWorld specifies if the standard worlds should be read only. If
	// set to true, the WorldProvider won't be saved to at all.
	ReadOnlyWorld bool
	// SaveChunks selects which loaded chunks the worlds store when they save
	// and unload them: only those generated or changed since their last save,
	// the default, or all of them. See world.Config.SaveChunks.
	SaveChunks world.ChunkSaving
	// Generator constructs each dimension's generator using the resolved world
	// seed. Existing provider metadata takes precedence over OverworldSeed.
	// If nil, the built-in generators are used for all three dimensions.
	Generator func(dim world.Dimension, seed int64) world.Generator
	// GeneratorProfile selects how chunk generation uses the CPUs of the host.
	// The zero value picks a profile from the number of logical CPUs.
	GeneratorProfile world.GeneratorProfile
	// GeneratorWorkers controls the number of asynchronous workers dedicated
	// to generating chunks. If set to 0 or lower, GeneratorProfile selects the
	// worker count from the host's available CPUs. Consider raising this when
	// pre-generating terrain heavily, but profile if the generator is limited
	// by I/O (for example LevelDB operations).
	GeneratorWorkers int
	// GeneratorQueueSize is how many chunk generation jobs may wait for a
	// worker before a backlog warning is logged. Further jobs still queue:
	// chunk requests never wait for the workers. If set to 0 or lower, a size
	// proportional to the worker count will be chosen automatically. Raise
	// GeneratorWorkers if the logs report a sustained backlog.
	GeneratorQueueSize int
	// Regions lets the entities of parts of a world far apart, such as
	// around players far from each other, tick at the same time on all
	// cores. See world.Config.Regions.
	Regions bool
	// OverworldSeed is the seed for a new world in all dimensions. Existing
	// provider metadata takes precedence. Zero is a valid, deterministic seed.
	OverworldSeed int64
	// RandomTickSpeed specifies the rate at which blocks should be ticked in
	// the default worlds. Setting this value to -1 or lower will stop random
	// ticking altogether, while setting it higher results in faster ticking. If
	// left as 0, the RandomTickSpeed will default to a speed of 3 blocks per
	// sub chunk per tick (normal ticking speed).
	RandomTickSpeed int
	// SaveInterval is how often the server saves a checkpoint while saving is
	// on: its worlds, with their chunks, entities and level.dat, and the
	// players in them, captured at one instant. It runs from the end of the
	// last checkpoint. /save-all and closing the server save one too. The
	// worlds of a ReadOnlyWorld are not saved, but the players are. By
	// default, SaveInterval is DefaultSaveInterval, as Java's autosave. Setting
	// it to a negative number saves checkpoints only on /save-all and close.
	SaveInterval time.Duration
	// ChunkUnloadInterval specifies how often unused chunks should be unloaded
	// from memory when no longer in use. By default, this is set to 2 minutes.
	// ChunkUnloadInterval should not be used to prevent chunks from unloading
	// altogether. This should be done using a Loader with a custom Viewer.
	ChunkUnloadInterval time.Duration
	// Entities is a world.EntityRegistry with all entity types registered that
	// may be added to the Server's worlds. If no entity types are registered,
	// Entities will be set to entity.DefaultRegistry.
	Entities world.EntityRegistry
	// DisableOverworld disables creation of the overworld entirely. Nether and End portals that
	// target the overworld will still form, but they won't teleport entities while it is disabled.
	DisableOverworld bool
	// DisableNether disables creation of the Nether world and prevents Nether portals
	// from transporting players. The portal blocks can still be lit.
	DisableNether bool
	// DisableEnd disables creation of the End world and prevents End portals from
	// transporting players. The portal blocks can still be lit.
	DisableEnd bool
	// DefaultDimension controls which dimension new players spawn in and which world acts as the
	// server's standard dimension. If set to nil, the overworld is used.
	DefaultDimension world.Dimension
	// PortalDisabledMessage controls the chat message players receive when they try to
	// enter a portal that leads to a disabled dimension. If the string contains a
	// formatting directive such as %s, the name of the target dimension is passed as the
	// first argument. Set this to an empty string to disable the notification entirely.
	PortalDisabledMessage string
	// Blocks is the BlockRegistry template used for newly created worlds. If nil, world.DefaultBlockRegistry is used.
	// For a non-default registry, set this to world.NewBlockRegistry(), register blocks on that instance, and ensure
	// it is finalized before use.
	Blocks world.BlockRegistry
}

// DefaultSaveInterval is the SaveInterval of a Config that sets none: five
// minutes, as Java's autosave.
const DefaultSaveInterval = 5 * time.Minute

// ReconnectPolicy controls how the server handles duplicate logins for the same player.
type ReconnectPolicy uint8

const (
	// ReconnectKickJoining disconnects the connecting player when a session already exists.
	ReconnectKickJoining ReconnectPolicy = iota
	// ReconnectKickExisting disconnects the existing player and allows the new connection.
	ReconnectKickExisting
)

func parseReconnectPolicy(value string) (ReconnectPolicy, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "kick-joining", "kick_joining", "kick-connecting", "kick-connection", "kick-new":
		return ReconnectKickJoining, true
	case "kick-existing", "kick_existing", "kick-old", "kick-previous":
		return ReconnectKickExisting, true
	default:
		return 0, false
	}
}

// New creates a Server using fields of conf. The Server's worlds are created
// and connections from the Server's listeners may be accepted by calling
// Server.Listen() and Server.Accept() afterwards.
func (conf Config) New() *Server {
	if conf.Log == nil {
		conf.Log = slog.Default()
	}
	if len(conf.Listeners) == 0 {
		conf.Log.Warn("config: no listeners set, no connections will be accepted")
	}
	if conf.Name == "" {
		conf.Name = "Dragonfly Server"
	}
	if conf.StatusProvider == nil {
		conf.StatusProvider = statusProvider{name: conf.Name}
	}
	if conf.PlayerProvider == nil {
		conf.PlayerProvider = player.NopProvider{}
	}
	if conf.Allower == nil {
		conf.Allower = allower{}
	}
	if conf.WorldProvider == nil {
		conf.WorldProvider = world.NopProvider{}
	}
	if conf.Generator == nil {
		conf.Generator = defaultGenerator
	}
	if conf.MaxChunkRadius == 0 {
		conf.MaxChunkRadius = 12
	}
	if conf.SaveInterval == 0 {
		conf.SaveInterval = DefaultSaveInterval
	}
	if conf.ShutdownMessage.Zero() {
		conf.ShutdownMessage = chat.MessageServerDisconnect
	}
	if len(conf.Entities.Types()) == 0 {
		conf.Entities = entity.DefaultRegistry
	}
	if conf.Blocks == nil {
		conf.Blocks = world.DefaultBlockRegistry
	}
	if conf.IdentityKey == identity.Auto {
		conf.Log.Info("Players are keyed by name: Config.IdentityKey is identity.Auto, which a Server keys by name.")
		conf.IdentityKey = identity.Name
	}
	if a, ok := conf.Allower.(admission); ok {
		// The lists UserConfig.Config made follow the mode of the Server.
		a.mode = conf.IdentityKey
		conf.Allower = a
	}

	// Initialize the passed block registry and also initialize the default block registry which
	// is used in some vanilla paths.
	conf.Blocks.Finalize()
	world.DefaultBlockRegistry.Finalize()
	// Structure blocks load the templates of the vanilla data pack, as Java's
	// StructureTemplateManager does.
	world.SetBuiltinStructureTemplates(advanced.VanillaStructureTemplate)
	conf.OverworldSeed = conf.WorldProvider.Settings().ResolveSeed(conf.OverworldSeed)

	srv := &Server{
		conf:       conf,
		incoming:   make(chan incoming),
		dimensions: make(map[world.Dimension]*world.World),
		idleStop:   make(chan struct{}),
	}
	srv.checkpoints.srv, srv.checkpoints.stop, srv.checkpoints.done = srv, make(chan struct{}), make(chan struct{})
	if wl, ok := conf.Allower.(*Whitelist); ok {
		srv.whitelist = wl
	} else if holder, ok := conf.Allower.(interface{ underlyingWhitelist() *Whitelist }); ok {
		srv.whitelist = holder.underlyingWhitelist()
	}
	if access, ok := conf.Allower.(interface {
		adminAccess() (*operatorsList, *banList)
	}); ok {
		srv.operators, srv.bans = access.adminAccess()
	}
	if access, ok := conf.Allower.(interface{ knownAccess() *knownPlayers }); ok && conf.IdentityKey == identity.XUID {
		srv.known = access.knownAccess()
	}
	for _, lf := range conf.Listeners {
		l, err := lf(conf, srv)
		if err != nil {
			conf.Log.Error("create listener: " + err.Error())
			continue
		}
		if l == nil {
			conf.Log.Error("create listener: returned nil listener")
			continue
		}
		srv.listeners = append(srv.listeners, l)
	}

	creative_registerCreativeItems()
	recipe_registerVanilla()

	defaultDim := conf.DefaultDimension
	if defaultDim == nil {
		defaultDim = world.Overworld
	}
	if conf.dimensionDisabled(defaultDim) {
		if fallback, ok := conf.firstEnabledDimension(); ok {
			conf.Log.Warn("Default dimension disabled, falling back.", "default", strings.ToLower(fmt.Sprint(defaultDim)), "fallback", strings.ToLower(fmt.Sprint(fallback)))
			defaultDim = fallback
		} else {
			panic("config: at least one dimension must remain enabled")
		}
	}

	srv.defaultDimension = defaultDim

	srv.world = srv.createWorld(defaultDim)
	srv.registerWorld(defaultDim, srv.world)

	for _, dim := range []world.Dimension{world.Overworld, world.Nether, world.End} {
		if dim == defaultDim {
			continue
		}
		if conf.dimensionDisabled(dim) {
			conf.Log.Info("Skipping dimension load: dimension disabled", "dimension", strings.ToLower(fmt.Sprint(dim)))
			continue
		}
		w := srv.createWorld(dim)
		srv.registerWorld(dim, w)
	}

	return srv
}

func (conf Config) portalDisabledMessage(dim world.Dimension) string {
	if conf.PortalDisabledMessage == "" {
		return ""
	}
	name := fmt.Sprint(dim)
	if strings.Contains(conf.PortalDisabledMessage, "%") {
		return fmt.Sprintf(conf.PortalDisabledMessage, name)
	}
	return fmt.Sprintf("%s (%s)", conf.PortalDisabledMessage, name)
}

func (conf Config) dimensionDisabled(dim world.Dimension) bool {
	switch dim {
	case world.Overworld:
		return conf.DisableOverworld
	case world.Nether:
		return conf.DisableNether
	case world.End:
		return conf.DisableEnd
	}
	return false
}

func (conf Config) firstEnabledDimension() (world.Dimension, bool) {
	for _, dim := range []world.Dimension{world.Overworld, world.Nether, world.End} {
		if !conf.dimensionDisabled(dim) {
			return dim, true
		}
	}
	return nil, false
}

func parseDimension(name string) (world.Dimension, bool) {
	switch strings.ToLower(name) {
	case "", "overworld", "world", "default":
		return world.Overworld, true
	case "nether", "hell":
		return world.Nether, true
	case "end", "the_end", "end_dimension":
		return world.End, true
	}
	return nil, false
}

// UserConfig is the user configuration for a Dragonfly server. It holds
// settings that affect different aspects of the server, such as its name and
// maximum players: the keys of config.toml that the server owns, beside
// those the protocol adapters own in the same file. UserConfig may be
// serialised and can be converted to a Config by calling UserConfig.Config().
type UserConfig struct {
	Server struct {
		// Name is the name of the server as it shows up in the server list.
		Name string
		// DisableJoinQuitMessages specifies if default join and quit messages
		// for players should be disabled.
		DisableJoinQuitMessages bool
		// ReconnectPolicy controls what happens when a player connects while already online.
		// Valid values: "kick-joining" or "kick-existing".
		ReconnectPolicy string
		// IdentityKey is what the whitelist, the operators, the bans and
		// the data of players are keyed by: "xuid", the Xbox account;
		// "name", the name, which anyone can take without Xbox Live
		// sign-in; "auto", "xuid" if AuthEnabled is true and "name"
		// otherwise. The program resolves "auto" as it composes the file;
		// Config, given "auto" or "", keys by name.
		IdentityKey string
	}
	World struct {
		// SaveData controls whether a world's data will be saved and loaded.
		// If true, the server will use the default LevelDB data provider and if
		// false, an empty provider will be used. To use your own provider, turn
		// this value to false, as you will still be able to pass your own
		// provider.
		SaveData bool
		// Folder is the folder that the data of the world resides in.
		Folder string
		// SaveChunks selects which loaded chunks a save or an unload stores:
		// "modified", the default, stores those generated or changed since
		// they were last saved and those holding entities or block entities;
		// "all" stores every chunk loaded, changed or not, as pregenerating
		// a world wants. Any other value stops the server at start.
		SaveChunks string
		// Seed selects the generation seed for a new world in all dimensions.
		// Existing level.dat metadata takes precedence, including seed zero.
		Seed int64
		// SaveInterval is how often the server saves its worlds and the
		// players in them, all at one instant, as a duration such as "5m"
		// or "90s". Set it to "0s" to save only on /save-all and when the
		// server stops.
		SaveInterval string
		// GeneratorProfile tunes chunk generation for the CPU of the host:
		// "fast-cores" for a few fast cores (desktop CPUs), "many-cores" for
		// many slower cores (multi-socket servers), or "auto" to pick one from
		// the number of logical CPUs.
		GeneratorProfile string
		// GeneratorWorkers is the number of background workers that should be
		// dedicated to generating chunks. Set to 0 to let GeneratorProfile
		// select it from the host's CPU count.
		GeneratorWorkers int
		// GeneratorQueueSize is how many chunk generation jobs may wait for a
		// worker before a backlog warning is logged. Set to 0 to use an
		// automatically chosen size.
		GeneratorQueueSize int
		// Regions ticks the mobs around players far from each other at the
		// same time on all cores. Plugins must then expect the handlers of
		// players and other entities to be called from several goroutines
		// at once.
		Regions bool
		// DisableOverworld disables the overworld dimension entirely. Nether and End portals can still be activated,
		// but will not teleport entities to the overworld while it is disabled.
		DisableOverworld bool
		// DisableNether disables the Nether dimension entirely. Nether portals can still be activated
		// but will not teleport entities.
		DisableNether bool
		// DisableEnd disables the End dimension entirely. End portals can still be activated but will
		// not teleport entities.
		DisableEnd bool
		// DefaultDimension controls which dimension new players spawn in. Valid values are "overworld",
		// "nether" and "end". Defaults to "overworld".
		DefaultDimension string
		// PortalDisabledMessage controls the chat message that is sent when a player enters a portal
		// leading to a disabled dimension. The dimension name is passed as the first formatting argument.
		// Leave empty to suppress the notification entirely.
		PortalDisabledMessage string
	}
	Players struct {
		// MaxCount is the maximum amount of players allowed to join the server
		// at the same time. If set to 0, the amount of maximum players will
		// grow every time a player joins.
		MaxCount int
		// MaximumChunkRadius is the maximum chunk radius that players may set
		// in their settings. If they try to set it above this number, it will
		// be capped and set to the max.
		MaximumChunkRadius int
		// SaveData controls whether a player's data will be saved and loaded.
		// If true, the server will use the default LevelDB data provider and if
		// false, an empty provider will be used. To use your own provider, turn
		// this value to false, as you will still be able to pass your own
		// provider.
		SaveData bool
		// Folder controls where the player data will be stored by the default
		// LevelDB player provider if it is enabled.
		Folder string
	}
	Whitelist struct {
		// Enabled controls if the whitelist should be enforced for players attempting to join.
		Enabled bool
		// File is the path to the whitelist file that stores player names.
		// Supported formats:
		//   - .txt/.list/.enum: line-separated list, one player name per line.
		//   - .toml: legacy TOML format with a "players" array.
		File string
		// Reason is the message sent to players that are rejected by the whitelist.
		// If empty, a safe default is used.
		Reason string
	}
}

// Config converts a UserConfig to a Config, so that it may be used for creating
// a Server. An error is returned if creating data providers or loading the
// whitelist, operators or bans failed. The Config has no listener: the
// listeners of the protocol adapters, such as Config.Listener of package
// protocol/bedrock, are appended to its Listeners.
func (uc UserConfig) Config(log *slog.Logger) (Config, error) {
	var err error
	defaultDim := world.Dimension(world.Overworld)
	if name := strings.TrimSpace(uc.World.DefaultDimension); name != "" {
		if parsed, ok := parseDimension(name); ok {
			defaultDim = parsed
		} else if log != nil {
			log.Warn("Unknown default dimension, using overworld.", "value", name)
		}
	}

	generatorProfile, err := world.ParseGeneratorProfile(uc.World.GeneratorProfile)
	if err != nil {
		return Config{}, err
	}
	saveInterval, err := parseSaveInterval(uc.World.SaveInterval)
	if err != nil {
		return Config{}, err
	}

	saveChunks, err := world.ParseChunkSaving(uc.World.SaveChunks)
	if err != nil {
		return Config{}, err
	}

	identityKey := identity.Auto
	if value := strings.TrimSpace(uc.Server.IdentityKey); value != "" {
		if identityKey, err = identity.ParseMode(value); err != nil {
			return Config{}, err
		}
	}

	conf := Config{
		Log:                   log,
		Name:                  uc.Server.Name,
		IdentityKey:           identityKey,
		ReconnectPolicy:       ReconnectKickExisting,
		MaxPlayers:            uc.Players.MaxCount,
		MaxChunkRadius:        uc.Players.MaximumChunkRadius,
		OverworldSeed:         uc.World.Seed,
		SaveInterval:          saveInterval,
		GeneratorProfile:      generatorProfile,
		SaveChunks:            saveChunks,
		GeneratorWorkers:      uc.World.GeneratorWorkers,
		GeneratorQueueSize:    uc.World.GeneratorQueueSize,
		Regions:               uc.World.Regions,
		DisableOverworld:      uc.World.DisableOverworld,
		DisableNether:         uc.World.DisableNether,
		DisableEnd:            uc.World.DisableEnd,
		DefaultDimension:      defaultDim,
		PortalDisabledMessage: uc.World.PortalDisabledMessage,
	}
	whitelistFile := strings.TrimSpace(uc.Whitelist.File)
	if whitelistFile == "" {
		whitelistFile = "white-list.txt"
	}
	if !uc.Server.DisableJoinQuitMessages {
		conf.JoinMessage, conf.QuitMessage = chat.MessageJoin, chat.MessageQuit
	}
	if policyValue := strings.TrimSpace(uc.Server.ReconnectPolicy); policyValue != "" {
		if parsed, ok := parseReconnectPolicy(policyValue); ok {
			conf.ReconnectPolicy = parsed
		} else if log != nil {
			log.Warn("Unknown reconnect policy, using kick-existing.", "value", policyValue)
		}
	}
	if uc.World.SaveData {
		conf.WorldProvider, err = mcdb.Config{Log: log}.Open(uc.World.Folder)
		if err != nil {
			return conf, fmt.Errorf("create world provider: %w", err)
		}
	}
	wl, err := LoadWhitelist(whitelistFile)
	if err != nil {
		return conf, fmt.Errorf("load whitelist: %w", err)
	}
	wl.SetEnabled(uc.Whitelist.Enabled)
	wl.SetKickMessage(uc.Whitelist.Reason)

	// Keep operator and ban data beside the configured whitelist.
	baseDir := filepath.Dir(whitelistFile)
	ops, err := loadOperators(filepath.Join(baseDir, "ops.json"))
	if err != nil {
		return conf, fmt.Errorf("load ops list: %w", err)
	}
	bans, err := loadBans(filepath.Join(baseDir, "banned-players.json"))
	if err != nil {
		return conf, fmt.Errorf("load bans: %w", err)
	}
	if uc.Players.SaveData {
		conf.PlayerProvider, err = playerdb.NewProvider(uc.Players.Folder)
		if err != nil {
			return conf, fmt.Errorf("create player provider: %w", err)
		}
	}
	a := admission{mode: conf.IdentityKey, log: log, whitelist: wl, operators: ops, bans: bans}
	if conf.IdentityKey == identity.XUID {
		var existed bool
		if a.known, existed, err = loadKnownPlayers(filepath.Join(baseDir, "known-players.json"), log); err != nil {
			return conf, fmt.Errorf("load known players: %w", err)
		}
		if !existed {
			lister, _ := conf.PlayerProvider.(player.Lister)
			if err := moveToAccounts(a, lister); err != nil {
				return conf, fmt.Errorf("move the lists to Xbox accounts: %w", err)
			}
		}
	}
	a.warnHeld()
	conf.Allower = a
	return conf, nil
}

// parseSaveInterval parses the World.SaveInterval of a UserConfig into the
// SaveInterval of a Config: the default if it is empty, and a negative
// interval, which saves no periodic checkpoints, if it is zero or less.
func parseSaveInterval(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("World.SaveInterval: %w", err)
	}
	if d <= 0 {
		return -1, nil
	}
	return d, nil
}

// defaultGenerator constructs a dimension generator using the resolved seed.
// The overworld, nether and end still utilise Java 1.12 style generators.
func defaultGenerator(dim world.Dimension, seed int64) world.Generator {
	switch dim {
	case world.Overworld:
		return advanced.NewOverworld(seed)
	case world.Nether:
		return advanced.NewNether(seed)
	case world.End:
		return advanced.NewEnd(seed)
	}
	panic("should never happen")
}

// DefaultConfig returns a configuration with the default values filled out.
func DefaultConfig() UserConfig {
	c := UserConfig{}
	c.Server.Name = "Dragonfly Server"
	c.World.SaveData = true
	c.World.Folder = "world"
	c.World.SaveChunks = "modified"
	c.World.Seed = 0
	c.World.SaveInterval = "5m"
	c.World.GeneratorProfile = "auto"
	c.Server.ReconnectPolicy = "kick-existing"
	c.Server.IdentityKey = "auto"
	c.World.DisableOverworld = false
	c.World.DisableNether = false
	c.World.DisableEnd = false
	c.World.DefaultDimension = "overworld"
	c.World.PortalDisabledMessage = "The %s dimension is disabled on this server."
	c.Players.MaximumChunkRadius = 32
	c.Players.SaveData = true
	c.Players.Folder = "players"
	c.Whitelist.File = "white-list.txt"
	c.Whitelist.Reason = defaultWhitelistKickMessage
	return c
}

// noinspection ALL
//
//go:linkname creative_registerCreativeItems github.com/df-mc/dragonfly/server/item/creative.registerCreativeItems
func creative_registerCreativeItems()

// noinspection ALL
//
//go:linkname recipe_registerVanilla github.com/df-mc/dragonfly/server/item/recipe.registerVanilla
func recipe_registerVanilla()
