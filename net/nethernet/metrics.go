package nethernet

import "github.com/dm-vev/adamant/net/metrics"

// Reasons a datagram is dropped, for droppedDatagrams.
const (
	dropUnknownUfrag = iota
	dropUnknownAddress
	dropOtherProtocol
	dropReasons
)

var dropReasonNames = [dropReasons]string{"unknown_ufrag", "unknown_address", "other_protocol"}

// maxSocketMetrics bounds the sockets counted apart.
const maxSocketMetrics = 64

// Metrics of the listeners of the process.
var (
	datagramsReceived = metrics.Default.CounterVec("adamant_nethernet_datagrams_received_total",
		"Datagrams the sockets of NetherNet listeners received.", "socket", maxSocketMetrics, metrics.Itoa)
	bytesReceived = metrics.Default.CounterVec("adamant_nethernet_bytes_received_total",
		"Bytes of the datagrams the sockets of NetherNet listeners received.", "socket", maxSocketMetrics, metrics.Itoa)
	datagramsSent = metrics.Default.CounterVec("adamant_nethernet_datagrams_sent_total",
		"Datagrams the sockets of NetherNet listeners sent.", "socket", maxSocketMetrics, metrics.Itoa)
	bytesSent = metrics.Default.CounterVec("adamant_nethernet_bytes_sent_total",
		"Bytes of the datagrams the sockets of NetherNet listeners sent.", "socket", maxSocketMetrics, metrics.Itoa)
	droppedDatagrams = metrics.Default.CounterVec("adamant_nethernet_datagrams_dropped_total",
		"Datagrams no connection took, by why.", "reason", dropReasons, func(i int) string { return dropReasonNames[i] })
	relayedDatagrams = metrics.Default.Counter("adamant_nethernet_relayed_datagrams_total",
		"Datagrams of peers that came through the TURN relay.")
	offers = metrics.Default.Counter("adamant_nethernet_offers_total",
		"Offers the listeners answered, over HTTP or a signaling service.")
	offersRefused = metrics.Default.Counter("adamant_nethernet_offers_refused_total",
		"Offers the listeners refused with an error.")
	openSeconds = metrics.Default.Histogram("adamant_nethernet_open_seconds",
		"Time from the answer to an offer to the channels of the connection open.",
		0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 20)
	connectionsOpen = metrics.Default.Gauge("adamant_nethernet_connections_open",
		"Connections of the listeners whose channels opened and that did not close.")
	connectionsPending = metrics.Default.Gauge("adamant_nethernet_connections_pending",
		"Connections of the listeners that did not open yet.")
	sendBatches = metrics.Default.Counter("adamant_nethernet_send_batches_total",
		"Calls that sent the datagrams of several connections of a socket at once.")
	sendBatchMessages = metrics.Default.Histogram("adamant_nethernet_send_batch_messages",
		"What connections sent at once in a call sending for several.", 1, 2, 4, 8, 16, 32, 64)
)

// socketIndex returns the index of a socket of the listener for the
// metrics.
func (l *Listener) socketIndex(sock any) int {
	for i, s := range l.socks {
		if any(s) == sock {
			return i
		}
	}
	return 0
}
