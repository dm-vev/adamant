// Package bedrockdata holds data of Bedrock Edition that the world and the
// Bedrock adapter both read: the vanilla item registry, by which the world
// gives items their runtime IDs and the adapter defines them to the client.
// An embedded file cannot be shared across package directories, so it lives
// here until the item runtime IDs move to the adapter.
package bedrockdata

import _ "embed"

// VanillaItems is the item registry of BDS, as NBT: the runtime ID, whether
// the item is component based, its version and its data, by item name.
// vanilla-reference.json in the repository root records where it was taken
// from.
//
//go:embed vanilla_items.nbt
var VanillaItems []byte
