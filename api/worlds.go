package api

import (
	"errors"
	"fmt"
	"log/slog"
	"sort"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/mcdb"
	"github.com/go-gl/mathgl/mgl64"
)

// WorldSpec configures a dynamically created world managed by App.CreateWorld.
type WorldSpec struct {
	// Name uniquely identifies the world within the App (required).
	Name string
	// Dimension sets the dimension type (defaults to world.Overworld).
	Dimension world.Dimension
	// Generator sets the chunk generator (defaults to world.NopGenerator{}).
	Generator world.Generator
	// Provider overrides the persistence provider. When nil and Directory is
	// non-empty, mcdb.Open(Directory) is used; otherwise world.NopProvider{}.
	Provider world.Provider
	// Directory opens an mcdb world at this path when Provider is nil.
	Directory string
	// Blocks overrides the block registry (defaults to world.DefaultBlockRegistry).
	Blocks world.BlockRegistry
	// Entities overrides the entity registry (defaults to entity.DefaultRegistry).
	Entities world.EntityRegistry
	// ReadOnly prevents saving chunk changes to the provider.
	ReadOnly bool
	// Synchronous runs the world synchronously (useful for tests/tooling).
	Synchronous bool
	// RandomTickSpeed configures random tick speed (0 uses vanilla default, -1 disables).
	RandomTickSpeed int
	// Log overrides the logger used by the world.
	Log *slog.Logger
}

// CreateWorld dynamically creates and registers a new world under spec.Name.
// Dynamically created worlds are automatically closed on App.Disable() or via
// App.UnloadWorld(name).
func (a *App) CreateWorld(spec WorldSpec) (*world.World, error) {
	if a == nil {
		return nil, errors.New("api: nil App")
	}
	if spec.Name == "" {
		return nil, errors.New("api: WorldSpec.Name must not be empty")
	}
	a.mu.Lock()
	if _, exists := a.worlds[spec.Name]; exists {
		a.mu.Unlock()
		return nil, fmt.Errorf("api: world %q already exists", spec.Name)
	}
	a.mu.Unlock()

	prov := spec.Provider
	if prov == nil {
		if spec.Directory != "" {
			db, err := mcdb.Open(spec.Directory)
			if err != nil {
				return nil, fmt.Errorf("api: open mcdb world %q: %w", spec.Name, err)
			}
			prov = db
		} else {
			prov = world.NopProvider{}
		}
	}
	dim := spec.Dimension
	if dim == nil {
		dim = world.Overworld
	}
	gen := spec.Generator
	if gen == nil {
		gen = world.NopGenerator{}
	}
	entities := spec.Entities
	if len(entities.Types()) == 0 {
		entities = entity.DefaultRegistry
	}
	blocks := spec.Blocks
	if blocks == nil {
		blocks = world.DefaultBlockRegistry
	}
	w := world.Config{
		Log:             spec.Log,
		Dim:             dim,
		Provider:        prov,
		Generator:       gen,
		Blocks:          blocks,
		Entities:        entities,
		ReadOnly:        spec.ReadOnly,
		Synchronous:     spec.Synchronous,
		RandomTickSpeed: spec.RandomTickSpeed,
	}.New()

	a.mu.Lock()
	if _, exists := a.worlds[spec.Name]; exists {
		a.mu.Unlock()
		_ = w.Close()
		return nil, fmt.Errorf("api: world %q already exists", spec.Name)
	}
	a.worlds[spec.Name] = managedWorld{w: w, owned: true}
	a.mu.Unlock()
	return w, nil
}

// RegisterWorld registers an externally constructed world under name without
// transferring Close ownership to App.Disable().
func (a *App) RegisterWorld(name string, w *world.World) error {
	if a == nil || name == "" || w == nil {
		return errors.New("api: RegisterWorld requires non-nil App, non-empty name, and non-nil World")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, exists := a.worlds[name]; exists {
		return fmt.Errorf("api: world %q already registered", name)
	}
	a.worlds[name] = managedWorld{w: w, owned: false}
	return nil
}

// WorldByName returns a dynamically created/registered world by name, or one
// of the server's built-in dimensions ("overworld", "nether", "end").
func (a *App) WorldByName(name string) (*world.World, bool) {
	if a == nil || name == "" {
		return nil, false
	}
	a.mu.Lock()
	if entry, ok := a.worlds[name]; ok && entry.w != nil {
		a.mu.Unlock()
		return entry.w, true
	}
	a.mu.Unlock()
	switch name {
	case "overworld":
		w := a.World()
		return w, w != nil
	case "nether":
		w := a.Nether()
		return w, w != nil
	case "end":
		w := a.End()
		return w, w != nil
	default:
		return nil, false
	}
}

// Worlds returns a sorted slice of names for all dynamically managed worlds.
func (a *App) Worlds() []string {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	names := make([]string, 0, len(a.worlds))
	for name := range a.worlds {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// UnloadWorld unregisters name and closes the world if it was created by
// CreateWorld.
func (a *App) UnloadWorld(name string) error {
	if a == nil || name == "" {
		return errors.New("api: UnloadWorld requires non-nil App and non-empty name")
	}
	a.mu.Lock()
	entry, ok := a.worlds[name]
	if !ok {
		a.mu.Unlock()
		return fmt.Errorf("api: world %q not found", name)
	}
	delete(a.worlds, name)
	a.mu.Unlock()
	if entry.owned && entry.w != nil {
		return entry.w.Close()
	}
	return nil
}

// TransferPlayer safely moves p from tx's world to target and teleports them to
// pos using Adamant's checkpoint-safe world.Transfer protocol. If target is the
// player's current world, it simply teleports p to pos in tx. Returns false if
// a checkpoint temporarily holds transfers out of tx's world.
func TransferPlayer(p *player.Player, target *world.World, pos mgl64.Vec3, tx *world.Tx) bool {
	if p == nil || target == nil || tx == nil || tx.Closed() {
		return false
	}
	if tx.World() == target {
		p.Teleport(pos)
		return true
	}
	tr, ok := world.BeginTransfer(tx)
	if !ok {
		return false
	}
	h := tr.Remove(tx, p)
	if h == nil {
		tr.Cancel()
		return false
	}
	target.Exec(func(dst *world.Tx) {
		ent := dst.AddEntity(h)
		if moved, ok := ent.(*player.Player); ok {
			moved.Teleport(pos)
		}
	})
	return true
}
