package server

import (
	"context"
	"fmt"
	"iter"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/text"
	"github.com/df-mc/dragonfly/server/version"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
	"golang.org/x/text/language"
	_ "github.com/df-mc/dragonfly/server/item" // Imported for maintaining correct initialisation order.
)

// Server implements a Dragonfly server. It runs the main server loop and
// handles the connections of players trying to join the server.
type Server struct {
	conf Config

	once    sync.Once
	started atomic.Pointer[time.Time]

	world *world.World
	// dimensions holds the loaded dimensions keyed by their identifiers.
	dimensions       map[world.Dimension]*world.World
	defaultDimension world.Dimension

	// start is the content of the game as the client is defined it as its
	// game starts, made once the server starts listening.
	start       *session.StartContent
	whitelist   *Whitelist
	operators   *operatorsList
	bans        *banList
	idleTimeout atomic.Int64
	idleStop    chan struct{}
	listeners   []Listener
	incoming    chan incoming

	// players holds the players online.
	players players
	// pwg is a sync.WaitGroup used to wait for all players to be disconnected
	// before server shutdown, so that their data is saved properly.
	pwg sync.WaitGroup
	// wg is used to wait for all Listeners to be closed and their respective
	// goroutines to be finished.
	wg sync.WaitGroup
}

// incoming holds data of a player that is connecting to the server.
type incoming struct {
	conf player.Config
	s    *session.Session
	p    *onlinePlayer
	w    *world.World
}

// New creates a Server using a default Config. The Server's worlds are created
// and connections from the Server's listeners may be accepted by calling
// Server.Listen() and Server.Accept() afterwards.
func New() *Server {
	var conf Config
	return conf.New()
}

// Listen starts running the server's listeners. Connections will be accepted
// until the listeners are closed using a call to Close. Once Listen is called,
// players may be accepted using Server.Accept().
func (srv *Server) Listen() {
	t := time.Now()
	if !srv.started.CompareAndSwap(nil, &t) {
		panic("start server: already started")
	}

	info, _ := debug.ReadBuildInfo()
	if info == nil {
		info = &debug.BuildInfo{GoVersion: "N/A", Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "N/A"}}}
	}
	revision := ""
	for _, set := range info.Settings {
		if set.Key == "vcs.revision" {
			revision = set.Value
		}
	}

	srv.conf.Log.Info("Dragonfly server started.", "mc-version", version.Minecraft, "go-version", info.GoVersion, "commit", revision)
	srv.startListening()
	go srv.wait()
	go srv.idleTimeoutLoop()
}

// Accept accepts incoming players into the server, returning an iterator that
// yields players that join the server while blocking otherwise. The iterator
// returned ends when the Server is closed using a call to Close. The loop body
// runs on the player's world owner: blocking there stalls that world, and
// calling world.Call, world.CallEntity, world.CallRef, or Task.Wait for the
// same owner deadlocks. Players returned are only valid within the block of the
// for loop used to iterate over them. Use p.H() with player.Do for work that
// outlives the loop body:
//
//	for p := range srv.Accept() {
//	  // p is valid here
//	  go func() {
//	    // p is no longer valid here
//	  }()
//	}
//	// p is no longer valid here
func (srv *Server) Accept() iter.Seq[*player.Player] {
	return func(yield func(*player.Player) bool) {
		for {
			inc, ok := <-srv.incoming
			if !ok {
				return
			}
			srv.players.add(inc.p)

			ret, err := world.Call(context.Background(), inc.w, func(tx *world.Tx) (bool, error) {
				p := tx.AddEntity(inc.p.handle).(*player.Player)
				inc.s.Spawn(p, tx)
				return !yield(p), nil
			})
			if err != nil {
				srv.players.remove(inc.p.handle.UUID())
				srv.pwg.Done()
				// Join failed before spawn: the entity was never added, so close
				// the orphaned handle and fully tear the session down (Disconnect
				// only writes a packet; CloseConnection frees the conn/goroutines;
				// Unbind removes the player from the player list of the others).
				_ = inc.p.handle.Close()
				inc.s.Disconnect(text.Literal("join failed"))
				inc.s.CloseConnection()
				inc.s.Unbind()
				continue
			}
			if ret {
				return
			}
		}
	}
}

// World returns the server's standard world. Players will be spawned in this
// world and this world will be read from and written to when the world is
// edited. The default world may be configured to any supported dimension.
func (srv *Server) World() *world.World {
	return srv.world
}

// StartTime returns the time the server started listening for connections.
func (srv *Server) StartTime() time.Time {
	if t := srv.started.Load(); t != nil {
		return *t
	}
	return time.Time{}
}

// Nether returns the nether world of the server. Players are transported to it
// when entering a nether portal in the world returned by the World method.
func (srv *Server) Nether() *world.World {
	return srv.dimensions[world.Nether]
}

// End returns the end world of the server. Players are transported to it when
// entering an end portal in the world returned by the World method.
func (srv *Server) End() *world.World {
	return srv.dimensions[world.End]
}

// MaxPlayerCount returns the maximum amount of players that are allowed to
// play on the server at the same time. Players trying to join when the server
// is full will be refused to enter. If the config has a maximum player count
// set to 0, MaxPlayerCount will return Server.PlayerCount + 1.
func (srv *Server) MaxPlayerCount() int {
	if srv.conf.MaxPlayers == 0 {
		return srv.players.count() + 1
	}
	return srv.conf.MaxPlayers
}

// PlayerCount returns the total number of players connected to the Server.
func (srv *Server) PlayerCount() int {
	return srv.players.count()
}

// WhitelistEnabled reports if the whitelist is currently enforced on the server.
func (srv *Server) WhitelistEnabled() bool {
	if srv.whitelist == nil {
		return false
	}
	return srv.whitelist.Enabled()
}

// WhitelistSetEnabled updates whether the whitelist is enforced.
func (srv *Server) WhitelistSetEnabled(enabled bool) error {
	if srv.whitelist == nil {
		return ErrWhitelistUnavailable
	}
	srv.whitelist.SetEnabled(enabled)
	return nil
}

// WhitelistReload refreshes the whitelist contents from disk.
func (srv *Server) WhitelistReload() error {
	if srv.whitelist == nil {
		return ErrWhitelistUnavailable
	}
	return srv.whitelist.Reload()
}

// WhitelistAdd adds a player name to the whitelist.
func (srv *Server) WhitelistAdd(name string) (bool, error) {
	if srv.whitelist == nil {
		return false, ErrWhitelistUnavailable
	}
	return srv.whitelist.Add(name)
}

// WhitelistRemove removes a player name from the whitelist.
func (srv *Server) WhitelistRemove(name string) (bool, error) {
	if srv.whitelist == nil {
		return false, ErrWhitelistUnavailable
	}
	return srv.whitelist.Remove(name)
}

// WhitelistEntries returns the list of names currently present in the whitelist.
func (srv *Server) WhitelistEntries() ([]string, error) {
	if srv.whitelist == nil {
		return nil, ErrWhitelistUnavailable
	}
	return srv.whitelist.Players(), nil
}

// Players returns an iterator that yields players currently online. If Players
// is called from within a transaction, the respective transaction should be
// passed. Passing nil is otherwise valid. Each loop body runs on the yielded
// player's world owner, so blocking stalls that world and calling world.Call,
// world.CallEntity, world.CallRef, or Task.Wait for the same owner deadlocks.
// Players in other worlds are yielded by blocking on those owners sequentially;
// mirrored handlers in two worlds can therefore deadlock each other. For
// fan-out, collect Player.H values and schedule each with player.Do instead.
// Players returned are only valid within the block of the for loop used to
// iterate over them:
//
//	for p := range srv.Players(nil) {
//	  // p is valid here
//	  go func() {
//	    // p is no longer valid here
//	  }()
//	}
//	// p is no longer valid here
//
// Collecting all values from the iterator using a function such as
// slices.Collect immediately invalidates the players because their transactions
// will be finished. Use Player.H(), player.NewRef, or player.Do when a
// player must be referenced after the iterator callback returns.
func (srv *Server) Players(tx *world.Tx) iter.Seq[*player.Player] {
	handles := srv.players.handles()

	return func(yield func(*player.Player) bool) {
		for _, handle := range handles {
			if tx != nil {
				if e, ok := handle.Entity(tx); ok {
					if !yield(e.(*player.Player)) {
						break
					}
					continue
				}
			}
			ret, err := player.Call(context.Background(), handle, func(_ *world.Tx, p *player.Player) (bool, error) {
				return !yield(p), nil
			})
			if err != nil {
				continue
			}
			if ret {
				break
			}
		}
	}
}

// Player looks for a player on the server with the UUID passed. If found, the
// entity handle is returned and the bool returns holds a true value. If not,
// the bool returned is false and the handle is nil.
func (srv *Server) Player(uuid uuid.UUID) (*world.EntityHandle, bool) {
	return srv.players.handle(uuid)
}

// PlayerByName looks for a player on the server with the name passed. If
// found, the entity handle is returned and the bool returned holds a true
// value. If not, the bool is false and the handle is nil
func (srv *Server) PlayerByName(name string) (*world.EntityHandle, bool) {
	return srv.players.byName(name)
}

// PlayerByXUID looks for a player on the server with the XUID passed. If
// found, the entity handle is returned and the bool returned is true. If no
// player with the XUID was found, nil and false are returned.
func (srv *Server) PlayerByXUID(xuid string) (*world.EntityHandle, bool) {
	return srv.players.byXUID(xuid)
}

// CloseOnProgramEnd closes the server right before the program ends, so that
// all data of the server are saved properly.
func (srv *Server) CloseOnProgramEnd() {
	c := make(chan os.Signal, 2)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-c
		if err := srv.Close(); err != nil {
			srv.conf.Log.Error("close server: " + err.Error())
		}
	}()
}

// Close closes the server, making any call to Run/Accept cancel immediately.
func (srv *Server) Close() error {
	if srv.started.Load() == nil {
		panic("server not yet running")
	}
	srv.once.Do(srv.close)
	return nil
}

// close stops the server, storing player and world data to disk.
func (srv *Server) close() {
	close(srv.idleStop)

	srv.conf.Log.Debug("Disconnecting players...")
	for p := range srv.Players(nil) {
		p.Disconnect(srv.conf.ShutdownMessage.F().Text(p.Locale()))
	}
	srv.pwg.Wait()

	srv.conf.Log.Debug("Closing player provider...")
	if err := srv.conf.PlayerProvider.Close(); err != nil {
		srv.conf.Log.Error("Close player provider: " + err.Error())
	}

	srv.conf.Log.Debug("Closing worlds...")
	closed := make(map[*world.World]struct{})
	for _, w := range srv.dimensions {
		if w == nil {
			continue
		}
		if _, ok := closed[w]; ok {
			continue
		}
		if err := w.Close(); err != nil {
			srv.conf.Log.Error(fmt.Sprintf("Close dimension %v: ", w.Dimension()) + err.Error())
		}
		closed[w] = struct{}{}
	}

	srv.conf.Log.Debug("Closing listeners...")
	for _, l := range srv.listeners {
		if err := l.Close(); err != nil {
			srv.conf.Log.Error("Close listener: " + err.Error())
		}
	}
}

// listen makes the Server listen for new connections from the Listener passed.
// This may be used to listen for players on different interfaces. Note that
// the maximum player count of additional Listeners added is not enforced
// automatically. The limit must be enforced by the Listener.
func (srv *Server) listen(l Listener) {
	wg := new(sync.WaitGroup)
	ctx, cancel := context.WithCancel(context.Background())
	for {
		c, err := l.Accept()
		if err != nil {
			// Cancel the context so that any call to StartGameContext is
			// cancelled rapidly.
			cancel()
			// First wait until all connections that are being handled are done
			// inserting the player into the channel. Afterwards, when we're
			// sure no more values will be inserted in the players channel, we
			// can return so the player channel can be closed.
			wg.Wait()
			srv.wg.Done()
			return
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			srv.finaliseConn(ctx, c, l)
		}()
	}
}

// startListening starts making the EncodeBlock listener listen, accepting new
// connections from players.
func (srv *Server) startListening() {
	srv.start = session.NewStartContent(srv.conf.Blocks, world.CustomItems(), world.CustomDimensions())

	srv.wg.Add(len(srv.listeners))
	for _, l := range srv.listeners {
		go srv.listen(l)
	}
}

// wait awaits the closing of all Listeners added to the Server through a call
// to listen and closed the players channel once that happens.
func (srv *Server) wait() {
	srv.wg.Wait()
	srv.conf.Log.Info("Server closed.", "uptime", time.Since(*srv.started.Load()).String())
	close(srv.incoming)
}

// finaliseConn finalises the session.Conn passed and adds it to the incoming
// channel.
func (srv *Server) finaliseConn(ctx context.Context, conn session.Conn, l Listener) {
	id := uuid.MustParse(conn.IdentityData().Identity)
	// A player still online leaves before the joining one is loaded: the
	// server then loads what it saved of the player as it left.
	if handle, ok := srv.Player(id); ok {
		if !srv.handleReconnect(l, conn, id, handle) {
			return
		}
	}

	var (
		fallback  bool
		requested world.Dimension
	)
	d, w, err := srv.conf.PlayerProvider.Load(id, func(dim world.Dimension) *world.World {
		requested = dim
		dest := srv.dimension(dim)
		if dest == nil {
			return srv.world
		}
		if dest.Dimension() != dim {
			fallback = true
		}
		return dest
	})
	if err != nil || fallback {
		w = srv.world
		<-w.Exec(func(tx *world.Tx) {
			d.Position = tx.PlayerSpawnPosition(w.Spawn())
		})
		d.GameMode = w.DefaultGameMode()
		if fallback {
			d.Velocity = mgl64.Vec3{}
			srv.conf.Log.Info("Relocating player from disabled dimension.", "requested", fmt.Sprint(requested), "target", fmt.Sprint(w.Dimension()))
		}
	}

	g := srv.game(conn.IdentityData().DisplayName, d, w)
	if err := session.StartGame(ctx, conn, g, srv.start, srv.conf.MuteEmoteChat); err != nil {
		_ = l.Disconnect(conn, "Connection timeout.")

		srv.conf.Log.Debug("spawn failed: "+err.Error(), "raddr", conn.RemoteAddr())
		return
	}
	srv.incoming <- srv.createPlayer(conn, d, w)
}

// game returns the Game a client that logged in as the player of the name
// passed starts in, as the player d in w.
func (srv *Server) game(name string, d player.Config, w *world.World) client.Game {
	return client.Game{
		Name:           srv.conf.Name,
		Level:          srv.world,
		World:          w,
		Position:       d.Position,
		Rotation:       d.Rotation,
		Operator:       srv.IsOperator(name),
		MaxChunkRadius: srv.conf.MaxChunkRadius,
		JoinMessage:    srv.conf.JoinMessage,
		QuitMessage:    srv.conf.QuitMessage,
		IsOperator:     srv.IsOperator,
		Left:           srv.handleSessionClose,
	}
}

func (srv *Server) handleReconnect(l Listener, conn session.Conn, id uuid.UUID, handle *world.EntityHandle) bool {
	switch srv.conf.ReconnectPolicy {
	case ReconnectKickExisting:
		player.Do(handle, func(_ *world.Tx, p *player.Player) {
			p.Disconnect("Logged in from another location.")
		})
		if srv.waitForPlayerRemoval(id, 5*time.Second) {
			return true
		}
		_ = l.Disconnect(conn, "Already logged in.")
		srv.conf.Log.Debug("spawn failed: already logged in", "raddr", conn.RemoteAddr())
		return false
	default:
		_ = l.Disconnect(conn, "Already logged in.")
		srv.conf.Log.Debug("spawn failed: already logged in", "raddr", conn.RemoteAddr())
		return false
	}
}

func (srv *Server) waitForPlayerRemoval(id uuid.UUID, timeout time.Duration) bool {
	timer := time.NewTimer(timeout)
	ticker := time.NewTicker(25 * time.Millisecond)
	defer timer.Stop()
	defer ticker.Stop()

	for {
		if _, ok := srv.Player(id); !ok {
			return true
		}
		select {
		case <-ticker.C:
		case <-timer.C:
			return false
		}
	}
}

// dimension returns a world by a dimension passed.
func (srv *Server) dimension(dimension world.Dimension) *world.World {
	if w := srv.dimensions[dimension]; w != nil {
		return w
	}
	return srv.world
}

// handleSessionClose handles the closing of a session. It removes the player
// of the session from the server.
func (srv *Server) handleSessionClose(tx *world.Tx, c session.Controllable) {
	// Always decrement the wait group because createPlayer unconditionally increments it.
	defer srv.pwg.Done()

	if _, ok := srv.players.handle(c.UUID()); !ok {
		// When a player disconnects immediately after a session is started, it
		// might not be added to the players map yet. This is expected, but we
		// need to be careful not to crash when this happens.
		return
	}
	// The player stays online until it is saved: a client logging in as it
	// waits for it to go before the server loads what it saved.
	defer srv.players.remove(c.UUID())

	if tx != nil {
		if err := srv.conf.PlayerProvider.Save(c.UUID(), c.(*player.Player).Data(), tx.World()); err != nil {
			srv.conf.Log.Error("Save player data: " + err.Error())
		}
	} else {
		srv.conf.Log.Error("Save player data: player's worlds closed before teardown; data not saved", "uuid", c.UUID())
	}
}

// createPlayer creates a new player instance using the connection passed,
// as the player of its identity.
func (srv *Server) createPlayer(conn session.Conn, conf player.Config, w *world.World) incoming {
	srv.pwg.Add(1)

	s := session.Config{
		Log:            srv.conf.Log,
		MaxChunkRadius: srv.conf.MaxChunkRadius,
		EmoteChatMuted: srv.conf.MuteEmoteChat,
		JoinMessage:    srv.conf.JoinMessage,
		QuitMessage:    srv.conf.QuitMessage,
		HandleStop:     srv.handleSessionClose,
		BlockRegistry:  w.BlockRegistry(),
		IsOperator:     srv.IsOperator,
	}.New(conn)

	id := session.IdentityOf(conn.IdentityData())
	conf.Name = id.Name
	conf.XUID = id.XUID
	conf.UUID = id.UUID
	conf.Locale, _ = language.Parse(strings.Replace(conn.ClientData().LanguageCode, "_", "-", 1))
	conf.Skin = session.ClientSkin(conn.ClientData())
	conf.Client = s
	conf.Log = srv.conf.Log.With("name", conn.IdentityData().DisplayName, "uuid", conn.IdentityData().Identity, "raddr", conn.RemoteAddr().String())

	handle := world.EntitySpawnOpts{Position: conf.Position, ID: id.UUID}.New(player.Type, conf)
	s.Bind(handle, conf.Skin)
	return incoming{s: s, w: w, conf: conf, p: &onlinePlayer{id: id, handle: handle}}
}

// createWorld loads a world with a specific dimension using the provider set
// in the Config. The nether and end dimensions point to the worlds that players
// are moved to when passing through the respective portals.
func (srv *Server) createWorld(dim world.Dimension) *world.World {
	logger := srv.conf.Log.With("dimension", strings.ToLower(fmt.Sprint(dim)))
	logger.Debug("Loading dimension...")

	gen := srv.conf.Generator(dim, srv.conf.OverworldSeed)
	sourceDim := dim
	conf := world.Config{
		Log:                 logger,
		Dim:                 dim,
		Provider:            srv.conf.WorldProvider,
		Seed:                srv.conf.OverworldSeed,
		Generator:           gen,
		GeneratorProfile:    srv.conf.GeneratorProfile,
		GeneratorWorkers:    srv.conf.GeneratorWorkers,
		GeneratorQueueSize:  srv.conf.GeneratorQueueSize,
		Regions:             srv.conf.Regions,
		RandomTickSpeed:     srv.conf.RandomTickSpeed,
		ReadOnly:            srv.conf.ReadOnlyWorld,
		SaveInterval:        srv.conf.SaveInterval,
		ChunkUnloadInterval: srv.conf.ChunkUnloadInterval,
		Entities:            srv.conf.Entities,
		Blocks:              srv.conf.Blocks,
		PortalDestination: func(target world.Dimension) *world.World {
			resolved := target
			if target == world.Nether && sourceDim == world.Nether {
				resolved = world.Overworld
			}
			if srv.conf.dimensionDisabled(resolved) {
				return nil
			}
			if dest, ok := srv.dimensions[resolved]; ok && dest != nil {
				return dest
			}
			if srv.world != nil && srv.world.Dimension() == resolved {
				return srv.world
			}
			return nil
		},
		PortalDisabledMessage: func(target world.Dimension) string {
			resolved := target
			if target == world.Nether && sourceDim == world.Nether {
				resolved = world.Overworld
			}
			if srv.conf.dimensionDisabled(resolved) {
				return srv.conf.portalDisabledMessage(resolved)
			}
			return ""
		},
		DefaultWorld: func() *world.World {
			return srv.world
		},
	}
	w := conf.New()
	if binder, ok := gen.(interface{ BindWorld(*world.World) }); ok {
		binder.BindWorld(w)
	}
	logger.Info("Opened dimension.", "name", w.Name())
	return w
}

func (srv *Server) registerWorld(dim world.Dimension, w *world.World) {
	if w == nil {
		return
	}
	srv.dimensions[dim] = w
}
