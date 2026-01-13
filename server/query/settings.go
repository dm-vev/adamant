package query

import "sync/atomic"

var (
	queryEnabled         atomic.Bool
	pluginListingEnabled atomic.Bool
)

func init() {
	// Lumi enables query by default.
	queryEnabled.Store(true)
	// Lumi does not list plugins by default.
	pluginListingEnabled.Store(false)
}

// SetEnabled controls whether query packets are answered.
//
// When disabled, query packets are silently ignored (no response is sent).
func SetEnabled(enabled bool) {
	queryEnabled.Store(enabled)
	invalidatePayloadCache()
}

// Enabled reports whether query packets are currently answered.
func Enabled() bool {
	return queryEnabled.Load()
}

// SetPluginListingEnabled controls whether plugin metadata is included in long query responses.
//
// When disabled, the `plugins` field contains only the engine label (matching Lumi's default behaviour).
func SetPluginListingEnabled(enabled bool) {
	pluginListingEnabled.Store(enabled)
	invalidatePayloadCache()
}

// PluginListingEnabled reports whether plugin metadata should be included in query responses.
func PluginListingEnabled() bool {
	return pluginListingEnabled.Load()
}
