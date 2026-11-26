package session

import (
	"bytes"
	"encoding/binary"
	"math"
	"strings"
	"unsafe"

	"github.com/cespare/xxhash/v2"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/dm-vev/adamant/net/nbt"
	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/world"
	"github.com/dm-vev/adamant/server/world/chunk"
)

// subChunkRequests is set to true to enable the sub-chunk request system. This can (likely) cause unexpected issues,
// but also solves issues with block entities such as item frames and lecterns as of v1.19.10.
const subChunkRequests = true

const (
	maxPendingBlobs         = 4096
	maxSubChunkOffsets      = 4096
	maxPooledChunkEncodeCap = 64 << 10
)

var (
	// zeroBorderPayload is the read-only payload for a chunk with zero border
	// blocks and no block entities.
	zeroBorderPayload = []byte{0}
	// blockEntityVersionZeroTag is the NetworkLittleEndian NBT encoding of the
	// default BlockEntityVersion int32(0) tag.
	blockEntityVersionZeroTag = []byte{
		3, 18, 'B', 'l', 'o', 'c', 'k', 'E', 'n', 't', 'i', 't', 'y', 'V', 'e', 'r', 's', 'i', 'o', 'n', 0,
	}
)

// ViewChunk ...
func (s *Session) ViewChunk(pos world.ChunkPos, dim world.Dimension, col *world.Column) {
	if !s.conn.ClientCacheEnabled() {
		s.sendNetworkChunk(pos, dim, col)
		return
	}
	s.sendBlobHashes(pos, dim, col)
}

// ViewSubChunks sends the sub-chunks at the offsets from centre and returns
// the bytes of their payloads: to a client that caches them, those of the
// block entities, as the blobs go when it asks for them.
func (s *Session) ViewSubChunks(centre world.SubChunkPos, offsets []mc.SubChunkOffset, tx *world.Tx) int {
	cacheEnabled := s.conn.ClientCacheEnabled()
	if s.chunkLoader == nil {
		// The chunk loader is initialised during Spawn, so return an empty response for early requests.
		dim, _ := world.DimensionID(tx.World().Dimension())
		s.writePacket(&mc.SubChunkPacket{
			Dimension:       int32(dim),
			Position:        mc.SubChunkPos(centre),
			CacheEnabled:    cacheEnabled,
			SubChunkEntries: nil,
		})
		return 0
	}
	if len(offsets) > maxSubChunkOffsets {
		// Cap offsets to bound memory usage when handling malformed or abusive requests.
		offsets = offsets[:maxSubChunkOffsets]
	}
	r := tx.Range()

	entries := make([]mc.SubChunkEntry, 0, len(offsets))
	var transaction map[uint64]struct{}
	if cacheEnabled {
		transaction = make(map[uint64]struct{}, len(offsets))
	}
	size := 0
	for _, offset := range offsets {
		ind := int16(centre.Y()) + int16(offset[1]) - int16(r[0])>>4
		if ind < 0 || ind > int16(r.Height()>>4) {
			entries = append(entries, mc.SubChunkEntry{Result: mc.SubChunkResultIndexOutOfBounds, Offset: offset})
			continue
		}
		col, ok := s.chunkLoader.Chunk(world.ChunkPos{
			centre.X() + int32(offset[0]),
			centre.Z() + int32(offset[2]),
		})
		if !ok {
			entries = append(entries, mc.SubChunkEntry{Result: mc.SubChunkResultChunkNotFound, Offset: offset})
			continue
		}
		entry := s.subChunkEntry(offset, ind, col, transaction)
		size += len(entry.RawPayload.Val)
		entries = append(entries, entry)
	}
	if cacheEnabled && len(transaction) > 0 {
		s.blobMu.Lock()
		s.openChunkTransactions = append(s.openChunkTransactions, transaction)
		s.blobMu.Unlock()
	}
	dim, _ := world.DimensionID(tx.World().Dimension())
	s.writePacket(&mc.SubChunkPacket{
		Dimension:       int32(dim),
		Position:        mc.SubChunkPos(centre),
		CacheEnabled:    cacheEnabled,
		SubChunkEntries: entries,
	})
	return size
}

func (s *Session) subChunkEntry(offset mc.SubChunkOffset, ind int16, col *world.Column, transaction map[uint64]struct{}) mc.SubChunkEntry {
	subMapType, subMap := subChunkHeightMap(col, ind)
	var subMapData proto.Optional[mc.HeightMap]
	if subMap != nil {
		var heightMap mc.HeightMap
		if len(subMap) == 256 {
			copy(unsafe.Slice(&heightMap[0][0], 256), subMap)
		} else {
			for z := range heightMap {
				copy(heightMap[z][:], subMap[z*16:(z+1)*16])
			}
		}
		subMapData = proto.Some(heightMap)
	}

	sub := col.Sub()[ind]
	if sub.Empty() {
		return mc.SubChunkEntry{
			Result:              mc.SubChunkResultSuccessAllAir,
			HeightMapType:       subMapType,
			HeightMapData:       subMapData,
			RenderHeightMapType: subMapType,
			RenderHeightMapData: subMapData,
			Offset:              offset,
		}
	}
	serialisedSubChunk := s.tables().networkSubChunkPayload(col, ind)
	blockEntityPayload := subChunkBlockEntityPayload(col, ind)

	entry := mc.SubChunkEntry{
		Result:              mc.SubChunkResultSuccess,
		HeightMapType:       subMapType,
		HeightMapData:       subMapData,
		RenderHeightMapType: subMapType,
		RenderHeightMapData: subMapData,
		Offset:              offset,
	}
	if s.conn.ClientCacheEnabled() {
		if hash := xxhash.Sum64(serialisedSubChunk); s.trackBlob(hash, serialisedSubChunk) {
			transaction[hash] = struct{}{}
			entry.BlobHash = proto.Some(hash)
			entry.RawPayload = proto.Some(blockEntityPayload)
			return entry
		}
	}
	entry.RawPayload = proto.Some(joinPayloads(serialisedSubChunk, blockEntityPayload))
	return entry
}

func subChunkHeightMap(col *world.Column, ind int16) (byte, []int8) {
	if mapType, mapData, ok := col.CachedSubChunkHeightMap(ind); ok {
		return mapType, mapData
	}

	chunkMap := col.HeightMap()
	var buf [256]int8
	subY := col.SubY(ind)
	higher, lower := true, true
	for z := uint8(0); z < 16; z++ {
		row := uint16(z) << 4
		for x := uint8(0); x < 16; x++ {
			y := chunkMap.At(x, z)
			otherInd := col.SubIndex(y)
			if otherInd > ind {
				buf[row|uint16(x)], lower = 16, false
				continue
			}
			if otherInd < ind {
				buf[row|uint16(x)], higher = -1, false
				continue
			}
			buf[row|uint16(x)], lower, higher = int8(y-subY), false, false
		}
	}
	var subMapType byte
	var subMap []int8
	switch {
	case higher:
		subMapType = mc.HeightMapDataTooHigh
	case lower:
		subMapType = mc.HeightMapDataTooLow
	default:
		subMapType = mc.HeightMapDataHasData
		subMap = make([]int8, 256)
		copy(subMap, buf[:])
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
		biomes := s.tables().networkBiomePayload(col)
		if hash := xxhash.Sum64(biomes); s.trackBlob(hash, biomes) {
			s.writePacket(&mc.LevelChunkPacket{
				Dimension:     s.dimensionID(dim),
				SubChunkCount: 0,
				Position:      mc.ChunkPos(pos),
				SubChunkLimit: proto.Some(int32(c.HighestFilledSubChunk())),
				BlobHashes:    []uint64{hash},
				RawPayload:    []byte{0},
				CacheEnabled:  true,
			})
			return
		}
		return
	}

	var (
		data   = s.tables().networkChunkData(col)
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

	s.writePacket(&mc.LevelChunkPacket{
		Dimension:     s.dimensionID(dim),
		Position:      mc.ChunkPos{pos.X(), pos.Z()},
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
		s.writePacket(&mc.LevelChunkPacket{
			Dimension:     s.dimensionID(dim),
			SubChunkCount: 0,
			Position:      mc.ChunkPos(pos),
			SubChunkLimit: proto.Some(int32(c.HighestFilledSubChunk())),
			RawPayload:    s.tables().limitedChunkPayload(col),
		})
		return
	}

	s.writePacket(&mc.LevelChunkPacket{
		Dimension:     s.dimensionID(dim),
		Position:      mc.ChunkPos{pos.X(), pos.Z()},
		SubChunkCount: uint32(len(c.Sub())),
		RawPayload:    s.tables().fullChunkPayload(col),
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

func joinPayloads(first, second []byte) []byte {
	if len(second) == 0 {
		return first
	}
	raw := make([]byte, len(first)+len(second))
	copy(raw, first)
	copy(raw[len(first):], second)
	return raw
}

// The payloads of a column are kept in the caches of the column: those of
// tables that name blocks as the game does in the column's own, and those
// of tables of another class of blocks in the cache of the column for the
// key of that class, so that each class encodes a sub-chunk once.

// columnCache returns the cache of a column the payloads of sub-chunks and
// chunks of the tables are kept in.
func (w *wireTables) columnCache(col *world.Column) *world.ColumnCache {
	if w.blocks.Load() == nil {
		w.hashes()
	}
	return col.Cache(w.slot.Load())
}

func (w *wireTables) networkChunkData(col *world.Column) chunk.SerialisedData {
	sub := col.Sub()
	data := chunk.SerialisedData{
		SubChunks: make([][]byte, len(sub)),
		Biomes:    w.networkBiomePayload(col),
	}
	for i := range sub {
		data.SubChunks[i] = w.networkSubChunkPayload(col, int16(i))
	}
	return data
}

// networkBiomePayload returns the biomes of a column, which every protocol
// names alike: they are kept in the column's own cache, with the payload
// limitedChunkPayload sends, which is the biomes and a byte after them: the
// two are one array.
func (w *wireTables) networkBiomePayload(col *world.Column) []byte {
	if payload, ok := col.CachedNetworkBiomePayload(); ok {
		return payload
	}
	limited := encodeBiomesTerminated(col.Chunk)
	biomes := limited[: len(limited)-1 : len(limited)-1]
	col.CacheNetworkBiomePayload(biomes)
	col.CacheLimitedChunkPayload(limited)
	return biomes
}

func (w *wireTables) networkSubChunkPayload(col *world.Column, ind int16) []byte {
	cache := w.columnCache(col)
	if payload, ok := cache.CachedNetworkSubChunkPayload(ind); ok {
		return payload
	}
	payload := encodeSubChunk(col.Chunk, int(ind), w.blocksOf(col.BlockRegistry()))
	cache.CacheNetworkSubChunkPayload(ind, payload)
	return payload
}

// limitedChunkPayload returns the payload of a column sent to a client that
// asks for its sub-chunks: its biomes, which every protocol names alike, and
// a byte after them. It shares its array with the biomes of the column.
func (w *wireTables) limitedChunkPayload(col *world.Column) []byte {
	if payload, ok := col.CachedLimitedChunkPayload(); ok {
		return payload
	}
	w.networkBiomePayload(col)
	payload, _ := col.CachedLimitedChunkPayload()
	return payload
}

func (w *wireTables) fullChunkPayload(col *world.Column) []byte {
	cache := w.columnCache(col)
	if payload, ok := cache.CachedFullChunkPayload(); ok {
		return payload
	}

	data := w.networkChunkData(col)
	totalLen := 1 + len(data.Biomes)
	for _, subChunk := range data.SubChunks {
		totalLen += len(subChunk)
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
	cache.CacheFullChunkPayload(raw)
	return raw
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
	if len(blockEntities) == 0 {
		return zeroBorderPayload
	}
	buf := chunkWireBufferPool.Get().(*[]byte)
	// Length of 1 byte for the border block count.
	b := (*buf)[:0]
	b = append(b, 0)
	b = appendAllBlockEntities(b, blockEntities)
	return releaseChunkWireBuffer(buf, b)
}

func encodeChunkBlockEntitiesNoBorder(blockEntities map[cube.Pos]world.Block) []byte {
	if len(blockEntities) == 0 {
		return nil
	}
	buf := chunkWireBufferPool.Get().(*[]byte)
	b := appendAllBlockEntities((*buf)[:0], blockEntities)
	if len(b) == 0 {
		if cap(b) <= maxPooledChunkEncodeCap {
			*buf = b[:0]
			chunkWireBufferPool.Put(buf)
		}
		return nil
	}
	return releaseChunkWireBuffer(buf, b)
}

func encodeSubChunkBlockEntities(col *world.Column, ind int16) []byte {
	if len(col.BlockEntities) == 0 {
		return nil
	}
	buf := chunkWireBufferPool.Get().(*[]byte)
	b := appendSubChunkEntities((*buf)[:0], col, ind)
	if len(b) == 0 {
		if cap(b) <= maxPooledChunkEncodeCap {
			*buf = b[:0]
			chunkWireBufferPool.Put(buf)
		}
		return nil
	}
	return releaseChunkWireBuffer(buf, b)
}

func appendAllBlockEntities(b []byte, blockEntities map[cube.Pos]world.Block) []byte {
	for pos, block := range blockEntities {
		nbtBlock, ok := clientBlockActor(block)
		if !ok {
			continue
		}
		b = appendBlockEntityNBT(b, pos, nbtBlock)
	}
	return b
}

func appendSubChunkEntities(b []byte, col *world.Column, ind int16) []byte {
	for pos, block := range col.BlockEntities {
		if col.SubIndex(int16(pos.Y())) != ind {
			continue
		}
		nbtBlock, ok := clientBlockActor(block)
		if !ok {
			continue
		}
		b = appendBlockEntityNBT(b, pos, nbtBlock)
	}
	return b
}

// clientBlockActor returns the block as a block actor clients are sent, if
// it has NBT Bedrock knows a block actor for.
func clientBlockActor(b world.Block) (world.NBTer, bool) {
	if _, serverOnly := b.(world.ServerOnlyNBTer); serverOnly {
		return nil, false
	}
	nbtBlock, ok := b.(world.NBTer)
	return nbtBlock, ok
}

func appendBlockEntityNBT(b []byte, pos cube.Pos, nbtBlock world.NBTer) []byte {
	data := nbtBlock.EncodeNBT()
	if _, actor := data["id"]; !actor {
		// A placeholder state without block actor data.
		return b
	}
	return appendClientActorNetworkNBT(b, data, pos)
}

// appendClientActorNetworkNBT encodes a block actor compound directly in
// NetworkLittleEndian NBT format, adding position and default
// BlockEntityVersion while omitting adamant: keys without cloning data or
// allocating through reflection. Any unsupported Go value falls back to
// nbt.NewEncoderWithEncoding.
func appendClientActorNetworkNBT(b []byte, data map[string]any, pos cube.Pos) []byte {
	start := len(b)
	// Root compound tag (10) with empty name (length 0).
	b = append(b, 10, 0)
	hasVersion := false
	for k, v := range data {
		if strings.HasPrefix(k, "adamant:") || k == "x" || k == "y" || k == "z" {
			continue
		}
		if k == "BlockEntityVersion" {
			hasVersion = true
		}
		var ok bool
		if b, ok = appendNetNBTTag(b, k, v, 1); !ok {
			return appendBlockEntityNBTFallback(b[:start], data, pos)
		}
	}
	if !hasVersion {
		b = append(b, blockEntityVersionZeroTag...)
	}
	b = append(b, 3, 1, 'x')
	b = appendVarint32(b, int32(pos[0]))
	b = append(b, 3, 1, 'y')
	b = appendVarint32(b, int32(pos[1]))
	b = append(b, 3, 1, 'z')
	b = appendVarint32(b, int32(pos[2]))
	return append(b, 0)
}

func appendBlockEntityNBTFallback(b []byte, data map[string]any, pos cube.Pos) []byte {
	var buf bytes.Buffer
	enc := nbt.NewEncoderWithEncoding(&buf, nbt.NetworkLittleEndian)
	_ = enc.Encode(clientActorNBT(data, pos))
	return append(b, buf.Bytes()...)
}

const maxNetNBTDepth = 512

func appendNetNBTTag(b []byte, name string, v any, depth int) ([]byte, bool) {
	if depth >= maxNetNBTDepth || len(name) > math.MaxInt16 {
		return b, false
	}
	switch v := v.(type) {
	case byte:
		b = appendNetNBTHeader(b, 1, name)
		return append(b, v), true
	case bool:
		b = appendNetNBTHeader(b, 1, name)
		if v {
			return append(b, 1), true
		}
		return append(b, 0), true
	case int16:
		b = appendNetNBTHeader(b, 2, name)
		return binary.LittleEndian.AppendUint16(b, uint16(v)), true
	case int32:
		b = appendNetNBTHeader(b, 3, name)
		return appendVarint32(b, v), true
	case int64:
		b = appendNetNBTHeader(b, 4, name)
		return appendVarint64(b, v), true
	case float32:
		b = appendNetNBTHeader(b, 5, name)
		return binary.LittleEndian.AppendUint32(b, math.Float32bits(v)), true
	case float64:
		b = appendNetNBTHeader(b, 6, name)
		return binary.LittleEndian.AppendUint64(b, math.Float64bits(v)), true
	case string:
		if len(v) > math.MaxInt16 {
			return b, false
		}
		b = appendNetNBTHeader(b, 8, name)
		return appendNetNBTString(b, v), true
	case map[string]any:
		b = appendNetNBTHeader(b, 10, name)
		return appendNetNBTCompoundBody(b, v, depth+1)
	default:
		return appendNetNBTSliceTag(b, name, v, depth)
	}
}

func appendNetNBTSliceTag(b []byte, name string, v any, depth int) ([]byte, bool) {
	switch v := v.(type) {
	case []byte:
		b = appendNetNBTHeader(b, 9, name)
		b = append(b, 1)
		b = appendVarint32(b, int32(len(v)))
		return append(b, v...), true
	case []int32:
		b = appendNetNBTHeader(b, 9, name)
		b = append(b, 3)
		b = appendVarint32(b, int32(len(v)))
		for _, elem := range v {
			b = appendVarint32(b, elem)
		}
		return b, true
	case []int64:
		b = appendNetNBTHeader(b, 9, name)
		b = append(b, 4)
		b = appendVarint32(b, int32(len(v)))
		for _, elem := range v {
			b = appendVarint64(b, elem)
		}
		return b, true
	case []string:
		b = appendNetNBTHeader(b, 9, name)
		b = append(b, 8)
		b = appendVarint32(b, int32(len(v)))
		for _, elem := range v {
			if len(elem) > math.MaxInt16 {
				return b, false
			}
			b = appendNetNBTString(b, elem)
		}
		return b, true
	case []map[string]any:
		b = appendNetNBTHeader(b, 9, name)
		b = append(b, 10)
		b = appendVarint32(b, int32(len(v)))
		for _, elem := range v {
			var ok bool
			if b, ok = appendNetNBTCompoundBody(b, elem, depth+2); !ok {
				return b, false
			}
		}
		return b, true
	case []any:
		return appendNetNBTAnySliceTag(b, name, v, depth)
	default:
		return b, false
	}
}

func appendNetNBTAnySliceTag(b []byte, name string, v []any, depth int) ([]byte, bool) {
	if len(v) == 0 {
		b = appendNetNBTHeader(b, 9, name)
		b = append(b, 0)
		return appendVarint32(b, 0), true
	}
	switch v[0].(type) {
	case string:
		b = appendNetNBTHeader(b, 9, name)
		b = append(b, 8)
		b = appendVarint32(b, int32(len(v)))
		for _, elem := range v {
			s, ok := elem.(string)
			if !ok || len(s) > math.MaxInt16 {
				return b, false
			}
			b = appendNetNBTString(b, s)
		}
		return b, true
	case map[string]any:
		b = appendNetNBTHeader(b, 9, name)
		b = append(b, 10)
		b = appendVarint32(b, int32(len(v)))
		for _, elem := range v {
			m, ok := elem.(map[string]any)
			if !ok {
				return b, false
			}
			if b, ok = appendNetNBTCompoundBody(b, m, depth+2); !ok {
				return b, false
			}
		}
		return b, true
	default:
		return b, false
	}
}

func appendNetNBTCompoundBody(b []byte, m map[string]any, depth int) ([]byte, bool) {
	if depth >= maxNetNBTDepth {
		return b, false
	}
	for k, v := range m {
		var ok bool
		if b, ok = appendNetNBTTag(b, k, v, depth); !ok {
			return b, false
		}
	}
	return append(b, 0), true
}

func appendNetNBTHeader(b []byte, tag byte, name string) []byte {
	b = append(b, tag)
	return appendNetNBTString(b, name)
}

func appendNetNBTString(b []byte, s string) []byte {
	u := uint32(len(s))
	for u >= 0x80 {
		b = append(b, byte(u)|0x80)
		u >>= 7
	}
	b = append(b, byte(u))
	return append(b, s...)
}

func appendVarint64(b []byte, v int64) []byte {
	u := uint64((v << 1) ^ (v >> 63))
	for u >= 0x80 {
		b = append(b, byte(u)|0x80)
		u >>= 7
	}
	return append(b, byte(u))
}

// clientUpdateNBT returns the block actor update data passed as the client of
// the session knows it. The players a vault connects to are listed by actor
// unique ID, and the client knows its own player by the ID of the session.
func (s *Session) clientUpdateNBT(data map[string]any) map[string]any {
	if players, ok := data["connected_players"].([]int64); ok && s.ent != nil {
		own := s.ent.UniqueID()
		ids := make([]int64, len(players))
		for i, id := range players {
			if ids[i] = id; id == own {
				ids[i] = int64(selfEntityRuntimeID)
			}
		}
		data["connected_players"] = ids
	}
	return data
}

// clientActorNBT returns the block actor compound clients are sent for a
// block at pos with the NBT data passed: Bedrock's actor without the
// adamant: keys holding state only the server uses.
func clientActorNBT(data map[string]any, pos cube.Pos) map[string]any {
	actor := world.BlockActorNBT(data, pos)
	for key := range actor {
		if strings.HasPrefix(key, "adamant:") {
			delete(actor, key)
		}
	}
	return actor
}
