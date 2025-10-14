package player

import "sync/atomic"

type handlerWrapper func(*Player, Handler) Handler

var playerHandlerWrap atomic.Value

func init() {
	playerHandlerWrap.Store(handlerWrapper(func(_ *Player, h Handler) Handler {
		return h
	}))
}

// SetHandlerWrap installs a function that may wrap handlers assigned through
// Player.Handle. The wrapper runs after the handler has been normalised (nil
// handlers are replaced with NopHandler) and may replace it with an alternate
// implementation.
func SetHandlerWrap(w func(*Player, Handler) Handler) {
	if w == nil {
		playerHandlerWrap.Store(handlerWrapper(func(_ *Player, h Handler) Handler {
			return h
		}))
		return
	}
	playerHandlerWrap.Store(handlerWrapper(w))
}

func wrapPlayerHandler(p *Player, h Handler) Handler {
	return playerHandlerWrap.Load().(handlerWrapper)(p, h)
}
