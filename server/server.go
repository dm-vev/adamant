package server

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"net"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/identity"
	_ "github.com/df-mc/dragonfly/server/item" // Imported for maintaining correct initialisation order.
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/chat"
	"github.com/df-mc/dragonfly/server/text"
	"github.com/df-mc/dragonfly/server/version"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/metrics"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// Server implements a Dragonfly server. It runs the main server loop and
// handles the connections of players trying to join the server.
type Server struct {
	conf Config

	once    sync.Once
	started atomic.Pointer[time.Time]
	// closing is set as the server begins to close, when it is no longer
	// ready for players.
	closing atomic.Bool
	// clock, if not nil, is the clock of the probes of health, for tests.
	clock func() time.Time

	world *world.World
	// dimensions holds the loaded dimensions keyed by their identifiers.
	dimensions       map[world.Dimension]*world.World
	defaultDimension world.Dimension

	whitelist *Whitelist
	operators *operatorsList
	bans      *banList
	// commandLevels are the levels of [Permissions.Commands] by the names
	// of the commands, which Listen resolves.
	commandLevels atomic.Pointer[map[string]cmd.Level]
	// known are the accounts the server has seen join, if it is keyed by
	// XUID.
	known       *knownPlayers
	idleTimeout atomic.Int64
	idleStop    chan struct{}
	listeners   []Listener
	// listening holds, for each listener, whether its accept loop runs.
	listening []atomic.Bool
	incoming  chan incoming

	// metrics is the registry of the metrics of the server, and stats what
	// it counts of itself for them.
	metrics *metrics.Registry
	stats   serverStats

	// players holds the players online.
	players players
	// playersSkipped is set once Players(tx) skipped a player of another
	// world, which it warns of once.
	playersSkipped atomic.Bool
	// checkpoints saves the worlds and the players at one instant, and
	// playersInWorld is set if the player provider keeps the records of
	// players in the world provider's database, which then commits them
	// with the worlds at each checkpoint.
	playersInWorld bool
	// playerSaves orders the saves of each player.
	checkpoints checkpointer
	playerSaves playerSaves
	// backups makes the backups of the server, one at a time.
	backups backups
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
	c    client.Joining
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
	// A problem of the config found only now, as a level set for a command
	// that no one registered, panics before the server starts.
	srv.resolveCommandLevels()
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
	go srv.checkpoints.run(srv.conf.SaveInterval)
	go srv.backups.run()
}

// Accept accepts incoming players into the server, returning an iterator that
// yields players that join the server while blocking otherwise. The iterator
// returned ends when the Server is closed using a call to Close. The loop body
// runs on the player's world owner: blocking there stalls that world, and
// world.Call, world.CallEntity, world.CallRef, Task.Wait and Players(nil) fail
// there with world.ErrOwnerWait. Players returned are only valid within the block of the
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
				inc.c.Spawn(p, tx)
				srv.welcome(p)
				return !yield(p), nil
			})
			if err != nil {
				srv.players.remove(inc.p.handle.UUID())
				srv.pwg.Done()
				// Join failed before spawn: the entity was never added, so close
				// the orphaned handle and fully tear the client down (Disconnect
				// only tells the client; CloseConnection frees the connection;
				// Unlist removes the player from the player list of the others).
				_ = inc.p.handle.Close()
				inc.c.Disconnect(text.Literal("join failed"))
				inc.c.CloseConnection()
				srv.players.roster.Unlist(inc.c)
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

// WhitelistAdd adds a player to the whitelist: by name on a server keyed
// by name; on one keyed by XUID, the account of "xuid:<XUID>" or of the
// player online with the name, or else the name, held until an account
// joins with it.
func (srv *Server) WhitelistAdd(name string) (bool, error) {
	if srv.whitelist == nil {
		return false, ErrWhitelistUnavailable
	}
	en, _, err := srv.addition(name)
	if errors.Is(err, identity.ErrEmptyName) {
		return false, ErrWhitelistInvalidName
	} else if err != nil {
		return false, err
	}
	return srv.whitelist.add(en)
}

// WhitelistRemove removes a player from the whitelist, named as
// WhitelistAdd names it, or by the name the account of an entry last had.
func (srv *Server) WhitelistRemove(name string) (bool, error) {
	if srv.whitelist == nil {
		return false, ErrWhitelistUnavailable
	}
	k, err := srv.removal(Whitelisted, name)
	if errors.Is(err, identity.ErrEmptyName) {
		return false, ErrWhitelistInvalidName
	} else if err != nil {
		return false, err
	}
	return srv.whitelist.remove(k)
}

// WhitelistEntries returns the list of names currently present in the whitelist.
func (srv *Server) WhitelistEntries() ([]string, error) {
	if srv.whitelist == nil {
		return nil, ErrWhitelistUnavailable
	}
	return srv.entryStrings(srv.whitelist.view), nil
}

// OnlinePlayer is a player online, as the server knows it without asking its
// world: the identity its client logged in as and its handle.
type OnlinePlayer struct {
	// Name, XUID and UUID are those the client of the player logged in
	// with. Name is what Player.Name returns.
	Name, XUID string
	UUID       uuid.UUID
	// Handle is the handle of the player's entity: use player.Do with it to
	// act on the player in its world.
	Handle *world.EntityHandle
}

// Online returns the players online now, in the order they joined. It reads
// the server's list only and never waits for a world: call it from anywhere,
// including from inside a transaction, to name or count the players of every
// world.
func (srv *Server) Online() []OnlinePlayer {
	ps := srv.players.snapshot()
	out := make([]OnlinePlayer, len(ps))
	for i, p := range ps {
		out[i] = OnlinePlayer{Name: p.id.Name, XUID: p.id.XUID, UUID: p.id.UUID, Handle: p.handle}
	}
	return out
}

// Players returns an iterator over the players online, in the order they
// joined.
//
// With a transaction, it yields the players of tx's world only, at once and
// in tx: the players of other worlds are not reachable from a transaction
// without waiting for their owners, which may be waiting for this one. Use
// Online for the names and the count of every player, and player.Do with
// their handles to act on them. The first time Players skips a player of
// another world, the server logs a warning naming the caller.
//
// Without one (tx == nil), from off any world owner, it yields every player,
// each in a transaction of its own world, one after another: each loop body
// runs on the yielded player's world owner, so blocking stalls that world, and
// world.Call, world.CallEntity, world.CallRef, and Task.Wait fail there with
// world.ErrOwnerWait. Called with nil from a world owner, it cannot wait for
// the owners of the players and yields nothing.
//
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
	if tx == nil {
		return srv.everyPlayer
	}
	handles := srv.players.handles()
	return func(yield func(*player.Player) bool) {
		for _, handle := range handles {
			e, ok := handle.Entity(tx)
			if !ok {
				srv.warnPlayersSkipped()
				continue
			}
			if !yield(e.(*player.Player)) {
				return
			}
		}
	}
}

// everyPlayer yields every player online, each in a transaction of its own
// world: Players(nil). It waits for the owners of their worlds, one after
// another, and so must not run inside a transaction.
func (srv *Server) everyPlayer(yield func(*player.Player) bool) {
	for _, handle := range srv.players.handles() {
		ret, err := player.Call(context.Background(), handle, func(_ *world.Tx, p *player.Player) (bool, error) {
			return !yield(p), nil
		})
		if err != nil {
			continue
		}
		if ret {
			return
		}
	}
}

// warnPlayersSkipped logs, the first time only, that Players(tx) skipped a
// player of another world than that of tx, naming the code that ranged over
// it: code that relied on Players reaching every world.
func (srv *Server) warnPlayersSkipped() {
	if !srv.playersSkipped.CompareAndSwap(false, true) {
		return
	}
	caller := "unknown"
	// 0 is this function, 1 the iterator of Players and 2 the function
	// ranging over it.
	if pc, file, line, ok := runtime.Caller(2); ok {
		caller = fmt.Sprintf("%s (%s:%d)", runtime.FuncForPC(pc).Name(), file, line)
	}
	srv.conf.Log.Warn("Server.Players(tx) yields the players of the world of tx only: use Server.Online for every player.", "caller", caller)
}

// Player looks for a player on the server with the UUID passed. If found, the
// entity handle is returned and the bool returns holds a true value. If not,
// the bool returned is false and the handle is nil.
func (srv *Server) Player(uuid uuid.UUID) (*world.EntityHandle, bool) {
	return srv.players.handle(uuid)
}

// PlayerByName looks for a player on the server with the name passed, in
// any case and with spaces around, as /kick finds one. If found, the entity
// handle is returned and the bool returned holds a true value. If not, the
// bool is false and the handle is nil
func (srv *Server) PlayerByName(name string) (*world.EntityHandle, bool) {
	if p, ok := srv.players.named(name); ok {
		return p.handle, true
	}
	return nil, false
}

// PlayerByXUID looks for a player on the server with the XUID passed. If
// found, the entity handle is returned and the bool returned is true. If no
// player with the XUID was found, nil and false are returned. On a server
// with Xbox Live sign-in off, the XUID of a player is what its client
// claimed (see Player.XUID): two players may claim one, and the first
// found is returned.
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
	srv.closing.Store(true)
	close(srv.idleStop)
	// A backup running is cancelled, and deletes what it wrote, before the
	// players leave and the last checkpoint is saved.
	srv.backups.close()

	srv.conf.Log.Debug("Disconnecting players...")
	for p := range srv.Players(nil) {
		p.Disconnect(srv.conf.ShutdownMessage.F().Text(p.Locale()))
	}
	srv.pwg.Wait()

	// The players saved as they left: the last checkpoint saves the worlds
	// before they close, and level.dat with them.
	srv.checkpoints.close()
	srv.conf.Log.Debug("Saving checkpoint...")
	_ = srv.checkpoints.checkpoint(context.Background(), "close")

	srv.conf.Log.Debug("Closing player provider...")
	if err := srv.conf.PlayerProvider.Close(); err != nil {
		srv.conf.Log.Error("Close player provider: " + err.Error())
	}
	if err := srv.known.close(); err != nil {
		srv.conf.Log.Error("Write the known players: " + err.Error())
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

// listen makes the Server listen for the joins of clients from the Listener
// passed. This may be used to listen for players on different interfaces.
// Note that the maximum player count of additional Listeners added is not
// enforced automatically. The limit must be enforced by the Listener.
func (srv *Server) listen(i int, l Listener) {
	wg := new(sync.WaitGroup)
	ctx, cancel := context.WithCancel(context.Background())
	for {
		j, err := l.Accept()
		if err != nil {
			srv.listening[i].Store(false)
			// Cancel the context so that any Join.Start is cancelled
			// rapidly.
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
			srv.join(ctx, j)
		}()
	}
}

// startListening starts making the EncodeBlock listener listen, accepting new
// connections from players.
func (srv *Server) startListening() {
	srv.wg.Add(len(srv.listeners))
	for i, l := range srv.listeners {
		srv.listening[i].Store(true)
		go srv.listen(i, l)
	}
}

// wait awaits the closing of all Listeners added to the Server through a call
// to listen and closed the players channel once that happens.
func (srv *Server) wait() {
	srv.wg.Wait()
	srv.conf.Log.Info("Server closed.", "uptime", time.Since(*srv.started.Load()).String())
	close(srv.incoming)
}

// join answers the Join of a client: it holds the join to the reconnect
// policy, loads the player, starts the game of the client and queues the
// player for Accept.
func (srv *Server) join(ctx context.Context, j client.Join) {
	login := j.Login()
	if srv.IdentityKey() == identity.XUID {
		// The Allower of the lists refuses such a login too: a listener
		// that lets one in without it never lets a self-named XUID match an
		// entry.
		k, ok := accountKey(login.Identity)
		if !ok {
			j.Reject(text.Literal(errNoAccount))
			srv.conf.Log.Debug("spawn failed: no Xbox Live account vouched for", "raddr", login.Addr)
			return
		}
		previous := ""
		if srv.known != nil {
			x, _ := k.XUID()
			previous = srv.known.seen(x, login.Identity.Name, login.Identity.UUID)
		}
		srv.followRename(k, login.Identity.Name, previous)
	}
	// A player still online leaves before the joining one is loaded: the
	// server then loads what it saved of the player as it left.
	if handle, ok := srv.online(login.Identity); ok {
		if !srv.handleReconnect(j, login, handle) {
			return
		}
	}

	var (
		fallback  bool
		requested world.Dimension
	)
	d, w, moved, err := srv.loadPlayer(login.Identity, func(dim world.Dimension) *world.World {
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
	if err != nil {
		// A record refused, such as one of another name, chose no world.
		fallback = false
	}
	if moved {
		srv.moveSpawn(login.Identity)
	}
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

	start, cancel := ctx, context.CancelFunc(func() {})
	if srv.conf.SpawnTimeout > 0 {
		start, cancel = context.WithTimeout(ctx, srv.conf.SpawnTimeout)
	}
	c, err := j.Start(start, srv.game(login, d, w))
	timedOut := errors.Is(start.Err(), context.DeadlineExceeded) && ctx.Err() == nil
	cancel()
	if err != nil {
		if timedOut {
			// The client never told it spawned: it holds the slot of a
			// player and its session no longer.
			j.Reject(text.Literal(srv.conf.SpawnTimeoutMessage))
			srv.conf.Log.Debug("spawn failed: timed out", "raddr", login.Addr, "timeout", srv.conf.SpawnTimeout.String())
			return
		}
		j.Reject(text.Literal("Connection timeout."))

		srv.conf.Log.Debug("spawn failed: "+err.Error(), "raddr", login.Addr)
		return
	}
	srv.incoming <- srv.createPlayer(login, c, d, w)
}

// welcome welcomes the player p, which just spawned: it subscribes p to the
// chat and writes the join message of the server, unless it has none, with
// the name of p.
func (srv *Server) welcome(p *player.Player) {
	chat.Global.Subscribe(p)
	if !srv.conf.JoinMessage.Zero() {
		chat.Global.Writet(srv.conf.JoinMessage, p.Name())
	}
}

// game returns the Game a client of login starts in, as the player d in w.
func (srv *Server) game(login client.Login, d player.Config, w *world.World) client.Game {
	return client.Game{
		Name:           srv.conf.Name,
		Level:          srv.world,
		World:          w,
		Position:       d.Position,
		Rotation:       d.Rotation,
		Operator:       srv.operatorOf(login.Identity),
		MaxChunkRadius: srv.conf.MaxChunkRadius,
		IsOperator:     srv.PlayerIsOperator,
		Roster:         &srv.players.roster,
	}
}

// handleReconnect holds the join of a client that logged in as a player
// still online, handle, to the reconnect policy of the server: by its UUID,
// by its name on a server keyed by name, by its account on one keyed by
// XUID. It reports whether the client may join.
func (srv *Server) handleReconnect(j client.Join, login client.Login, handle *world.EntityHandle) bool {
	id := handle.UUID()
	switch srv.conf.ReconnectPolicy {
	case ReconnectKickExisting:
		player.Do(handle, func(_ *world.Tx, p *player.Player) {
			p.Disconnect("Logged in from another location.")
		})
		if srv.waitForPlayerRemoval(id, 5*time.Second) {
			return true
		}
		j.Reject(text.Literal("Already logged in."))
		srv.conf.Log.Debug("spawn failed: already logged in", "raddr", login.Addr)
		return false
	default:
		j.Reject(text.Literal("Already logged in."))
		srv.conf.Log.Debug("spawn failed: already logged in", "raddr", login.Addr)
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

// left handles the player c leaving with its client: the server saves it
// and stops counting it online. It is the Left hook of every player the
// server makes.
func (srv *Server) left(tx *world.Tx, c *player.Player) {
	// Always decrement the wait group because createPlayer unconditionally increments it.
	defer srv.pwg.Done()
	srv.stats.leaves.Inc()

	online, ok := srv.players.player(c.UUID())
	if !ok {
		// When a player disconnects immediately after a session is started, it
		// might not be added to the players map yet. This is expected, but we
		// need to be careful not to crash when this happens.
		return
	}
	// The player stays online until it is saved: a client logging in as it
	// waits for it to go before the server loads what it saved.
	defer srv.players.remove(c.UUID())

	if tx != nil {
		// The snapshot holds inventories of its own: the player's are
		// closed as it is torn down, while the provider may keep the data.
		// It is newer than any a checkpoint captured and may still write.
		p := capturedPlayer{key: srv.dataKey(online.id), seq: srv.playerSaves.next(), data: c.Snapshot()}
		if _, err := srv.playerSaves.save(srv.conf.PlayerProvider, p, tx.World()); err != nil {
			srv.conf.Log.Error("Save player data: " + err.Error())
		}
	} else {
		srv.conf.Log.Error("Save player data: player's worlds closed before teardown; data not saved", "uuid", c.UUID())
	}
}

// createPlayer creates a new player instance, the player of the client c
// that logged in as login says, and binds c to it.
func (srv *Server) createPlayer(login client.Login, c client.Joining, conf player.Config, w *world.World) incoming {
	srv.pwg.Add(1)
	srv.stats.joins.Inc()

	id := login.Identity
	conf.Name = id.Name
	conf.XUID = id.XUID
	conf.UUID = id.UUID
	conf.DataUUID = srv.dataKey(id)
	conf.Locale = login.Locale
	conf.Skin = login.Skin
	conf.Client = c
	conf.Left = srv.left
	conf.QuitMessage = srv.conf.QuitMessage
	conf.AntiCheat, conf.AntiCheatExempt = srv.conf.AntiCheat, srv.conf.AntiCheatExempt
	conf.Log = srv.conf.Log.With("name", id.Name, "uuid", id.UUID.String(), "data-uuid", conf.DataUUID.String(), "raddr", addrString(login.Addr))

	handle := world.EntitySpawnOpts{Position: conf.Position, ID: id.UUID}.New(player.Type, conf)
	c.Bind(handle, conf.Skin)
	return incoming{c: c, w: w, conf: conf, p: &onlinePlayer{id: id, handle: handle}}
}

// addrString returns the address passed as a string, "" if it is nil.
func addrString(addr net.Addr) string {
	if addr == nil {
		return ""
	}
	return addr.String()
}

// createWorld loads a world with a specific dimension using the provider set
// in the Config. The nether and end dimensions point to the worlds that players
// are moved to when passing through the respective portals.
func (srv *Server) createWorld(dim world.Dimension) *world.World {
	logger := srv.conf.Log.With("dimension", dimensionLabel(dim))
	faults, countFault := srv.stats.faultCounter()
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
		SaveChunks:          srv.conf.SaveChunks,
		SaveInterval:        -1, // The checkpoints of the server save the world.
		StagingLimit:        srv.conf.StagingLimit,
		StagingFull:         srv.checkpoints.stagingFull,
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
		Fatal: srv.crash,
		Fault: countFault,
	}
	w := conf.New()
	srv.stats.faultsMu.Lock()
	if srv.stats.faults == nil {
		srv.stats.faults = map[*world.World]*[world.FaultFatal + 1]metrics.Counter{}
	}
	srv.stats.faults[w] = faults
	srv.stats.faultsMu.Unlock()
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
