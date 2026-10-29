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
	rateLimited = metrics.Default.Counter("adamant_bedrock_rate_limited_total",
		"Connections cut for sending more than their rates allow.")
)

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
