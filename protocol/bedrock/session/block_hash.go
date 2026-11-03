package session

import (
	"encoding/binary"
	"fmt"
	"maps"
	"sort"
	"sync"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/segmentio/fasthash/fnv1a"
)

// blockHashes are the network block hashes of the runtime IDs of a finalized
// registry, as BDS computes them from the name and properties of a state,
// and their inverse. Clients know blocks by these hashes once block network
// ID hashes are enabled, whatever the palette of the server.
type blockHashes struct {
	hash []uint32          // by runtime ID
	rid  map[uint32]uint32 // by hash
}

// runtimeIDToHash returns the network block hash of a runtime ID, and false
// if the registry has no such runtime ID.
func (h *blockHashes) runtimeIDToHash(rid uint32) (uint32, bool) {
	if rid >= uint32(len(h.hash)) {
		return 0, false
	}
	return h.hash[rid], true
}

// blockID returns the network block hash of a block of the registry br the
// hashes are of, 0 if it has none, as clients take a block they do not know.
func (h *blockHashes) blockID(br world.BlockRegistry, b world.Block) uint32 {
	hash, _ := h.runtimeIDToHash(br.BlockRuntimeID(b))
	return hash
}

// hashToRuntimeID returns the runtime ID of a network block hash, and false
// if no state of the registry has it.
func (h *blockHashes) hashToRuntimeID(hash uint32) (uint32, bool) {
	rid, ok := h.rid[hash]
	return rid, ok
}

// registryHashes holds the blockHashes of each registry hashesOf was passed,
// by registry.
var registryHashes sync.Map

// hashesOf returns the network block hashes of a finalized registry, built
// the first time it is passed. It panics if two states of the registry have
// the same hash, as clients could not tell them apart.
func hashesOf(br chunk.BlockRegistry) *blockHashes {
	if h, ok := registryHashes.Load(br); ok {
		return h.(*blockHashes)
	}
	h, _ := registryHashes.LoadOrStore(br, buildBlockHashes(br))
	return h.(*blockHashes)
}

// buildBlockHashes hashes every runtime ID of a finalized registry as the
// registry's Finalize did: the hash of the state, which the inverse maps
// back to the runtime ID, replaced by the hash of the state BDS sends for
// it where the two differ.
func buildBlockHashes(br chunk.BlockRegistry) *blockHashes {
	n := br.BlockCount()
	h := &blockHashes{hash: make([]uint32, n), rid: make(map[uint32]uint32, n)}
	scratch := make([]byte, 0, 0xff)
	for i := range n {
		rid := uint32(i)
		name, properties, _ := br.RuntimeIDToState(rid)
		var netHash uint32
		netHash, scratch = networkBlockHash(name, properties, scratch)
		if other, ok := h.rid[netHash]; ok {
			otherName, otherProperties, _ := br.RuntimeIDToState(other)
			panic(fmt.Sprintf("network block hash collision for (%s %+v) and (%s %+v)", name, properties, otherName, otherProperties))
		}
		h.rid[netHash] = rid
		h.hash[rid] = netHash
		if client, ok := clientDoorProperties(properties); ok {
			h.hash[rid], scratch = networkBlockHash(name, client, scratch)
		}
	}
	return h
}

// clientDoorProperties returns the state BDS sends for the upper half of an
// open door. Only the lower half carries open_bit on Bedrock, while the
// server keeps Java's open state on both halves, so the upper half is sent
// closed.
func clientDoorProperties(properties map[string]any) (map[string]any, bool) {
	if _, door := properties["door_hinge_bit"]; !door {
		return nil, false
	}
	upper, open := properties["upper_block_bit"], properties["open_bit"]
	if (upper != true && upper != uint8(1)) || (open != true && open != uint8(1)) {
		return nil, false
	}
	client := maps.Clone(properties)
	if open == true {
		client["open_bit"] = false
	} else {
		client["open_bit"] = uint8(0)
	}
	return client, true
}

// networkBlockHash produces the canonical "network block hash" for a (name, properties) block state.
// This hash is used for mapping network palette entries back to runtime IDs.
//
// The scratch slice is used to reduce allocations. The returned slice should be passed back in on subsequent calls to
// reuse the same backing array.
func networkBlockHash(name string, properties map[string]any, scratch []byte) (uint32, []byte) {
	if name == "minecraft:unknown" {
		return 0xfffffffe, scratch // -2
	}

	keys := make([]string, 0, len(properties))
	for k := range properties {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	data := scratch[:0]
	writeString := func(str string) {
		data = binary.LittleEndian.AppendUint16(data, uint16(len(str)))
		data = append(data, []byte(str)...)
	}

	data = append(data, 10) // compound
	data = append(data, 0)
	data = append(data, 0)

	data = append(data, 8) // string
	writeString("name")
	writeString(name)

	data = append(data, 10) // compound
	writeString("states")
	for _, k := range keys {
		v := properties[k]
		switch v := v.(type) {
		case string:
			data = append(data, 8) // string
			writeString(k)
			writeString(v)

		case uint8:
			data = append(data, 1) // tagByte
			writeString(k)
			data = append(data, byte(v))
		case int8:
			data = append(data, 1) // tagByte
			writeString(k)
			data = append(data, byte(v))
		case bool:
			b := 0
			if v {
				b = 1
			}
			data = append(data, 1) // tagByte
			writeString(k)
			data = append(data, byte(b))

		case uint16:
			data = append(data, 2) // tagInt16
			writeString(k)
			data = binary.LittleEndian.AppendUint16(data, uint16(v))
		case int16:
			data = append(data, 2) // tagInt16
			writeString(k)
			data = binary.LittleEndian.AppendUint16(data, uint16(v))

		case uint32:
			data = append(data, 3) // tagInt32
			writeString(k)
			data = binary.LittleEndian.AppendUint32(data, uint32(v))
		case int32:
			data = append(data, 3) // tagInt32
			writeString(k)
			data = binary.LittleEndian.AppendUint32(data, uint32(v))
		default:
			panic(fmt.Sprintf("unhandled nbt type: %T", v))
		}
	}
	data = append(data, 0) // end
	data = append(data, 0) // end

	return fnv1a.HashBytes32(data), data
}
