package world

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
)

// Transfer is an entity on its way out of a world into another, as a
// respawn, a teleport to another world or a portal moves one: removed from
// the first, it is in no world until it is added to one again. A checkpoint
// that captured its worlds then would find it in neither. HoldTransfers
// holds new transfers out of the worlds a checkpoint captures and waits
// until those under way ended, so that every entity is in a world when they
// are captured.
//
// A Transfer ends when the handle of the entity it removed is added to a
// world, whichever it is, or closed: the caller adds it to its destination,
// or back to the world it left if the destination refuses it, as before.
type Transfer struct {
	src     *World
	removed bool
	ended   atomic.Bool
}

// transfers guards the holds and transfers of every world: World.holds is
// the number of HoldTransfers holding the world, and World.transfersOut the
// number of transfers out of it under way. landed, if not nil, is closed
// when a transfer ends, for HoldTransfers to look again.
var transfers struct {
	mu     sync.Mutex
	landed chan struct{}
}

// BeginTransfer starts a transfer of an entity out of the world of tx. It
// reports false while a checkpoint holds transfers out of the world: the
// caller leaves the entity where it is and tries again later, as portal
// travel does at the next contact with the portal. Otherwise the caller
// removes the entity with Remove, in tx, or calls Cancel if it removes none.
func BeginTransfer(tx *Tx) (*Transfer, bool) {
	w := tx.World()
	transfers.mu.Lock()
	defer transfers.mu.Unlock()
	if w.holds > 0 {
		return nil, false
	}
	w.transfersOut++
	return &Transfer{src: w}, true
}

// Remove removes e from the world of tx, in which the transfer began, and
// returns its handle, nil if e was in no world. The transfer ends once the
// handle is added to a world or closed. A nil Transfer removes e as
// Tx.RemoveEntity does.
func (t *Transfer) Remove(tx *Tx, e Entity) *EntityHandle {
	h := tx.RemoveEntity(e)
	if t == nil {
		return h
	}
	t.removed = true
	if h == nil {
		t.end()
		return nil
	}
	if old := h.transfer.Swap(t); old != nil {
		old.end()
	}
	if h.Closed() {
		h.endTransfer()
	}
	return h
}

// Cancel ends a transfer that removed no entity. It does nothing after
// Remove, or on a nil Transfer.
func (t *Transfer) Cancel() {
	if t != nil && !t.removed {
		t.end()
	}
}

// end ends the transfer once.
func (t *Transfer) end() {
	if !t.ended.CompareAndSwap(false, true) {
		return
	}
	transfers.mu.Lock()
	defer transfers.mu.Unlock()
	t.src.transfersOut--
	if transfers.landed != nil {
		close(transfers.landed)
		transfers.landed = nil
	}
}

// endTransfer ends the transfer of the entity of e, if one is under way: e
// was added to a world or closed.
func (e *EntityHandle) endTransfer() {
	if t := e.transfer.Swap(nil); t != nil {
		t.end()
	}
}

// HoldTransfers makes BeginTransfer refuse transfers out of the worlds
// passed and waits until those under way ended, then returns the function
// that lifts the hold. It gives up when ctx ends, lifting the hold. A
// checkpoint holds transfers while it captures its worlds (CaptureAll), so
// that no entity is between two of them. Nothing blocks on the hold: a
// transfer refused is tried again later, and those under way end on the
// owners of their destinations as usual. Called from a world owner or a
// region worker while transfers are under way, it returns ErrOwnerWait,
// lifting the hold: the transfer may need the same owner to end.
func HoldTransfers(ctx context.Context, worlds []*World) (release func(), err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var held []*World
	seen := make(map[*World]bool, len(worlds))
	transfers.mu.Lock()
	for _, w := range worlds {
		if w == nil || seen[w] {
			continue
		}
		seen[w] = true
		held = append(held, w)
		w.holds++
	}
	transfers.mu.Unlock()
	var once sync.Once
	release = func() {
		once.Do(func() {
			transfers.mu.Lock()
			defer transfers.mu.Unlock()
			for _, w := range held {
				w.holds--
			}
		})
	}
	for {
		transfers.mu.Lock()
		under := 0
		for _, w := range held {
			under += w.transfersOut
		}
		if under == 0 {
			transfers.mu.Unlock()
			return release, nil
		}
		if transfers.landed == nil {
			transfers.landed = make(chan struct{})
		}
		landed := transfers.landed
		transfers.mu.Unlock()
		if err := checkOwnerWait("HoldTransfers"); err != nil {
			release()
			return nil, err
		}
		select {
		case <-landed:
		case <-ctx.Done():
			release()
			return nil, fmt.Errorf("world: hold transfers: %d under way: %w", under, ctx.Err())
		}
	}
}
