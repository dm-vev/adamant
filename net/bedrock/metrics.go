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
	ids            []uint16
	packets, bytes []uint64
}

// add counts a packet.
func (t *tally) add(p []byte) {
	id, n := binary.Uvarint(p)
	if n <= 0 {
		return
	}
	i := uint16(id & (maxPacketID - 1))
	for k, x := range t.ids {
		if x == i {
			t.packets[k]++
			t.bytes[k] += uint64(len(p))
			return
		}
	}
	if len(t.ids) == maxTally {
		countPacket(packetsSent, packetBytesSent, p)
		return
	}
	t.ids = append(t.ids, i)
	t.packets = append(t.packets, 1)
	t.bytes = append(t.bytes, uint64(len(p)))
}

// flush adds what was counted to the counters of the process.
func (t *tally) flush() {
	for k, i := range t.ids {
		packetsSent.Add(int(i), t.packets[k])
		packetBytesSent.Add(int(i), t.bytes[k])
	}
	t.ids, t.packets, t.bytes = t.ids[:0], t.packets[:0], t.bytes[:0]
}

// countPacket counts a packet in a vector of packets and one of bytes.
func countPacket(packets, bytes *metrics.CounterVec, p []byte) {
	id, n := binary.Uvarint(p)
	if n <= 0 {
		return
	}
	i := int(id & (maxPacketID - 1))
	packets.Add(i, 1)
	bytes.Add(i, uint64(len(p)))
}
