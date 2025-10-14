package world

import "sync/atomic"

type handlerWrapper func(*World, Handler) Handler

var worldHandlerWrap atomic.Value

func init() {
	worldHandlerWrap.Store(handlerWrapper(func(_ *World, h Handler) Handler {
		return h
	}))
}

// SetHandlerWrap installs a wrapper applied to handlers assigned through
// World.Handle. The wrapper is invoked after nil handlers are normalised to
// NopHandler.
func SetHandlerWrap(w func(*World, Handler) Handler) {
	if w == nil {
		worldHandlerWrap.Store(handlerWrapper(func(_ *World, h Handler) Handler {
			return h
		}))
		return
	}
	worldHandlerWrap.Store(handlerWrapper(w))
}

func wrapWorldHandler(w *World, h Handler) Handler {
	return worldHandlerWrap.Load().(handlerWrapper)(w, h)
}
