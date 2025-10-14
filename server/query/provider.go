package query

import (
	"runtime/debug"
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

// engineLabel constructs the engine identifier that is shown by query clients.
var engineLabel = buildEngineLabel()

// buildEngineLabel inspects build metadata to determine the engine label that
// is reported through the query interface. The build information is optional,
// so sane defaults are supplied when it cannot be determined.
func buildEngineLabel() string {
	info, ok := debug.ReadBuildInfo()
	if !ok || info == nil {
		return "Adamant"
	}
	version := info.Main.Version
	if version == "" {
		version = "dev"
	}
	return "Adamant (" + version + ")"
}
