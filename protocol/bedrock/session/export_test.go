package session

import (
	"maps"
	"slices"

	"github.com/df-mc/dragonfly/protocol/bedrock/internal/versions"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/menu"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/go-gl/mathgl/mgl64"
)

// HandlePacket handles a packet as the session does once it read one, for
// the tests of other packages, without the recovery of the goroutine that
// reads: a panic of a handler reaches the test.
func (s *Session) HandlePacket(pk proto.Packet, tx *world.Tx, c Controllable) error {
	return s.handlePacket(pk, tx, c)
}

// Inventories returns the inventories of the player of the session, as the
// session holds them.
func (s *Session) Inventories() (inv, offHand, enderChest, ui *inventory.Inventory, armour *inventory.Armour) {
	return s.inv, s.offHand, s.enderChest, s.ui, s.armour
}

// Window returns the menu the session shows open, and the ID and type of
// its window.
func (s *Session) Window() (m *menu.Menu, id, containerType uint32) {
	return s.menu.Load(), s.openedWindowID.Load(), s.openedContainerID.Load()
}

// EncodeSubChunk and EncodeBiomes encode the parts of a chunk as the session
// sends them to the clients of the default protocol, for the tests of the
// encoding.
func EncodeSubChunk(c *chunk.Chunk, ind int) []byte {
	return encodeSubChunk(c, ind, blockTable(oldProtocol(), c.BlockRegistry()))
}
func EncodeBiomes(c *chunk.Chunk) []byte { return encodeBiomes(c) }

// StatesOfDefault returns a runtime ID of each state of a finalized
// registry the clients of the default protocol know, the one they read its
// hash back as, sorted by its hash: the same states whatever the protocol
// whose blocks the game holds, as the scenes of the tests of the encoding
// take them.
func StatesOfDefault(br world.BlockRegistry) []uint32 {
	h := blockTable(oldProtocol(), br)
	hashes := slices.Sorted(maps.Keys(h.rid))
	rids := make([]uint32, len(hashes))
	for i, hash := range hashes {
		rids[i] = h.rid[hash]
	}
	return rids
}

// NetworkItemNBT returns the item NBT ItemStack sends for a stack, for the
// tests that build the stacks clients send.
func NetworkItemNBT(s item.Stack) map[string]any { return networkItemNBT(s) }

// SoundPackets returns the packets a session on the default block registry
// hands out to play a sound at a position, for the tests of the mapping of
// sounds.
func SoundPackets(pos mgl64.Vec3, t world.Sound) []proto.Packet {
	s := &Session{br: world.DefaultBlockRegistry}
	var pks []proto.Packet
	s.soundPackets(pos, t, false, func(pk proto.Packet) { pks = append(pks, pk) })
	return pks
}

// oldProtocol returns protocol 2193, the oldest the adapter speaks and the
// primary the goldens of testdata/golden were recorded with.
func oldProtocol() *versions.Protocol {
	p, _ := versions.ByNumber(2193)
	return p
}
