package server

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
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
	mu sync.Mutex
	id uint64
	// stop ends the periodic checkpoints, and done is closed once they
	// ended.
	stop, done chan struct{}
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
// If the worlds cannot be captured within world.CaptureTimeout, nothing is
// written and the error is returned. reason is logged. It waits for the
// world owners: call it off any of them.
func (c *checkpointer) checkpoint(ctx context.Context, reason string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	srv := c.srv
	start := time.Now()
	cps, err := world.CaptureAll(ctx, srv.distinctWorlds(), srv.capturePlayers)
	if len(cps) == 0 && err != nil {
		srv.conf.Log.Error("Save checkpoint: "+err.Error(), "reason", reason)
		return err
	}
	c.id++
	captured := time.Now()
	errs := []error{err}

	chunks, settings := 0, false
	for _, cp := range cps {
		chunks += cp.Columns()
		settings = settings || cp.Settings() != nil
		errs = append(errs, cp.Write())
	}
	players := 0
	for _, cp := range cps {
		for _, p := range cp.Extra.([]capturedPlayer) {
			saved, err := srv.playerSaves.save(srv.conf.PlayerProvider, p, cp.World())
			if err != nil {
				srv.conf.Log.Error("Save player data: "+err.Error(), "key", p.key)
				errs = append(errs, err)
			} else if saved {
				players++
			}
		}
	}
	if w, ok := srv.conf.WorldProvider.(world.SettingsWriter); ok && settings {
		if err := w.WriteSettings(); err != nil {
			srv.conf.Log.Error("Save checkpoint: " + err.Error())
			errs = append(errs, err)
		}
	}
	online := map[uuid.UUID]bool{}
	for _, p := range srv.players.snapshot() {
		online[srv.dataKey(p.id)] = true
	}
	srv.playerSaves.forget(online)
	srv.conf.Log.Debug("Saved checkpoint.", "checkpoint", c.id, "reason", reason, "chunks", chunks, "players", players, "captured", captured.Sub(start), "written", time.Since(captured))
	return errors.Join(errs...)
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
