package session

import "github.com/dm-vev/adamant/net/metrics"

// Metrics of the sessions of the process.
var (
	flushes = metrics.Default.Counter("adamant_session_flushes_total",
		"Batches the flushers wrote to the connections of sessions.")
	flushSeconds = metrics.Default.Histogram("adamant_session_flush_seconds",
		"Time a flusher took to encode, compress and send the batch of a session.",
		0.00005, 0.0001, 0.00025, 0.0005, 0.001, 0.0025, 0.005, 0.01, 0.05)
	flushPackets = metrics.Default.Histogram("adamant_session_flush_packets",
		"Packets of the batches of sessions.", 1, 2, 5, 10, 20, 50, 100, 500, 2000)
	tickToSend = metrics.Default.Histogram("adamant_replication_delay_seconds",
		"Time from the end of the tick of a world to the batch of a session of it sent.",
		0.0001, 0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1)
	backlogClosed = metrics.Default.Counter("adamant_sessions_backlog_closed_total",
		"Sessions closed for packets piled up on a connection that did not keep up.")
	inboxCut = metrics.Default.Counter("adamant_sessions_inbox_closed_total",
		"Sessions closed for a client far ahead of the world of its player.")
	sessionsOpen = metrics.Default.Gauge("adamant_sessions",
		"Sessions of players in the worlds of the server.")
)
