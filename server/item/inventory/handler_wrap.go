package inventory

import "sync/atomic"

type handlerWrapper func(*Inventory, Handler) Handler

var inventoryHandlerWrap atomic.Value

func init() {
	inventoryHandlerWrap.Store(handlerWrapper(func(_ *Inventory, h Handler) Handler {
		return h
	}))
}

// SetHandlerWrap installs a function that wraps handlers assigned through
// Inventory.Handle. Wrappers run after nil handlers are substituted with
// NopHandler.
func SetHandlerWrap(w func(*Inventory, Handler) Handler) {
	if w == nil {
		inventoryHandlerWrap.Store(handlerWrapper(func(_ *Inventory, h Handler) Handler {
			return h
		}))
		return
	}
	inventoryHandlerWrap.Store(handlerWrapper(w))
}

func wrapInventoryHandler(inv *Inventory, h Handler) Handler {
	return inventoryHandlerWrap.Load().(handlerWrapper)(inv, h)
}
