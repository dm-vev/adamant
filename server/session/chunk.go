package session

import (
	"bytes"
	"sync"

	"github.com/cespare/xxhash/v2"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/sandertv/gophertunnel/minecraft/nbt"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// subChunkRequests is set to true to enable the sub-chunk request system. This can (likely) cause unexpected issues,
// but also solves issues with block entities such as item frames and lecterns as of v1.19.10.
const subChunkRequests = true

const (
	maxPendingBlobs         = 4096
	maxSubChunkOffsets      = 4096
	maxPooledChunkEncodeCap = 64 << 10
)

var chunkEncodeBufferPool = sync.Pool{
	New: func() any {
		return bytes.NewBuffer(make([]byte, 0, 512))
	},
}

// ViewChunk ...
func (s *Session) ViewChunk(pos world.ChunkPos, dim world.Dimension, col *world.Column) {
	if !s.conn.ClientCacheEnabled() {
		s.sendNetworkChunk(pos, dim, col)
		return
	}
	s.sendBlobHashes(pos, dim, col)
}

// ViewSubChunks ...
func (s *Session) ViewSubChunks(center world.SubChunkPos, offsets []protocol.SubChunkOffset, tx *world.Tx) {
	if s.chunkLoader == nil {
		// The chunk loader is initialised during Spawn, so return an empty response for early requests.
		dim, _ := world.DimensionID(tx.World().Dimension())
		s.writePacket(&packet.SubChunk{
			Dimension:       int32(dim),
			Position:        protocol.SubChunkPos(center),
			CacheEnabled:    s.conn.ClientCacheEnabled(),
			SubChunkEntries: nil,
		})
		return
	}
	if len(offsets) > maxSubChunkOffsets {
		// Cap offsets to bound memory usage when handling malformed or abusive requests.
		offsets = offsets[:maxSubChunkOffsets]
	}
	r := tx.Range()

	entries := make([]protocol.SubChunkEntry, 0, len(offsets))
	transaction := make(map[uint64]struct{})
	for _, offset := range offsets {
		ind := int16(center.Y()) + int16(offset[1]) - int16(r[0]>>4)
		if ind < 0 || ind > int16(r.Height()>>4) {
			entries = append(entries, protocol.SubChunkEntry{Result: protocol.SubChunkResultIndexOutOfBounds, Offset: offset})
			continue
		}
		col, ok := s.chunkLoader.Chunk(world.ChunkPos{
			center.X() + int32(offset[0]),
			center.Z() + int32(offset[2]),
		})
		if !ok {
			entries = append(entries, protocol.SubChunkEntry{Result: protocol.SubChunkResultChunkNotFound, Offset: offset})
			continue
		}
		entries = append(entries, s.subChunkEntry(offset, ind, col, transaction))
	}
	if s.conn.ClientCacheEnabled() && len(transaction) > 0 {
		s.blobMu.Lock()
		s.openChunkTransactions = append(s.openChunkTransactions, transaction)
		s.blobMu.Unlock()
	}
	dim, _ := world.DimensionID(tx.World().Dimension())
	s.writePacket(&packet.SubChunk{
		Dimension:       int32(dim),
		Position:        protocol.SubChunkPos(center),
		CacheEnabled:    s.conn.ClientCacheEnabled(),
		SubChunkEntries: entries,
	})
}

func (s *Session) subChunkEntry(offset protocol.SubChunkOffset, ind int16, col *world.Column, transaction map[uint64]struct{}) protocol.SubChunkEntry {
	subMapType, subMap := subChunkHeightMap(col, ind)

	sub := col.Chunk.Sub()[ind]
	if sub.Empty() {
		return protocol.SubChunkEntry{
			Result:              protocol.SubChunkResultSuccessAllAir,
			HeightMapType:       subMapType,
			HeightMapData:       subMap,
			RenderHeightMapType: subMapType,
			RenderHeightMapData: subMap,
			Offset:              offset,
		}
	}

	serialisedSubChunk := networkSubChunkPayload(col, ind)
	blockEntityPayload := subChunkBlockEntityPayload(col, ind)

	entry := protocol.SubChunkEntry{
		Result:              protocol.SubChunkResultSuccess,
		RawPayload:          joinPayloads(serialisedSubChunk, blockEntityPayload),
		HeightMapType:       subMapType,
		HeightMapData:       subMap,
		RenderHeightMapType: subMapType,
		RenderHeightMapData: subMap,
		Offset:              offset,
	}
	if s.conn.ClientCacheEnabled() {
		if hash := xxhash.Sum64(serialisedSubChunk); s.trackBlob(hash, serialisedSubChunk) {
			transaction[hash] = struct{}{}

			entry.BlobHash = hash
			entry.RawPayload = blockEntityPayload
		}
	}
	return entry
}

func subChunkHeightMap(col *world.Column, ind int16) (byte, []int8) {
	if mapType, mapData, ok := col.CachedSubChunkHeightMap(ind); ok {
		return mapType, mapData
	}

	chunkMap := col.Chunk.HeightMap()
	subMapType, subMap := byte(protocol.HeightMapDataHasData), make([]int8, 256)
	higher, lower := true, true
	for x := uint8(0); x < 16; x++ {
		for z := uint8(0); z < 16; z++ {
			y, i := chunkMap.At(x, z), (uint16(z)<<4)|uint16(x)
			otherInd := col.Chunk.SubIndex(y)
			if otherInd > ind {
				subMap[i], lower = 16, false
				continue
			}
			if otherInd < ind {
				subMap[i], higher = -1, false
				continue
			}
			subMap[i], lower, higher = int8(y-col.Chunk.SubY(otherInd)), false, false
		}
	}
	if higher {
		subMapType, subMap = protocol.HeightMapDataTooHigh, nil
	} else if lower {
		subMapType, subMap = protocol.HeightMapDataTooLow, nil
	}

	col.CacheSubChunkHeightMap(ind, subMapType, subMap)
	return subMapType, subMap
}

// dimensionID returns the dimension ID of the world that the session is in.
func (s *Session) dimensionID(dim world.Dimension) int32 {
	d, _ := world.DimensionID(dim)
	return int32(d)
}

// sendBlobHashes sends chunk blob hashes of the data of the chunk and stores the data in a map of blobs. Only
// data that the client doesn't yet have will be sent over the network.
func (s *Session) sendBlobHashes(pos world.ChunkPos, dim world.Dimension, col *world.Column) {
	c := col.Chunk
	if subChunkRequests {
		biomes := networkBiomePayload(col)
		if hash := xxhash.Sum64(biomes); s.trackBlob(hash, biomes) {
			s.writePacket(&packet.LevelChunk{
				Dimension:       s.dimensionID(dim),
				SubChunkCount:   protocol.SubChunkRequestModeLimited,
				Position:        protocol.ChunkPos(pos),
				HighestSubChunk: c.HighestFilledSubChunk(),
				BlobHashes:      []uint64{hash},
				RawPayload:      []byte{0},
				CacheEnabled:    true,
			})
			return
		}
		return
	}

	var (
		data   = networkChunkData(col)
		count  = uint32(len(data.SubChunks))
		blobs  = append(data.SubChunks, data.Biomes)
		hashes = make([]uint64, len(blobs))
		m      = make(map[uint64]struct{}, len(blobs))
	)
	for i, blob := range blobs {
		h := xxhash.Sum64(blob)
		hashes[i], m[h] = h, struct{}{}
	}

	s.blobMu.Lock()
	pending := len(s.blobs)
	if pending+len(hashes) > maxPendingBlobs {
		s.blobMu.Unlock()
		s.conf.Log.Error("too many blobs pending", "n", pending)
		s.CloseConnection()
		return
	}
	s.openChunkTransactions = append(s.openChunkTransactions, m)
	for i := range hashes {
		s.blobs[hashes[i]] = blobs[i]
	}
	s.blobMu.Unlock()

	raw := chunkBlockEntityPayload(col, false)

	s.writePacket(&packet.LevelChunk{
		Dimension:     s.dimensionID(dim),
		Position:      protocol.ChunkPos{pos.X(), pos.Z()},
		SubChunkCount: count,
		CacheEnabled:  true,
		BlobHashes:    hashes,
		RawPayload:    raw,
	})
}

// sendNetworkChunk sends a network encoded chunk to the client.
func (s *Session) sendNetworkChunk(pos world.ChunkPos, dim world.Dimension, col *world.Column) {
	c := col.Chunk
	if subChunkRequests {
		biomes := networkBiomePayload(col)
		raw := make([]byte, len(biomes)+1)
		copy(raw, biomes)
		s.writePacket(&packet.LevelChunk{
			Dimension:       s.dimensionID(dim),
			SubChunkCount:   protocol.SubChunkRequestModeLimited,
			Position:        protocol.ChunkPos(pos),
			HighestSubChunk: c.HighestFilledSubChunk(),
			RawPayload:      raw,
		})
		return
	}

	data := networkChunkData(col)
	totalLen := 1 + len(data.Biomes)
	for _, s := range data.SubChunks {
		totalLen += len(s)
	}
	blockEntityPayload := chunkBlockEntityPayload(col, true)
	totalLen += len(blockEntityPayload)

	raw := make([]byte, 0, totalLen)
	for _, subChunk := range data.SubChunks {
		raw = append(raw, subChunk...)
	}
	raw = append(raw, data.Biomes...)
	raw = append(raw, 0)
	raw = append(raw, blockEntityPayload...)

	s.writePacket(&packet.LevelChunk{
		Dimension:     s.dimensionID(dim),
		Position:      protocol.ChunkPos{pos.X(), pos.Z()},
		SubChunkCount: uint32(len(data.SubChunks)),
		RawPayload:    raw,
	})
}

// trackBlob attempts to track the given blob. If the player has too many pending blobs, it returns false and closes the
// connection.
func (s *Session) trackBlob(hash uint64, blob []byte) bool {
	s.blobMu.Lock()
	if len(s.blobs) >= maxPendingBlobs {
		s.blobMu.Unlock()
		s.conf.Log.Error("too many blobs pending", "n", maxPendingBlobs)
		s.CloseConnection()
		return false
	}
	s.blobs[hash] = blob
	s.blobMu.Unlock()
	return true
}

func pooledChunkEncodeBuffer() *bytes.Buffer {
	buf := chunkEncodeBufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	return buf
}

func releaseChunkEncodeBuffer(buf *bytes.Buffer) {
	if buf.Cap() > maxPooledChunkEncodeCap {
		return
	}
	buf.Reset()
	chunkEncodeBufferPool.Put(buf)
}

func joinPayloads(first, second []byte) []byte {
	if len(second) == 0 {
		return first
	}
	raw := make([]byte, len(first)+len(second))
	copy(raw, first)
	copy(raw[len(first):], second)
	return raw
}

func networkChunkData(col *world.Column) chunk.SerialisedData {
	sub := col.Chunk.Sub()
	data := chunk.SerialisedData{
		SubChunks: make([][]byte, len(sub)),
		Biomes:    networkBiomePayload(col),
	}
	for i := range sub {
		data.SubChunks[i] = networkSubChunkPayload(col, int16(i))
	}
	return data
}

func networkBiomePayload(col *world.Column) []byte {
	if payload, ok := col.CachedNetworkBiomePayload(); ok {
		return payload
	}
	payload := chunk.EncodeBiomes(col.Chunk, chunk.NetworkEncoding)
	col.CacheNetworkBiomePayload(payload)
	return payload
}

func networkSubChunkPayload(col *world.Column, ind int16) []byte {
	if payload, ok := col.CachedNetworkSubChunkPayload(ind); ok {
		return payload
	}
	payload := chunk.EncodeSubChunk(col.Chunk, chunk.NetworkEncoding, int(ind))
	col.CacheNetworkSubChunkPayload(ind, payload)
	return payload
}

func chunkBlockEntityPayload(col *world.Column, noBorder bool) []byte {
	if payload, ok := col.CachedChunkBlockEntityPayload(noBorder); ok {
		return payload
	}
	var payload []byte
	if noBorder {
		payload = encodeChunkBlockEntitiesNoBorder(col.BlockEntities)
	} else {
		payload = encodeChunkBlockEntities(col.BlockEntities)
	}
	col.CacheChunkBlockEntityPayload(noBorder, payload)
	return payload
}

func subChunkBlockEntityPayload(col *world.Column, ind int16) []byte {
	if payload, ok := col.CachedSubChunkBlockEntityPayload(ind); ok {
		return payload
	}
	payload := encodeSubChunkBlockEntities(col, ind)
	col.CacheSubChunkBlockEntityPayload(ind, payload)
	return payload
}

func encodeChunkBlockEntities(blockEntities map[cube.Pos]world.Block) []byte {
	buf := pooledChunkEncodeBuffer()
	defer releaseChunkEncodeBuffer(buf)

	// Length of 1 byte for the border block count.
	buf.WriteByte(0)
	encodeAllBlockEntities(buf, blockEntities)

	raw := make([]byte, buf.Len())
	copy(raw, buf.Bytes())
	return raw
}

func encodeChunkBlockEntitiesNoBorder(blockEntities map[cube.Pos]world.Block) []byte {
	buf := pooledChunkEncodeBuffer()
	defer releaseChunkEncodeBuffer(buf)

	encodeAllBlockEntities(buf, blockEntities)
	if buf.Len() == 0 {
		return nil
	}
	raw := make([]byte, buf.Len())
	copy(raw, buf.Bytes())
	return raw
}

func encodeSubChunkBlockEntities(col *world.Column, ind int16) []byte {
	buf := pooledChunkEncodeBuffer()
	defer releaseChunkEncodeBuffer(buf)

	encodeSubChunkEntities(buf, col, ind)
	if buf.Len() == 0 {
		return nil
	}
	raw := make([]byte, buf.Len())
	copy(raw, buf.Bytes())
	return raw
}

func encodeAllBlockEntities(buf *bytes.Buffer, blockEntities map[cube.Pos]world.Block) {
	enc := nbt.NewEncoderWithEncoding(buf, nbt.NetworkLittleEndian)
	for pos, block := range blockEntities {
		nbtBlock, ok := block.(world.NBTer)
		if !ok {
			continue
		}
		encodeBlockEntityNBT(enc, pos, nbtBlock)
	}
}

func encodeSubChunkEntities(buf *bytes.Buffer, col *world.Column, ind int16) {
	enc := nbt.NewEncoderWithEncoding(buf, nbt.NetworkLittleEndian)
	for pos, block := range col.BlockEntities {
		if col.Chunk.SubIndex(int16(pos.Y())) != ind {
			continue
		}
		nbtBlock, ok := block.(world.NBTer)
		if !ok {
			continue
		}
		encodeBlockEntityNBT(enc, pos, nbtBlock)
	}
}

func encodeBlockEntityNBT(enc *nbt.Encoder, pos cube.Pos, nbtBlock world.NBTer) {
	data := nbtBlock.EncodeNBT()
	if data == nil {
		data = map[string]any{}
	}
	data["x"], data["y"], data["z"] = int32(pos[0]), int32(pos[1]), int32(pos[2])
	_ = enc.Encode(data)
}
