package session

import (
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

// UseSessionRequests has the session handle item stack requests through
// its own handlers, as before they moved into server/menu, which
// TestGoldenMenuRequests compares with the batches of the menu.
func UseSessionRequests(s *Session) { s.sessionRequests = true }

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
// sends them, for the tests of the encoding.
func EncodeSubChunk(c *chunk.Chunk, ind int) []byte { return encodeSubChunk(c, ind) }
func EncodeBiomes(c *chunk.Chunk) []byte            { return encodeBiomes(c) }

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
