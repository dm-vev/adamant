package bedrock

import (
	"encoding/binary"

	"github.com/dm-vev/adamant/net/metrics"
)

// maxPacketID bounds the packet IDs, ten bits of the header.
const maxPacketID = 1024

// Metrics of the connections of the process.
var (
	packetsSent = metrics.Default.CounterVec("adamant_bedrock_packets_sent_total",
		"Packets written to Bedrock connections, by ID.", "id", maxPacketID, metrics.Itoa)
	packetBytesSent = metrics.Default.CounterVec("adamant_bedrock_packet_bytes_sent_total",
		"Bytes of the packets written to Bedrock connections, header included, by ID.", "id", maxPacketID, metrics.Itoa)
	packetsReceived = metrics.Default.CounterVec("adamant_bedrock_packets_received_total",
		"Packets read from Bedrock connections, by ID.", "id", maxPacketID, metrics.Itoa)
	packetBytesReceived = metrics.Default.CounterVec("adamant_bedrock_packet_bytes_received_total",
		"Bytes of the packets read from Bedrock connections, header included, by ID.", "id", maxPacketID, metrics.Itoa)
	batchesSent = metrics.Default.Counter("adamant_bedrock_batches_sent_total",
		"Batches sent on Bedrock connections, static ones included.")
	batchBytesRaw = metrics.Default.Counter("adamant_bedrock_batch_bytes_raw_total",
		"Bytes of the bodies of the batches sent, before compression.")
	batchBytesSent = metrics.Default.Counter("adamant_bedrock_batch_bytes_sent_total",
		"Bytes of the batches sent, after compression.")
	batchCacheHits = metrics.Default.Counter("adamant_bedrock_batch_cache_hits_total",
		"Batches sent compressed as another connection compressed the same body.")
	rateLimited = metrics.Default.Counter("adamant_bedrock_rate_limited_total",
		"Connections cut for sending more than their rates allow.")
)

// maxTally bounds the packet IDs a tally keeps apart; the packets of more
// are counted at once.
const maxTally = 32

// tally counts the packets of a batch by ID until the batch is sent, so
// that the counters of the process, which every connection adds to, are
// added to once a batch for each ID rather than once a packet.
type tally struct {
	n       int
	ids     [maxTally]uint16
	packets [maxTally]uint64
	bytes   [maxTally]uint64
}

// packetID decodes the 10-bit packet ID from a raw packet header.
func packetID(p []byte) (uint16, bool) {
	if len(p) > 0 {
		if b0 := p[0]; b0 < 0x80 {
			return uint16(b0), true
		} else if len(p) >= 2 && p[1] < 0x80 {
			return uint16(b0&0x7f) | (uint16(p[1])<<7)&(maxPacketID-1), true
		}
	}
	id, n := binary.Uvarint(p)
	if n <= 0 {
		return 0, false
	}
	return uint16(id & (maxPacketID - 1)), true
}

// addID counts a packet with an already-decoded ID and byte length.
func (t *tally) addID(packets, bytes *metrics.CounterVec, id uint16, size uint64) {
	for k := range t.n {
		if t.ids[k] == id {
			t.packets[k]++
			t.bytes[k] += size
			return
		}
	}
	if t.n == maxTally {
		packets.Add(int(id), 1)
		bytes.Add(int(id), size)
		return
	}
	t.ids[t.n] = id
	t.packets[t.n] = 1
	t.bytes[t.n] = size
	t.n++
}

// add counts a packet.
func (t *tally) add(packets, bytes *metrics.CounterVec, p []byte) {
	if id, ok := packetID(p); ok {
		t.addID(packets, bytes, id, uint64(len(p)))
	}
}

// flush adds what was counted to the counters of the process.
func (t *tally) flush(packets, bytes *metrics.CounterVec) {
	for k := range t.n {
		i := int(t.ids[k])
		packets.Add(i, t.packets[k])
		bytes.Add(i, t.bytes[k])
	}
	t.n = 0
}
