package cmd

import (
	"sync"

	"github.com/dm-vev/adamant/server/world"
)

// Deferrer is implemented by a Source that waits for the output a command
// sends after it returned, as /save-all sends once the world was saved: a
// remote client, which answers its request with the output of the command,
// waits for it before it answers. A command that sends output later calls
// Defer before it returns, and the done function Defer returns once the
// later output was sent. A Source that is not a Deferrer is sent the later
// output as it comes, as the console is.
type Deferrer interface {
	// Defer tells the Source that output will follow the command, and
	// returns the function to call once it was sent.
	Defer() (done func())
}

// Later returns a function that sends output to src from a later transaction,
// or from none, once the transaction of the command ended, to be called once.
// A source that is an entity, as a player, is valid only in the transaction of
// the command: it is found again by its handle, wherever it is then. A source
// that is a Deferrer is told output follows, and that it was sent once it was
// (including when called with a nil Output to signal cancellation).
func Later(src Source) func(tx *world.Tx, o *Output) {
	base := src
	for {
		w, ok := base.(Wrapper)
		if !ok {
			break
		}
		base = w.Unwrap()
	}
	if d, ok := base.(Deferrer); ok {
		done := d.Defer()
		var once sync.Once
		return func(_ *world.Tx, o *Output) {
			once.Do(func() {
				defer done()
				if o != nil {
					base.SendCommandOutput(o)
				}
			})
		}
	}
	e, ok := base.(world.Entity)
	if !ok {
		return func(_ *world.Tx, o *Output) {
			if o != nil {
				base.SendCommandOutput(o)
			}
		}
	}
	h := e.H()
	sendTo := func(e world.Entity, o *Output) {
		if s, ok := e.(Source); ok {
			s.SendCommandOutput(o)
		}
	}
	return func(tx *world.Tx, o *Output) {
		if o == nil {
			return
		}
		if e, ok := handleEntityInTx(h, tx); ok {
			sendTo(e, o)
			return
		}
		h.Do(func(_ *world.Tx, e world.Entity) { sendTo(e, o) })
	}
}
