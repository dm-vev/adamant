// Package bedrockdata holds data of Bedrock Edition the Bedrock adapter reads:
// the vanilla item registry, by which the adapter defines the items to the
// client, and the runtime IDs it gives items from it.
package bedrockdata

import _ "embed"

// VanillaItems is the item registry of BDS, as NBT: the runtime ID, whether
// the item is component based, its version and its data, by item name.
// vanilla-reference.json in the repository root records where it was taken
// from.
//
//go:embed vanilla_items.nbt
var VanillaItems []byte
