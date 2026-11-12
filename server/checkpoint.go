package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/df-mc/dragonfly/server/logging"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/mcdb"
	"github.com/google/uuid"
)

// checkpointer saves the server at one instant: every world and the players
// in them, captured at once on the world owners (world.CaptureAll), then
// written off them, the worlds first, then the players, then level.dat. It
// replaces the autosave of the worlds the server makes, which it turns off,
// runs every Config.SaveInterval while saving is on, and serves /save-all
// and the close of the server.
type checkpointer struct {
	srv *Server
	// mu runs one checkpoint at a time; id is the number of the last one.
	// closed is set by the checkpoint of the server closing, after which
	// none is saved.
	mu     sync.Mutex
	id     uint64
	closed bool
	// stop ends the periodic checkpoints, and done is closed once they
	// ended.
	stop, done chan struct{}
	// early is set while a checkpoint asked for by a world whose staged
	// chunks exceed the limit is waiting or running.
	early atomic.Bool
	// lastSuccess is the Unix time in nanoseconds the last checkpoint that
	// wrote everything ended, 0 if none did.
	lastSuccess atomic.Int64
}

// log returns the logger of the checkpoints: the one of the server, scoped
// to the saves.
func (c *checkpointer) log() *slog.Logger { return logging.Sub(c.srv.conf.Log, logging.Save) }

// initSaves readies the checkpoints and the backups of srv. The checkpoints
// number on from the last the world holds, so that the number a backup
// records names one checkpoint across restarts.
func (srv *Server) initSaves() {
	c := &srv.checkpoints
	c.srv, c.stop, c.done = srv, make(chan struct{}), make(chan struct{})
	if lc, ok := srv.conf.WorldProvider.(interface {
		LastCheckpoint() (mcdb.CheckpointRecord, error)
	}); ok {
		if r, err := lc.LastCheckpoint(); err == nil {
			c.id = r.ID
		}
	}
	srv.backups.init(srv)
}

// run saves a checkpoint every interval while saving is on, until stop is
// closed. The interval runs from the end of a checkpoint, so that a long one
// delays the next rather than piling them up. A negative interval saves
// none.
func (c *checkpointer) run(interval time.Duration) {
	defer close(c.done)
	if interval <= 0 {
		<-c.stop
		return
	}
	t := time.NewTimer(interval)
	defer t.Stop()
	for {
		select {
		case <-c.stop:
			return
		case <-t.C:
		}
		if c.srv.SaveEnabledState() {
			_ = c.checkpoint(context.Background(), "autosave")
		}
		t.Reset(interval)
	}
}

// stagingFull saves a checkpoint at once, even with saving off, as the
// chunks w unloaded since the last one take bytes, more than the staging
// limit. It is the StagingFull of the worlds of the server. A checkpoint so
// asked for while another is waiting is not saved twice.
func (c *checkpointer) stagingFull(w *world.World, bytes int) {
	if !c.early.CompareAndSwap(false, true) {
		return
	}
	defer c.early.Store(false)
	select {
	case <-c.stop:
		// Closing the server saves a last checkpoint.
		return
	default:
	}
	c.log().Warn("Saving a checkpoint early: the chunks unloaded since the last exceed the staging limit.", "dimension", fmt.Sprint(w.Dimension()), "bytes", bytes)
	_ = c.checkpoint(context.Background(), "staged-cap")
}

// lastID returns the number of the last checkpoint saved, waiting for one
// running.
func (c *checkpointer) lastID() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.id
}

// close ends the periodic checkpoints, waiting for one running.
func (c *checkpointer) close() {
	close(c.stop)
	<-c.done
}

// checkpoint captures every dimension of the server and the players in them
// at one instant, then writes what it captured and waits until it was:
//
//  1. the worlds, their columns and the data saved beside them;
//  2. the players, so that a record never refers to world state that is not
//     on disk, such as the blocks a player stands on or its bed;
//  3. level.dat, through world.SettingsWriter.
//
// A world provider that is a world.Committer, as mcdb.DB is, commits the
// worlds as one write that lands whole or not at all, with the players if
// the player provider keeps them in the same database
// (playerdb.WorldProvider): a crash then leaves exactly the last checkpoint
// committed, and level.dat, written after, is restored from it if it is
// older. A failed commit leaves the worlds to save their columns and data
// again at the next checkpoint, and level.dat as it was.
//
// No entity is between two worlds while they are captured: transfers out of
// them are held, and those under way land first (world.HoldTransfers). If
// the worlds cannot be captured within world.CaptureTimeout, nothing is
// written and the error is returned. reason is logged. It waits for the
// world owners: call it off any of them.
func (c *checkpointer) checkpoint(ctx context.Context, reason string) (err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = reason == "close"
	srv := c.srv
	start := time.Now()
	stats, reasonAt := &srv.stats, reasonIndex(reason)
	defer func() {
		stats.checkpoints[reasonAt].Inc()
		if err != nil {
			stats.checkpointFailures[reasonAt].Inc()
			return
		}
		c.lastSuccess.Store(time.Now().UnixNano())
	}()
	ctx, cancel := context.WithTimeout(ctx, world.CaptureTimeout)
	defer cancel()
	worlds := srv.distinctWorlds()
	// A provider that commits a checkpoint at once (mcdb) takes the writes
	// of the worlds, and of the players if it keeps them, in one commit.
	var commit world.Commit
	if committer, ok := srv.conf.WorldProvider.(world.Committer); ok && !srv.conf.ReadOnlyWorld {
		commit = committer.BeginCommit()
	}
	release, err := world.HoldTransfers(ctx, worlds)
	if err != nil {
		if commit != nil {
			commit.Discard()
		}
		c.log().Error("Save checkpoint: "+err.Error(), "reason", reason)
		return err
	}
	cps, err := world.CaptureAllInto(ctx, worlds, commit, srv.capturePlayers)
	release()
	if len(cps) == 0 && err != nil {
		if commit != nil {
			commit.Discard()
		}
		c.log().Error("Save checkpoint: "+err.Error(), "reason", reason)
		return err
	}
	c.id++
	captured := time.Now()
	stats.capture.Observe(captured.Sub(start).Seconds())
	errs := []error{err}

	chunks, settings, paused, werr := writeWorlds(cps)
	errs = append(errs, werr)
	players := 0
	savePlayers := func() {
		for _, cp := range cps {
			for _, p := range cp.Extra.([]capturedPlayer) {
				saved, err := srv.playerSaves.save(srv.conf.PlayerProvider, p, cp.World())
				if err != nil {
					c.log().Error("Save player data: "+err.Error(), "key", p.key)
					errs = append(errs, err)
				} else if saved {
					players++
				}
			}
		}
	}
	// The players go into the commit if their provider keeps them in the
	// world database, and are saved after it otherwise.
	inCommit := commit != nil && srv.playersInWorld
	if inCommit {
		savePlayers()
	}
	var commitErr error
	if commit != nil {
		if commitErr = commit.Commit(c.id); commitErr != nil {
			c.log().Error("Save checkpoint: "+commitErr.Error(), "reason", reason)
			errs = append(errs, commitErr)
		}
		for _, cp := range cps {
			cp.Committed(commitErr)
		}
	}
	if !inCommit {
		savePlayers()
	}
	if w, ok := srv.conf.WorldProvider.(world.SettingsWriter); ok && settings && commitErr == nil {
		if err := w.WriteSettings(); err != nil {
			c.log().Error("Save checkpoint: " + err.Error())
			errs = append(errs, err)
		}
	}
	online := map[uuid.UUID]bool{}
	for _, p := range srv.players.snapshot() {
		online[srv.dataKey(p.id)] = true
	}
	srv.playerSaves.forget(online)
	stats.write.Observe(time.Since(captured).Seconds())
	stats.lastChunks.Store(int64(chunks))
	stats.lastPlayers.Store(int64(players))
	c.log().Debug("Saved checkpoint.", "checkpoint", c.id, "reason", reason, "chunks", chunks, "players", players, "captured", captured.Sub(start), "paused", paused, "written", time.Since(captured))
	return errors.Join(errs...)
}

// writeWorlds writes the worlds captured by a checkpoint and returns the
// columns they captured, whether any captured its settings, the longest a
// capture held a world and the errors of the writes joined.
func writeWorlds(cps []*world.Checkpoint) (chunks int, settings bool, paused time.Duration, err error) {
	var errs []error
	for _, cp := range cps {
		chunks += cp.Columns()
		paused = max(paused, cp.CaptureTime())
		settings = settings || cp.Settings() != nil
		errs = append(errs, cp.Write())
	}
	return chunks, settings, paused, errors.Join(errs...)
}

// distinctWorlds returns the dimensions of the server, each world once.
func (srv *Server) distinctWorlds() []*world.World {
	var worlds []*world.World
	seen := map[*world.World]bool{}
	for _, dim := range []world.Dimension{srv.defaultDimension, world.Overworld, world.Nether, world.End} {
		if w := srv.dimensions[dim]; w != nil && !seen[w] {
			seen[w] = true
			worlds = append(worlds, w)
		}
	}
	return worlds
}

// capturedPlayer is a player captured by a checkpoint: the key the server
// keeps its record by (Server.dataKey), its data, detached from it, and the
// number of the snapshot.
type capturedPlayer struct {
	key  uuid.UUID
	seq  uint64
	data player.Config
}

// capturePlayers captures the players of the server in the world of tx: a
// snapshot of each that is online, which is what srv.left saves when it
// quits. It is the extra function of the checkpoints' CaptureAll.
func (srv *Server) capturePlayers(tx *world.Tx) any {
	var captured []capturedPlayer
	for e := range tx.Players() {
		p, ok := e.(*player.Player)
		if !ok {
			continue
		}
		online, ok := srv.players.player(p.UUID())
		if !ok || online.handle != p.H() {
			// Not a player of the server, such as one a plugin made.
			continue
		}
		captured = append(captured, capturedPlayer{key: srv.dataKey(online.id), seq: srv.playerSaves.next(), data: p.Snapshot()})
	}
	return captured
}

// playerSaves orders the saves of the players, so that the record of a
// player is never replaced with an older one. It keeps them by the key of
// the record. A checkpoint writes the
// players it captured after it captured them, off the owners, while a
// player quitting meanwhile is saved from a newer snapshot by srv.left. Each
// snapshot takes a number when it is taken, in the transaction of the
// player's world, and a save of a snapshot older than the last one saved of
// the player is skipped.
type playerSaves struct {
	seq atomic.Uint64

	mu   sync.Mutex
	last map[uuid.UUID]*playerSave
}

// playerSave is the last save of a player: written is the number of its
// snapshot, under mu, which a save holds while it writes.
type playerSave struct {
	mu      sync.Mutex
	written uint64
}

// next numbers a snapshot of a player about to be taken.
func (s *playerSaves) next() uint64 { return s.seq.Add(1) }

// save saves the player captured through provider, with w, the world it is
// in, unless a newer snapshot of it was saved. It reports whether it saved.
func (s *playerSaves) save(provider player.Provider, p capturedPlayer, w *world.World) (bool, error) {
	s.mu.Lock()
	if s.last == nil {
		s.last = make(map[uuid.UUID]*playerSave)
	}
	last := s.last[p.key]
	if last == nil {
		last = &playerSave{}
		s.last[p.key] = last
	}
	s.mu.Unlock()

	last.mu.Lock()
	defer last.mu.Unlock()
	if p.seq <= last.written {
		return false, nil
	}
	if err := provider.Save(p.key, p.data, w); err != nil {
		return false, err
	}
	last.written = p.seq
	return true, nil
}

// forget forgets the saves of the players no longer online, of the keys
// online holds. A checkpoint calls it once it wrote its players: no
// snapshot of a player who left is waiting to be written then, as
// checkpoints capture only players online.
func (s *playerSaves) forget(online map[uuid.UUID]bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key := range s.last {
		if !online[key] {
			delete(s.last, key)
		}
	}
}
