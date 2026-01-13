package query

import (
	"sync/atomic"
)

// ProviderFunc produces Data for the query responder. The host and port values
// represent the address that the query listener is bound to and should be
// reflected in the returned Data structure.
type ProviderFunc func(host string, port int) Data

var (
	providerPointer atomic.Pointer[ProviderFunc]
)

// RegisterProvider registers the ProviderFunc that supplies query responses.
//
// The most recent provider is used to serve query requests. Passing a nil
// function unregisters the current provider, after which responses will fall
// back to the latest cached snapshot or default values.
func RegisterProvider(fn ProviderFunc) {
	invalidatePayloadCache()
	if fn == nil {
		providerPointer.Store(nil)
		return
	}
	providerPointer.Store(&fn)
}

// loadProvider retrieves the currently registered provider function, if any.
func loadProvider() ProviderFunc {
	ptr := providerPointer.Load()
	if ptr == nil {
		return nil
	}
	return *ptr
}

// engineLabel is the default server identifier reported via query.
//
// Lumi uses the constant `Nukkit.NUKKIT` ("Lumi") as the engine label in both the `server_engine` field and as the
// base of the `plugins` value. Keeping this default aligned makes the query output compatible without requiring the
// caller to fill Data.Engine explicitly.
const engineLabel = "Lumi"
